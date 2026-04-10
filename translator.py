#!/usr/bin/env python3
"""
Translator proxy:
  GET  /api/v1/models            → 返回伪造的 OpenRouter 兼容模型列表
  POST /api/v1/chat/completions  → Chat Completions → Anthropic Messages → sub2api
  POST /api/v1/messages          → 透传，带 /api 前缀去除 + model 名映射
  其他路径                        → 透传，带 /api 前缀去除
监听 127.0.0.1:18080，上游 http://192.168.48.12:8080。
"""

import json
import time
import uuid
import urllib.request
import urllib.error
from http.server import BaseHTTPRequestHandler, HTTPServer

UPSTREAM = "http://192.168.48.12:8080"
FORWARD_HEADERS = (
    "Authorization", "Content-Type", "x-api-key",
    "anthropic-version", "anthropic-beta", "Accept",
)
SKIP_RESP_HEADERS = ("transfer-encoding", "connection", "content-length")

# OpenRouter model 名 → sub2api model 名
MODEL_MAP = {
    "anthropic/claude-sonnet-4-6":      "claude-sonnet-4-6",
    "anthropic/claude-haiku-4-5":       "claude-haiku-4-5-20251001",
    "anthropic/claude-opus-4-6":        "claude-opus-4-6",
    # 带点号的别名（Anthropic SDK env var 格式）
    "anthropic/claude-sonnet-4.6":      "claude-sonnet-4-6",
    "anthropic/claude-haiku-4.5":       "claude-haiku-4-5-20251001",
    "anthropic/claude-opus-4.6":        "claude-opus-4-6",
}

# GET /v1/models 时返回的假模型列表（Trae 用它验证用户输入的 model ID）
# 必须用 provider/model 格式，OpenRouter 客户端会校验格式
FAKE_MODELS = [
    "anthropic/claude-sonnet-4-5",
    "anthropic/claude-haiku-4-5",
    "anthropic/claude-opus-4-1",
]


def map_model(name: str) -> str:
    return MODEL_MAP.get(name, name)


def flatten_content(content) -> str:
    """将内容展平为纯字符串（仅用于 system 消息）。"""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "".join(
            p.get("text", p.get("content", "")) if isinstance(p, dict) else str(p)
            for p in content
        )
    return str(content)


def convert_content(content):
    """将 Chat Completions content 转换为 Anthropic content 格式。
    纯文本返回字符串；含图片则返回内容块列表。
    """
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return str(content)

    has_image = any(isinstance(p, dict) and p.get("type") == "image_url" for p in content)
    if not has_image:
        return "".join(
            p.get("text", p.get("content", "")) if isinstance(p, dict) else str(p)
            for p in content
        )

    blocks = []
    for part in content:
        if not isinstance(part, dict):
            blocks.append({"type": "text", "text": str(part)})
            continue
        if part.get("type") == "text":
            blocks.append({"type": "text", "text": part.get("text", "")})
        elif part.get("type") == "image_url":
            image_url = part.get("image_url", {})
            url = image_url.get("url", "") if isinstance(image_url, dict) else str(image_url)
            if url.startswith("data:"):
                # data:image/jpeg;base64,xxxx
                header, data = url.split(",", 1)
                media_type = header.split(";")[0].split(":")[1]
                blocks.append({
                    "type": "image",
                    "source": {"type": "base64", "media_type": media_type, "data": data},
                })
            else:
                blocks.append({
                    "type": "image",
                    "source": {"type": "url", "url": url},
                })
    return blocks


# ── Chat Completions ↔ Anthropic Messages 转换 ────────────────────────────────

def convert_tools(tools: list) -> list:
    """Chat Completions tools → Anthropic tools。
    兼容两种格式：
    - OpenAI 格式：{"type": "function", "function": {"name": ..., "parameters": ...}}
    - Anthropic 格式（已就绪）：{"name": ..., "description": ..., "input_schema": ...}
    """
    result = []
    for t in tools:
        if t.get("type") == "function":
            # OpenAI Chat Completions 格式
            f = t.get("function", {})
            result.append({
                "name": f.get("name", ""),
                "description": f.get("description", ""),
                "input_schema": f.get("parameters", {"type": "object", "properties": {}}),
            })
        elif "name" in t and ("input_schema" in t or "description" in t):
            # 已经是 Anthropic 格式，直接透传
            result.append(t)
    return result


def convert_tool_choice(tc):
    """Chat Completions tool_choice → Anthropic tool_choice。"""
    if isinstance(tc, str):
        if tc == "required":
            return {"type": "any"}
        if tc == "none":
            return {"type": "none"}
        return {"type": "auto"}
    if isinstance(tc, dict) and tc.get("type") == "function":
        return {"type": "tool", "name": tc.get("function", {}).get("name", "")}
    return {"type": "auto"}


def convert_messages(messages: list) -> list:
    """将 Chat Completions messages 数组转换为 Anthropic messages 数组。"""
    result = []
    i = 0
    while i < len(messages):
        m = messages[i]
        role = m.get("role", "")

        if role == "system":
            i += 1
            continue

        if role == "tool":
            # 连续的 tool 消息合并成一条 user 消息（多个 tool_result 块）
            blocks = []
            while i < len(messages) and messages[i].get("role") == "tool":
                tm = messages[i]
                blocks.append({
                    "type": "tool_result",
                    "tool_use_id": tm.get("tool_call_id", ""),
                    "content": flatten_content(tm.get("content", "")),
                })
                i += 1
            result.append({"role": "user", "content": blocks})
            continue

        if role == "assistant":
            tool_calls = m.get("tool_calls")
            if tool_calls:
                blocks = []
                if m.get("content"):
                    blocks.append({"type": "text", "text": flatten_content(m["content"])})
                for tc in tool_calls:
                    f = tc.get("function", {})
                    try:
                        args = json.loads(f.get("arguments", "{}"))
                    except (json.JSONDecodeError, ValueError):
                        args = {}
                    blocks.append({
                        "type": "tool_use",
                        "id": tc.get("id", f"toolu_{uuid.uuid4().hex[:8]}"),
                        "name": f.get("name", ""),
                        "input": args,
                    })
                result.append({"role": "assistant", "content": blocks})
                i += 1
                continue
            result.append({"role": "assistant", "content": convert_content(m.get("content", ""))})
            i += 1
            continue

        result.append({"role": "user", "content": convert_content(m.get("content", ""))})
        i += 1

    return result


def chat_to_anthropic(data: dict) -> dict:
    """Chat Completions 请求 → Anthropic Messages 请求。"""
    messages = data.get("messages", [])
    system_parts = [flatten_content(m["content"]) for m in messages if m.get("role") == "system"]
    out = {
        "model": map_model(data.get("model", "")),
        "max_tokens": data.get("max_tokens", 4096),
        "messages": convert_messages(messages),
    }
    if system_parts:
        out["system"] = "\n".join(system_parts)
    for field in ("stream", "temperature", "top_p", "stop"):
        if field in data:
            out[field] = data[field]
    if "tools" in data:
        out["tools"] = convert_tools(data["tools"])
    if "tool_choice" in data:
        out["tool_choice"] = convert_tool_choice(data["tool_choice"])
    return out


def anthropic_to_chat(data: dict, orig_model: str) -> dict:
    """Anthropic Messages 响应 → Chat Completions 响应。"""
    content_blocks = data.get("content", [])
    text = "".join(b.get("text", "") for b in content_blocks if b.get("type") == "text")
    tool_calls = [
        {
            "id": b.get("id", f"call_{uuid.uuid4().hex[:8]}"),
            "type": "function",
            "function": {
                "name": b.get("name", ""),
                "arguments": json.dumps(b.get("input", {})),
            },
        }
        for b in content_blocks if b.get("type") == "tool_use"
    ]
    stop_reason = data.get("stop_reason", "end_turn")
    finish_map = {"end_turn": "stop", "max_tokens": "length", "tool_use": "tool_calls"}
    usage_raw = data.get("usage", {})
    inp = usage_raw.get("input_tokens", 0)
    out_tok = usage_raw.get("output_tokens", 0)
    message = {"role": "assistant", "content": text or None}
    if tool_calls:
        message["tool_calls"] = tool_calls
    return {
        "id": data.get("id", f"chatcmpl-{uuid.uuid4().hex}"),
        "object": "chat.completion",
        "created": int(time.time()),
        "model": orig_model,
        "choices": [{
            "index": 0,
            "message": message,
            "finish_reason": finish_map.get(stop_reason, "stop"),
        }],
        "usage": {
            "prompt_tokens": inp,
            "completion_tokens": out_tok,
            "total_tokens": inp + out_tok,
        },
    }


def make_chunk(cid: str, model: str, delta: dict, finish=None) -> str:
    obj = {
        "id": cid,
        "object": "chat.completion.chunk",
        "created": int(time.time()),
        "model": model,
        "choices": [{"index": 0, "delta": delta, "finish_reason": finish}],
    }
    return f"data: {json.dumps(obj)}\n\n"


class AnthropicToChat:
    """将 Anthropic SSE 流转换为 Chat Completions SSE 流。"""

    def __init__(self, orig_model: str):
        self.model = orig_model
        self.cid = f"chatcmpl-{uuid.uuid4().hex}"
        self.started = False
        self._done = False
        # tool_use 流式状态：index → call_id
        self._tool_idx: dict[int, str] = {}

    def feed(self, line: str) -> str:
        line = line.rstrip("\r\n")
        if not line.startswith("data:"):
            return ""
        payload = line[5:].strip()
        try:
            ev = json.loads(payload)
        except (json.JSONDecodeError, ValueError):
            return ""

        ev_type = ev.get("type", "")
        out = ""

        if ev_type == "message_start" and not self.started:
            self.started = True
            out += make_chunk(self.cid, self.model, {"role": "assistant", "content": ""})

        elif ev_type == "content_block_start":
            block = ev.get("content_block", {})
            if block.get("type") == "tool_use":
                idx = ev.get("index", 0)
                call_id = block.get("id", f"call_{uuid.uuid4().hex[:8]}")
                self._tool_idx[idx] = call_id
                out += make_chunk(self.cid, self.model, {
                    "tool_calls": [{
                        "index": idx,
                        "id": call_id,
                        "type": "function",
                        "function": {"name": block.get("name", ""), "arguments": ""},
                    }]
                })

        elif ev_type == "content_block_delta":
            delta = ev.get("delta", {})
            if delta.get("type") == "text_delta":
                out += make_chunk(self.cid, self.model, {"content": delta.get("text", "")})
            elif delta.get("type") == "input_json_delta":
                idx = ev.get("index", 0)
                out += make_chunk(self.cid, self.model, {
                    "tool_calls": [{"index": idx, "function": {"arguments": delta.get("partial_json", "")}}]
                })

        elif ev_type == "message_delta":
            stop_reason = ev.get("delta", {}).get("stop_reason", "end_turn")
            finish_map = {"end_turn": "stop", "max_tokens": "length", "tool_use": "tool_calls"}
            out += make_chunk(self.cid, self.model, {}, finish_map.get(stop_reason, "stop"))

        elif ev_type == "message_stop" and not self._done:
            self._done = True
            out += "data: [DONE]\n\n"

        return out


# ── HTTP Handler ──────────────────────────────────────────────────────────────

class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        print(f"[translator] {self.address_string()} {fmt % args}")

    def _read_body(self) -> bytes:
        n = int(self.headers.get("Content-Length", 0))
        return self.rfile.read(n) if n else b""

    def _build_headers(self, body: bytes, extra: dict = None) -> dict:
        h = {}
        for k in FORWARD_HEADERS:
            v = self.headers.get(k)
            if v:
                h[k] = v
        h["Content-Length"] = str(len(body))
        if extra:
            h.update(extra)
        return h

    def _send_json(self, data: dict, status: int = 200):
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _strip_api_prefix(self, path: str) -> str:
        if path.startswith("/api/"):
            return path[4:]
        if path == "/api":
            return "/"
        return path

    # ── 伪造 models 列表 ──────────────────────────────────────────────────────

    def _handle_models(self):
        resp_data = {
            "object": "list",
            "data": [
                {
                    "id": m,
                    "object": "model",
                    "created": int(time.time()),
                    "owned_by": "anthropic",
                }
                for m in FAKE_MODELS
            ],
        }
        self._send_json(resp_data)
        print(f"[translator] GET models → 返回 {len(FAKE_MODELS)} 个伪造模型")

    # ── Chat Completions → Anthropic Messages 转发 ────────────────────────────

    def _handle_chat_completions(self, body: bytes):
        try:
            req_data = json.loads(body) if body else {}
        except json.JSONDecodeError:
            req_data = {}

        orig_model = req_data.get("model", "")
        is_stream = bool(req_data.get("stream", False))
        print(f"[translator] POST chat/completions → /v1/messages model={orig_model} stream={is_stream}")

        anthropic_req = chat_to_anthropic(req_data)
        upstream_body = json.dumps(anthropic_req).encode()
        # 确保 anthropic-version 存在，sub2api 可能需要
        headers = self._build_headers(upstream_body, {
            "Content-Type": "application/json",
            "anthropic-version": self.headers.get("anthropic-version", "2023-06-01"),
        })

        url = UPSTREAM + "/v1/messages"
        req = urllib.request.Request(url, data=upstream_body, headers=headers, method="POST")
        try:
            resp = urllib.request.urlopen(req, timeout=120)
        except urllib.error.HTTPError as e:
            rb = e.read()
            self.send_response(e.code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(rb)))
            self.end_headers()
            self.wfile.write(rb)
            return

        if is_stream:
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.end_headers()
            conv = AnthropicToChat(orig_model)
            buf = b""
            with resp:
                while True:
                    chunk = resp.read(4096)
                    if not chunk:
                        break
                    buf += chunk
                    while b"\n" in buf:
                        line, buf = buf.split(b"\n", 1)
                        out = conv.feed(line.decode("utf-8", errors="replace"))
                        if out:
                            self.wfile.write(out.encode())
                            self.wfile.flush()
        else:
            rb = resp.read()
            try:
                chat_resp = anthropic_to_chat(json.loads(rb), orig_model)
                self._send_json(chat_resp)
            except (json.JSONDecodeError, TypeError):
                # 解析失败则原样返回
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(rb)))
                self.end_headers()
                self.wfile.write(rb)

    # ── 通用透传（含 /api 前缀去除 + model 名映射）────────────────────────────

    def _forward(self, method: str, body: bytes = None):
        if body is None:
            body = self._read_body()

        upstream_path = self._strip_api_prefix(self.path)

        # JSON body 中的 model 名映射
        ct = self.headers.get("Content-Type", "")
        orig_model = mapped_model = ""
        if "application/json" in ct and body:
            try:
                data = json.loads(body)
                orig_model = data.get("model", "")
                mapped_model = map_model(orig_model)
                if mapped_model != orig_model:
                    data["model"] = mapped_model
                    body = json.dumps(data).encode()
            except (json.JSONDecodeError, TypeError):
                pass

        model_log = ""
        if orig_model:
            model_log = f"model={orig_model}→{mapped_model}" if mapped_model != orig_model else f"model={orig_model}"
        print(f"[translator] {method} {self.path} → {upstream_path} {model_log}")

        url = UPSTREAM + upstream_path
        req = urllib.request.Request(
            url, data=body if body else None,
            headers=self._build_headers(body), method=method,
        )
        try:
            resp = urllib.request.urlopen(req, timeout=120)
        except urllib.error.HTTPError as e:
            rb = e.read()
            self.send_response(e.code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(rb)))
            self.end_headers()
            self.wfile.write(rb)
            return

        self.send_response(resp.status)
        for k, v in resp.headers.items():
            if k.lower() not in SKIP_RESP_HEADERS:
                self.send_header(k, v)
        self.end_headers()
        with resp:
            while True:
                chunk = resp.read(4096)
                if not chunk:
                    break
                self.wfile.write(chunk)
                self.wfile.flush()

    # ── 路由分发 ──────────────────────────────────────────────────────────────

    def _dispatch(self, method: str):
        upstream_path = self._strip_api_prefix(self.path)
        norm = upstream_path.lstrip("/").split("?")[0]  # 去掉 query string 后比较

        if method == "GET" and norm == "v1/models":
            self._handle_models()
            return

        if method == "POST" and norm == "v1/chat/completions":
            self._handle_chat_completions(self._read_body())
            return

        self._forward(method)

    def do_GET(self):     self._dispatch("GET")
    def do_POST(self):    self._dispatch("POST")
    def do_OPTIONS(self): self._dispatch("OPTIONS")
    def do_DELETE(self):  self._dispatch("DELETE")


if __name__ == "__main__":
    server = HTTPServer(("127.0.0.1", 18080), Handler)
    print(f"[translator] 监听 http://127.0.0.1:18080 → {UPSTREAM}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[translator] 已停止")

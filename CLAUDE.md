# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project does

`trae-proxy` intercepts HTTPS traffic destined for `openrouter.ai` and reroutes it to a local `sub2api` instance that speaks the Anthropic Messages API. It handles three request types:

1. **Anthropic Messages passthrough** (`POST /v1/messages`) — strips `/api` prefix, maps model names, streams response back unchanged
2. **Chat Completions translation** (`POST /v1/chat/completions`) — converts OpenAI-format requests to Anthropic Messages format, forwards to sub2api, converts response back; supports streaming (SSE) and tool use
3. **Fake models endpoint** (`GET /v1/models`) — returns a hardcoded model list so clients (e.g. Trae) can validate model IDs

DNS hijack via `/etc/hosts` makes `openrouter.ai` resolve to `127.0.0.1`. Caddy terminates TLS on :443 and forwards plain HTTP to `translator.py` on :18080.

> **Warning**: While the proxy is active, `openrouter.ai` resolves to localhost — real OpenRouter is unreachable from any local app or browser.

## Architecture

```
Client  (ANTHROPIC_BASE_URL=https://openrouter.ai/api)
      │
      ↓  [/etc/hosts: openrouter.ai → 127.0.0.1]
Caddy :443  (TLS termination, tls internal)
      ↓  [plain HTTP]
translator.py :18080  (routing + format conversion)
      │
      ├─ GET  /v1/models           → fake model list (no upstream call)
      ├─ POST /v1/chat/completions → convert to Anthropic → /v1/messages → convert back
      └─ POST /v1/messages + other → strip /api prefix + map model → passthrough
      │
      ↓
sub2api  192.168.48.12:8080  (Anthropic Messages API)
```

## Commands

```bash
./proxy.sh start    # adds hosts entry, starts translator + Caddy, trusts cert (needs sudo)
./proxy.sh stop     # stops everything, removes hosts entry
./proxy.sh status   # show running state
python3 translator.py   # run translator standalone (for debugging)
tail -f translator.log  # tail logs
```

Requires: `brew install caddy`

## Key configuration

- **Upstream**: `http://192.168.48.12:8080` (hardcoded in `translator.py` as `UPSTREAM`)
- **Translator listen**: `127.0.0.1:18080`
- **Model mapping** (`MODEL_MAP` in `translator.py`): accepts both dot and dash style names
  - `anthropic/claude-haiku-4.5` / `anthropic/claude-haiku-4-5` → `claude-haiku-4-5-20251001`
  - `anthropic/claude-sonnet-4.5` / `anthropic/claude-sonnet-4-5` → `claude-sonnet-4-5-20251001`
  - `anthropic/claude-sonnet-4.6` / `anthropic/claude-sonnet-4-6` → `claude-sonnet-4-6`
  - `anthropic/claude-opus-4.6` / `anthropic/claude-opus-4-6` → `claude-opus-4-6`
- **Path rewrite**: `/api/v1/...` → `/v1/...` (strips the `/api` prefix the Anthropic SDK appends)

## translator.py internals

Three request paths in `_dispatch()`:

1. **`GET /v1/models`** → `_handle_models()` — returns `FAKE_MODELS` list, no upstream call
2. **`POST /v1/chat/completions`** → `_handle_chat_completions()` — full format conversion:
   - `chat_to_anthropic()` converts request (messages, system, tools, tool_choice, stream)
   - `convert_messages()` handles role mapping: system→extracted, tool→user with tool_result blocks, assistant tool_calls→tool_use blocks
   - `convert_content()` handles multimodal: text passthrough, image_url→base64/url source blocks
   - Non-stream: `anthropic_to_chat()` converts response back to Chat Completions format
   - Stream: `AnthropicToChat` class converts Anthropic SSE events to Chat Completions SSE chunks, including tool_use streaming
3. **Everything else** → `_forward()` — strips `/api` prefix, maps model in JSON body, streams upstream response in 4 KB chunks

## Runtime files (gitignored)

- `.caddy.pid` / `.translator.pid` — PID files created by `proxy.sh`
- `translator.log` — stdout/stderr of the translator process

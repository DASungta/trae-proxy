# trae-proxy 产品化设计

## 概述

将 trae-proxy 从"Python 脚本 + Caddy + shell 脚本"三件套重写为 Go 单二进制，实现零依赖、跨平台、一键即用。内置 TLS 终端、DNS 劫持管理、模型映射配置化。

## 目标用户

阶段一：自己 + 公司同事（熟悉终端）。阶段二：技术社区用户。

## 核心决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 分发方式 | 单二进制 | 零依赖，去掉 Caddy 和 Python |
| 语言 | Go | 标准库强（net/http、crypto/tls），编译快，跨平台 |
| TLS 方案 | 自签 CA + 自动信任 | 用户无需手动管理证书 |
| 配置格式 | TOML | 人类可读，注释友好 |
| 跨平台 | macOS + Linux + Windows | 构建矩阵覆盖主流平台 |

## CLI 接口

```bash
# 首次初始化（一次性，需要 sudo / 管理员权限）
trae-proxy init
# → 生成本地 CA 到 ~/.config/trae-proxy/ca/
# → 安装 CA 到系统信任库
# → 生成默认 config.toml（如不存在）

# 日常使用
trae-proxy start                            # 前台运行
trae-proxy start -d                         # 后台守护进程
trae-proxy start --upstream http://x:8080   # 覆盖配置中的 upstream

# 管理
trae-proxy stop                             # 停止 + 移除 hosts 记录
trae-proxy status                           # 显示运行状态

# 清理
trae-proxy uninstall                        # 移除 CA + hosts + 配置（可选）
```

配置优先级：CLI flags > 环境变量 > config.toml > 内置默认值

首次 `start` 检测到未 `init`，自动提示用户先运行 `init`。

## 配置文件

路径：`~/.config/trae-proxy/config.toml`（`--config` 可指定其他路径）

```toml
# 上游 Anthropic Messages API 地址
upstream = "http://192.168.48.12:8080"

# HTTPS 监听地址
listen = ":443"

# 劫持的域名
hijack = "openrouter.ai"

# 模型映射：请求中的 model 名 → 上游 model 名
[models]
"anthropic/claude-sonnet-4.6" = "claude-sonnet-4-6"
"anthropic/claude-sonnet-4-6" = "claude-sonnet-4-6"
"anthropic/claude-sonnet-4.5" = "claude-sonnet-4-5-20251001"
"anthropic/claude-haiku-4.5" = "claude-haiku-4-5-20251001"
"anthropic/claude-haiku-4-5" = "claude-haiku-4-5-20251001"
"anthropic/claude-opus-4.6" = "claude-opus-4-6"
"anthropic/claude-opus-4-6" = "claude-opus-4-6"
```

**模型映射逻辑：**
1. 精确匹配 `[models]` 表
2. 未命中 → 去掉 `anthropic/` 前缀后直接透传
3. 仍无 → 原样透传

**伪造 models 列表：** `GET /v1/models` 自动从 `[models]` 的 key 去重生成，不再单独维护。

## 架构

```
Client  (ANTHROPIC_BASE_URL=https://openrouter.ai/api)
      │
      ↓  [hosts: openrouter.ai → 127.0.0.1]
trae-proxy :443  (Go net/http + crypto/tls, 自签证书)
      │
      ├─ GET  /v1/models           → 伪造模型列表（无上游调用）
      ├─ POST /v1/chat/completions → 转换为 Anthropic Messages → 上游 → 转换回
      └─ POST /v1/messages + 其他  → strip /api + model 映射 → 透传
      │
      ↓
上游 Anthropic Messages API（任意兼容实现）
```

## 请求路由

所有请求先去掉 `/api` 前缀，然后分发：

### GET /v1/models
从配置的 `[models]` keys 去重生成 OpenRouter 格式模型列表。无上游调用。

### POST /v1/chat/completions
完整的 Chat Completions ↔ Anthropic Messages 双向转换：

**请求方向（Chat → Anthropic）：**
- messages: system 角色提取为顶层 `system` 字段
- messages: tool role → user role with `tool_result` content blocks
- messages: assistant tool_calls → `tool_use` content blocks
- content: 纯文本直传；`image_url` → `base64` / `url` source blocks
- tools: OpenAI function format → Anthropic tool format
- tool_choice: `required` → `any`, `none` → `none`, function → `tool`
- 透传 stream, temperature, top_p, stop

**响应方向（Anthropic → Chat）：**
- 非流式：一次性转换 content blocks → message, tool_use → tool_calls, usage 映射
- 流式 SSE：状态机逐事件转换
  - `message_start` → role delta
  - `content_block_start` (tool_use) → tool_calls delta with id + name
  - `content_block_delta` (text_delta) → content delta
  - `content_block_delta` (input_json_delta) → tool_calls arguments delta
  - `message_delta` → finish_reason
  - `message_stop` → `[DONE]`

### 其他路径（透传）
- 去掉 `/api` 前缀
- JSON body 中 model 字段按配置映射
- 转发头：Authorization, x-api-key, anthropic-version, anthropic-beta, Content-Type, Accept
- 4KB chunk 流式传回，不缓冲

### 错误处理
- 上游 HTTP 错误：原样透传 status code + body
- 上游不可达：返回 502 + JSON `{"error": {"message": "upstream unreachable: <addr>", "type": "proxy_error"}}`
- JSON 解析失败：原样透传不做转换

## TLS 自签 CA

**证书存储：** `~/.config/trae-proxy/ca/`
- `root-ca.pem` + `root-ca-key.pem` — Root CA（有效期 10 年）
- `server.pem` + `server-key.pem` — 服务端证书（CN = 配置的 hijack 域名）

**`init` 流程：**
1. 用 `crypto/x509` 生成 Root CA
2. 用 Root CA 签发服务端证书（SAN = hijack 域名）
3. 安装 Root CA 到系统信任库

**平台信任安装：**
| 平台 | 命令 |
|------|------|
| macOS | `security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain root-ca.pem` |
| Linux | 复制到 `/usr/local/share/ca-certificates/` + `update-ca-certificates` |
| Windows | `certutil -addstore -f "ROOT" root-ca.pem` |

**hijack 域名变更时：** `init` 检测到域名与已签发证书不匹配，重新签发服务端证书。

## DNS 劫持

**hosts 文件路径：**
| 平台 | 路径 |
|------|------|
| macOS / Linux | `/etc/hosts` |
| Windows | `C:\Windows\System32\drivers\etc\hosts` |

**操作：**
- `start` 写入 `127.0.0.1 <hijack> # trae-proxy`，按标记 `# trae-proxy` 定位
- `stop` 按标记删除该行
- `start` 前检测已存在则跳过
- macOS 额外执行 `dscacheutil -flushcache` + `killall -HUP mDNSResponder`

**sudo 策略：**
- 只有 `init`（CA 信任安装）和 `start/stop`（hosts 修改）需要提权
- 程序用 `os/exec` 调用 `sudo`（Unix）或检测管理员权限（Windows），不要求整个进程以 root 运行

## 项目结构

```
trae-proxy/
├── cmd/trae-proxy/
│   └── main.go              # CLI 入口（cobra），子命令 init/start/stop/status/uninstall
├── internal/
│   ├── config/
│   │   └── config.go        # TOML 解析、默认值、优先级合并
│   ├── proxy/
│   │   ├── server.go        # HTTPS server 启动、路由分发
│   │   ├── forward.go       # 通用透传（strip /api、model 映射、流式转发）
│   │   ├── chat.go          # Chat Completions ↔ Anthropic Messages 转换
│   │   ├── chat_stream.go   # Anthropic SSE → Chat Completions SSE 状态机
│   │   └── models.go        # GET /v1/models
│   ├── tls/
│   │   └── ca.go            # CA 生成、证书签发、系统信任库安装/卸载
│   └── hosts/
│       └── hosts.go         # /etc/hosts 增删（按 runtime.GOOS 分派路径）
├── config.example.toml      # 示例配置（init 时复制为默认配置）
├── go.mod
├── go.sum
├── Makefile                  # build / build-all / install
└── CLAUDE.md
```

**Go 依赖：**
- `github.com/spf13/cobra` — CLI 框架
- `github.com/BurntSushi/toml` — TOML 解析
- 其余全用标准库

## 守护进程模式

`trae-proxy start -d` 后台运行时：
- PID 写入 `~/.config/trae-proxy/trae-proxy.pid`
- 日志输出到 `~/.config/trae-proxy/trae-proxy.log`（追加模式）
- `stop` 读取 PID 文件发送 SIGTERM（Unix）或 TerminateProcess（Windows）
- `status` 读取 PID 文件检查进程是否存活，显示 upstream、hijack 域名、监听地址

## 构建

```bash
make build                    # 当前平台
make build-all                # darwin-arm64, darwin-amd64, linux-amd64, windows-amd64
make install                  # go install → $GOPATH/bin
```

**后续分发（Phase 2）：**
- GitHub Releases（goreleaser 自动构建多平台二进制 + checksum）
- Homebrew tap
- Windows: scoop 或直接下载 .exe

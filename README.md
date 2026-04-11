# trae-proxy

将 Claude Code / Trae 等 AI 编程工具的 API 请求透明代理到任意 Anthropic Messages API 兼容的上游服务。单二进制，零依赖，跨平台，一键启动。

## 它解决什么问题

Claude Code、Trae 等工具通过 `ANTHROPIC_BASE_URL` 环境变量指定 API 地址。但有些场景下，你需要将请求转发到自己部署的中转服务（如 sub2api），同时处理以下差异：

- **协议转换**：部分客户端发送 OpenAI Chat Completions 格式，上游只接受 Anthropic Messages 格式
- **模型名映射**：客户端使用 `anthropic/claude-sonnet-4.6`，上游需要 `claude-sonnet-4-6`
- **TLS 证书**：HTTPS 请求需要受信任的证书，但目标域名指向 localhost

trae-proxy 通过 DNS 劫持 + TLS 自签 + 协议转换，让这一切对客户端完全透明。

## 工作原理

```
Claude Code / Trae  (ANTHROPIC_BASE_URL=https://openrouter.ai/api)
      │
      ↓  /etc/hosts: openrouter.ai → 127.0.0.1
trae-proxy :443  (内置 TLS，自签证书)
      │
      ├─ GET  /v1/models           → 返回配置中的模型列表（无上游调用）
      ├─ POST /v1/chat/completions → 转换为 Anthropic Messages → 上游 → 转换回
      └─ POST /v1/messages + 其他  → 去掉 /api 前缀 + 模型名映射 → 透传
      │
      ↓
上游 Anthropic Messages API（任意兼容实现）
```

**核心流程：**

1. `/etc/hosts` 将 `openrouter.ai` 指向 `127.0.0.1`
2. trae-proxy 在 443 端口用自签证书接收 HTTPS 请求
3. 去掉 Anthropic SDK 添加的 `/api` 路径前缀
4. 根据路由选择透传或协议转换
5. 将请求转发到上游，流式响应实时回传

## 快速开始

### 安装

**一键安装**（推荐，macOS / Linux）：

```bash
curl -fsSL https://raw.githubusercontent.com/DASungta/tare-proxy/master/install.sh | sudo bash
```

安装脚本自动检测系统和架构，下载对应的预编译二进制到 `/usr/local/bin`。

指定版本：`VERSION=v0.1.0 curl -fsSL ... | sudo bash`

<details>
<summary>其他安装方式</summary>

**手动下载**

从 [Releases](https://github.com/DASungta/tare-proxy/releases/latest) 页面下载对应平台的文件：

| 平台 | 文件名 |
|------|--------|
| macOS Apple Silicon | `trae-proxy-darwin-arm64` |
| macOS Intel | `trae-proxy-darwin-amd64` |
| Linux x86_64 | `trae-proxy-linux-amd64` |
| Windows x86_64 | `trae-proxy-windows-amd64.exe` |

下载后：

```bash
chmod +x trae-proxy-darwin-arm64
sudo mv trae-proxy-darwin-arm64 /usr/local/bin/trae-proxy
```

**从源码编译**（需要 Go 1.21+，适合二次开发）：

```bash
git clone https://github.com/DASungta/tare-proxy.git
cd trae-proxy
make install    # 编译并安装到 /usr/local/bin
```

</details>

### 初始化（首次使用，一次性操作）

```bash
sudo trae-proxy init
```

这一步会：
- 生成本地 Root CA 和服务端证书（存放在 `~/.config/trae-proxy/ca/`）
- 将 Root CA 安装到系统信任库（需要管理员权限）
- 创建默认配置文件 `~/.config/trae-proxy/config.toml`

### 启动

```bash
# 前台运行（Ctrl+C 停止）
sudo trae-proxy start

# 后台守护进程
sudo trae-proxy start -d

# 指定上游地址（覆盖配置文件）
sudo trae-proxy start --upstream http://your-server:8080
```

启动后，在 Claude Code / Trae 中配置环境变量：

```json
{
  "ANTHROPIC_BASE_URL": "https://openrouter.ai/api",
  "ANTHROPIC_MODEL": "anthropic/claude-sonnet-4.6"
}
```

### 停止

```bash
sudo trae-proxy stop
```

会同时停止守护进程并移除 `/etc/hosts` 条目。

### 查看状态

```bash
trae-proxy status
```

输出示例：

```
=== trae-proxy status ===

[hosts] ✓ openrouter.ai → 127.0.0.1
[daemon] ✓ running (pid 12345)

Upstream: http://192.168.48.12:8080
Listen:   :443
Hijack:   openrouter.ai
Models:   8 mappings
```

### 卸载

```bash
# 移除 CA 信任和 hosts 条目
sudo trae-proxy uninstall

# 同时删除配置目录
sudo trae-proxy uninstall --purge
```

## 各操作系统安装指南

### macOS

支持 Apple Silicon (M1/M2/M3/M4) 和 Intel 两种架构，安装脚本自动检测。

**安装：**

```bash
curl -fsSL https://raw.githubusercontent.com/DASungta/tare-proxy/master/install.sh | sudo bash
```

**初始化：**

```bash
sudo trae-proxy init
```

CA 证书会通过 `security add-trusted-cert` 安装到系统钥匙串（System Keychain）。首次执行时系统可能弹出密码确认框。

**启动：**

```bash
sudo trae-proxy start -d
```

macOS 会自动刷新 DNS 缓存（`dscacheutil -flushcache` + `killall -HUP mDNSResponder`），无需手动操作。

**注意事项：**
- 监听 443 端口和修改 `/etc/hosts` 需要 `sudo`
- 如果浏览器仍提示证书不信任，尝试重启浏览器或执行 `sudo trae-proxy init` 重新安装 CA

---

### Linux

目前支持 x86_64 (amd64) 架构。

**安装：**

```bash
curl -fsSL https://raw.githubusercontent.com/DASungta/tare-proxy/master/install.sh | sudo bash
```

**初始化：**

```bash
sudo trae-proxy init
```

CA 证书会复制到 `/usr/local/share/ca-certificates/` 并执行 `update-ca-certificates`。

> 如果系统没有 `update-ca-certificates` 命令（如 RHEL/CentOS），需要手动将 `~/.config/trae-proxy/ca/root-ca.pem` 复制到 `/etc/pki/ca-trust/source/anchors/` 并执行 `update-ca-trust`。

**启动：**

```bash
sudo trae-proxy start -d
```

**注意事项：**
- 某些发行版的 DNS 缓存（如 systemd-resolved）可能需要手动刷新：`sudo systemd-resolve --flush-caches`
- 如果使用 snap 版本的浏览器（如 Ubuntu 的 Firefox），它有独立的证书存储，可能不信任系统 CA。建议使用 apt 安装的浏览器

---

### Windows

Windows 不支持一键安装脚本，需要手动安装。

**安装：**

1. 从 [Releases](https://github.com/DASungta/tare-proxy/releases/latest) 页面下载 `trae-proxy-windows-amd64.exe`
2. 重命名为 `trae-proxy.exe`，放到你喜欢的目录（如 `C:\tools\`）
3. 将该目录添加到系统 `PATH` 环境变量

**初始化（以管理员身份运行 PowerShell）：**

```powershell
trae-proxy init
```

CA 证书通过 `certutil -addstore -f "ROOT"` 安装到 Windows 证书存储。系统会弹出安全警告，选择"是"确认安装。

**启动（以管理员身份运行 PowerShell）：**

```powershell
trae-proxy start -d
```

**停止：**

```powershell
trae-proxy stop
```

**注意事项：**
- 所有命令都需要**管理员权限**（右键 PowerShell → 以管理员身份运行）
- hosts 文件路径为 `C:\Windows\System32\drivers\etc\hosts`
- Windows Defender 可能在首次运行时弹出防火墙提示，需允许 trae-proxy 监听网络
- 守护进程模式通过 `TerminateProcess` 停止，而非 Unix 的 SIGTERM

## 配置

配置文件路径：`~/.config/trae-proxy/config.toml`

```toml
# 上游 Anthropic Messages API 地址
upstream = "http://192.168.48.12:8080"

# HTTPS 监听地址
listen = ":443"

# 劫持的域名（写入 /etc/hosts）
hijack = "openrouter.ai"

# 模型名映射：请求中的名称 → 上游名称
[models]
"anthropic/claude-sonnet-4.6" = "claude-sonnet-4-6"
"anthropic/claude-sonnet-4-6" = "claude-sonnet-4-6"
"anthropic/claude-sonnet-4.5" = "claude-sonnet-4-5-20251001"
"anthropic/claude-haiku-4.5"  = "claude-haiku-4-5-20251001"
"anthropic/claude-opus-4.6"   = "claude-opus-4-6"
```

### 配置优先级

CLI flags > 环境变量 > config.toml > 内置默认值

### 模型名映射规则

三级回退机制：

1. **精确匹配**：在 `[models]` 表中查找完全匹配
2. **去前缀**：去掉 `anthropic/` 前缀后直接使用（如 `anthropic/claude-new` → `claude-new`）
3. **原样透传**：以上都不匹配则保持原名

这意味着新增模型通常无需修改配置。

## 请求路由

所有请求先去掉 `/api` 路径前缀（Anthropic SDK 会自动追加），然后按以下规则路由：

### GET /v1/models

返回配置文件 `[models]` 中所有 key 组成的模型列表，格式兼容 OpenRouter。不会调用上游。用于客户端（如 Trae）验证模型 ID。

### POST /v1/chat/completions

**完整的 Chat Completions ↔ Anthropic Messages 双向转换。**

请求方向（Chat → Anthropic）：

| Chat Completions | Anthropic Messages |
|---|---|
| `messages[role=system]` | 提取为顶层 `system` 字段 |
| `messages[role=tool]` | 合并连续条目为 `user` + `tool_result` 块 |
| `messages[role=assistant].tool_calls` | 转换为 `tool_use` 内容块 |
| `content: [{type: "image_url", ...}]` | 转换为 `image` source 块（base64 或 URL） |
| `tools` (OpenAI function 格式) | 转换为 Anthropic tool 格式 |
| `tool_choice: "required"` | `{type: "any"}` |

响应方向（Anthropic → Chat）：

- **非流式**：一次性转换 content blocks、tool_use → tool_calls、usage、stop_reason
- **流式 SSE**：状态机逐事件转换，支持 text streaming 和 tool_use 参数流式传输

### 其他路径（透传）

去掉 `/api` 前缀 → 映射 JSON body 中的 model 字段 → 转发所有标准头 → 4KB chunk 流式回传。

## 技术细节

### 项目结构

```
trae-proxy/
├── cmd/trae-proxy/main.go           # CLI 入口（cobra）
├── internal/
│   ├── config/config.go             # TOML 配置、模型映射
│   ├── proxy/
│   │   ├── server.go                # HTTPS server、路由分发
│   │   ├── handler.go               # Chat Completions HTTP handler
│   │   ├── chat.go                  # 请求/响应格式转换
│   │   ├── chat_stream.go           # SSE 流式转换状态机
│   │   ├── convert.go               # 消息/内容/工具 转换函数
│   │   ├── forward.go               # 通用透传代理
│   │   ├── models.go                # 伪造模型列表端点
│   │   └── util.go                  # UUID 生成
│   ├── tls/ca.go                    # CA 生成、证书签发、系统信任
│   ├── hosts/hosts.go               # /etc/hosts 管理（跨平台）
│   └── daemon/                      # 守护进程模式（Unix/Windows）
│       ├── daemon.go                # 共享逻辑
│       ├── daemon_unix.go           # Unix: Setsid + SIGTERM
│       └── daemon_windows.go        # Windows: TerminateProcess
├── config.example.toml              # 示例配置
├── Makefile                         # 构建脚本
└── go.mod
```

### TLS 证书管理

`trae-proxy init` 使用 Go `crypto/x509` 标准库生成证书链：

1. **Root CA**：ECDSA P-256，10 年有效期，存储于 `~/.config/trae-proxy/ca/root-ca.pem`
2. **Server Cert**：ECDSA P-256，1 年有效期，SAN = 配置的 hijack 域名

系统信任安装按平台分派：

| 平台 | 命令 |
|------|------|
| macOS | `security add-trusted-cert` → System Keychain |
| Linux | 复制到 `/usr/local/share/ca-certificates/` + `update-ca-certificates` |
| Windows | `certutil -addstore -f "ROOT"` |

当 hijack 域名变更时，`init` 会自动检测 SAN 不匹配并重新签发服务端证书。

### DNS 劫持

通过修改系统 hosts 文件实现，标记 `# trae-proxy` 用于识别和清理：

```
127.0.0.1 openrouter.ai # trae-proxy
```

macOS 额外执行 DNS 缓存刷新（`dscacheutil -flushcache` + `killall -HUP mDNSResponder`）。

### SSE 流式转换

`StreamConverter` 是一个状态机，逐事件将 Anthropic SSE 流转换为 Chat Completions SSE 流：

```
Anthropic event              → Chat Completions chunk
─────────────────────────────────────────────────────
message_start                → {delta: {role: "assistant"}}
content_block_start(tool_use)→ {delta: {tool_calls: [{id, name}]}}
content_block_delta(text)    → {delta: {content: "..."}}
content_block_delta(json)    → {delta: {tool_calls: [{arguments: "..."}]}}
message_delta                → {finish_reason: "stop"|"tool_calls"}
message_stop                 → data: [DONE]
```

支持 tool_use 的流式参数传输，通过 `index → call_id` 映射维护多工具并发状态。

### 守护进程模式

`trae-proxy start -d` 通过重新执行自身实现后台运行：

1. 父进程打开日志文件（`~/.config/trae-proxy/trae-proxy.log`，追加模式）
2. 以去掉 `-d` 参数的方式启动子进程，stdout/stderr 重定向到日志
3. Unix 设置 `Setsid` 使子进程脱离终端会话
4. 写入 PID 文件后父进程退出

`trae-proxy stop` 读取 PID 文件，发送 SIGTERM（Unix）或 TerminateProcess（Windows）。

### 错误处理

| 场景 | 行为 |
|------|------|
| 上游返回 HTTP 错误 | 原样透传 status code + body |
| 上游不可达 | 返回 502 + `{"error": {"message": "upstream unreachable: <addr>", "type": "proxy_error"}}` |
| 请求 JSON 解析失败 | 原样透传不做转换 |
| 响应 JSON 解析失败 | 原样透传不做转换 |

### 转发的请求头

```
Authorization, Content-Type, x-api-key,
anthropic-version, anthropic-beta, Accept
```

如果请求中没有 `anthropic-version`，默认使用 `2023-06-01`。

## 依赖

| 依赖 | 用途 |
|------|------|
| [cobra](https://github.com/spf13/cobra) | CLI 框架 |
| [toml](https://github.com/BurntSushi/toml) | 配置文件解析 |
| Go 标准库 | net/http、crypto/tls、crypto/x509、encoding/json 等 |

无运行时依赖。编译后为单个静态二进制文件。

## 注意事项

- 代理运行期间，`openrouter.ai`（或配置的 hijack 域名）在本机解析到 localhost，**真实的 OpenRouter 服务不可访问**
- 监听 443 端口和修改 `/etc/hosts` 需要管理员权限
- 上游必须兼容 Anthropic Messages API（`POST /v1/messages`）
- 自签 CA 仅影响本机，不会影响其他设备

## 许可证

MIT

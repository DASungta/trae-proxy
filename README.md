# Trae Proxy

让 Trae 接入任意 Anthropic 或 OpenAI 兼容的自定义模型端点。

**特点：**

- 单二进制，零依赖，跨平台，一键启动
- 无需 `sudo`：macOS 通过原生授权对话框完成系统操作，无权限污染
- 交互式向导支持方向键导航、搜索过滤，初始化体验大幅提升

**支持的上游类型：**

- 各类中转站（sub2api、one-api、new-api 等）
- 支持 Anthropic Messages API / OpenAI Completions API 的云服务（讯飞星火、京东云、无问心穹、移动云等）
- 自建反代、中转（Antigravity 等）

> **从旧版本升级？** 查看 [v0.4.0 升级指南 →](docs/upgrade-v0.4.0.md)
>
> **想看旧版本文档？** [旧版 README (v0.3.x) →](docs/README-v0.3.md)

---

## 快速开始

> macOS / Linux 用户？[1 分钟快速开始 →](docs/quick-start.md)
>
> Windows 用户？[2 分钟快速开始 →](docs/win-quick-start.md)

新版本已经解除限制！~~**注意：一定要在 trae-proxy 没有 start 的时候，在 Trae 添加模型、编辑模型，否则会一直提示错误的模型名称！**~~ 

---

## 安装

### macOS / Linux（一键安装）

```bash
curl -fsSL https://raw.githubusercontent.com/DASungta/trae-proxy/main/install.sh | sudo bash
```

> v0.4.0 起不再需要 `sudo` 安装脚本。安装后，`init` 步骤会在需要时弹出系统授权对话框。

<details>
<summary>macOS 注意事项</summary>

- 支持 Apple Silicon（M 系列）和 Intel，脚本自动检测架构
- `init` 时 CA 证书和 hosts 修改通过 macOS 原生授权对话框获取权限，**不需要 `sudo`**
- 如果对话框未弹出，确认二进制路径不在受限目录（如 Downloads）

</details>

<details>
<summary>Linux 注意事项</summary>

- 目前支持 x86_64（amd64）架构
- Linux 平台 CA 安装和 hosts 修改仍需 `sudo`
- RHEL/CentOS 无 `update-ca-certificates`：手动将 `~/.config/trae-proxy/ca/root-ca.pem` 复制到 `/etc/pki/ca-trust/source/anchors/` 并执行 `update-ca-trust`

</details>

### Windows（手动安装）

1. 从 [Releases](https://github.com/DASungta/trae-proxy/releases/latest) 页面下载 `trae-proxy-windows-amd64.exe`
2. 重命名为 `trae-proxy.exe`，放到任意目录（如 `C:\tools\`）
3. 将该目录添加到系统 `PATH` 环境变量

所有命令需在**管理员身份的 PowerShell** 中运行（右键 → 以管理员身份运行）。

<details>
<summary>Windows 注意事项</summary>

- `init` 时 CA 证书通过 `certutil -addstore -f "ROOT"` 安装，系统会弹出安全警告，选"是"确认
- Windows Defender 首次运行时可能弹出防火墙提示，需允许 trae-proxy 监听网络
- 不支持 osascript 授权对话框，CA 安装仍需管理员权限

</details>

<details>
<summary>从源码编译（需要 Go 1.21+）</summary>

```bash
git clone https://github.com/DASungta/trae-proxy.git
cd trae-proxy
make install    # 编译并安装到 /usr/local/bin
```

</details>

---

## 初始化

```bash
trae-proxy init
```

启动交互式向导，依次完成：

1. **上游服务地址** — 输入中转服务 / API 端点 URL（自动校验格式）
2. **上游协议** — 方向键选择 `anthropic` 或 `openai`
3. **模型映射** — 从 Trae 支持的模型列表中搜索选择，并填写上游实际接受的模型名

向导结束后自动：
- 生成本地 Root CA 和服务端证书（存放在 `~/.config/trae-proxy/ca/`）
- 通过系统授权对话框（macOS）将 Root CA 安装到系统信任库
- 将配置写入 `~/.config/trae-proxy/config.toml`

> 跳过向导使用默认配置：`trae-proxy init --yes`

---

## 配置 Trae

1. 打开 Trae → 设置 → 模型 → 添加模型
2. 服务商选择 **OpenRouter**（默认劫持域名）
3. 选择对应模型（如 Anthropic: Claude Sonnet 4.5）
4. 填入你上游服务的 API 密钥
5. 点击添加，稍等片刻即可在自定义模型列表中看到

![添加模型-选择服务商](./docs/pics/add-model-select-provider.png)

![添加模型-填写密钥](./docs/pics/add-model-enter-api-key.png)

---

## 启动 / 停止

```bash
# 前台运行（Ctrl+C 停止）
trae-proxy start

# 后台守护进程
trae-proxy start -d

# 停止守护进程并清理 hosts
trae-proxy stop

# 重启（重新加载配置）
trae-proxy restart

# 查看状态
trae-proxy status
```

macOS 上 `start` 时修改 `/etc/hosts` 会弹出系统授权对话框，属于正常行为。

---

## 命令总览

| 命令 | 说明 | 常用标志 |
|---|---|---|
| `init` | 交互式向导：配置上游、协议、模型映射，生成 CA 并安装信任 | `-y` 跳过向导 |
| `start` | 启动代理（写入 hosts + 监听 443） | `-d` 后台，`--upstream`，`--listen`，`--config`，`-l`/`--log-level`，`--log-body` |
| `stop` | 停止守护进程并移除 hosts 条目 | — |
| `restart` | 重启守护进程，重新加载配置 | 同 `start`（不含 `-d`） |
| `status` | 显示 hosts / 守护进程 / 上游 / 模型映射数 | — |
| `update` | 从 GitHub Releases 自更新（macOS/Linux） | `--version`，`--force` |
| `uninstall` | 移除 CA 信任、hosts 条目、二进制本体 | `-y` 跳过交互 |

---

## 配置文件

路径：`~/.config/trae-proxy/config.toml`

```toml
# 上游服务地址，路径支持完整端点地址或基地址
#
# 基地址（大多数中转站）：
#   upstream = "http://your-relay:8080"
#   upstream = "https://ai.bayesdl.com/api/maas/"          # 移动云（OpenAI）
#   upstream = "https://modelservice.jdcloud.com/coding/openai"    # 京东云（OpenAI）
#   upstream = "https://modelservice.jdcloud.com/coding/anthropic" # 京东云（Anthropic）
#
# 完整端点地址（适用于百度千帆等路径非标准的服务）：
#   upstream = "https://qianfan.baidubce.com/v2/coding/chat/completions"  # 千帆（OpenAI）
#   upstream = "https://qianfan.baidubce.com/anthropic/coding/v1/messages" # 千帆（Anthropic）
#
# 填写完整端点时，trae-proxy 会忽略 upstream_protocol 的路径拼接，
# 直接将请求转发到该 URL（仅做协议格式转换）。
upstream = "http://your-server:8080"

# 上游协议：
#   "anthropic" — 将 OpenAI Chat Completions 转换为 Anthropic Messages 后转发
#   "openai"    — 直接透传，仅映射模型名
upstream_protocol = "anthropic"

# 本地监听地址（需要管理员权限）
listen = ":443"

# 劫持的域名（写入 /etc/hosts），默认 openrouter.ai
hijack = "openrouter.ai"

# 日志级别：error | warn | info（默认）| debug | trace
log_level = "info"

# true 时在 trace 级别打印完整请求/响应体（含敏感信息，慎用）
log_body = false

# true 时 GET /v1/models 透传到真实域名而非返回本地列表
# real_models = false

# 模型名映射：Trae 发送的模型名 → 上游实际接受的模型名
# 三级回退：精确匹配 → 去掉 "anthropic/"/"openai/" 前缀 → 原样透传
[models]
"anthropic/claude-sonnet-4.6" = "claude-sonnet-4.6"
"anthropic/claude-opus-4.6" = "claude-opus-4.6"
"anthropic/claude-haiku-4.5" = ""
"openai/gpt-oss-120b" = ""
"openai/gpt-5.4" = ""
"openai/gpt-5.4-mini" = ""
"google/gemini-3.1-pro-preview" = ""
"google/gemini-3.1-flash-lite-preview" = ""
"minimax/minimax-m2.7" = ""
"qwen/qwen3-coder-next" = ""
"z-ai/glm-5" = ""
```

### 完整端点地址示例（百度千帆）

千帆的 API 路径非标准（如 `/v2/coding/chat/completions`），需填写完整 URL：

```toml
# 千帆 OpenAI 兼容接口
upstream = "https://qianfan.baidubce.com/v2/coding/chat/completions"
upstream_protocol = "openai"

# 或千帆 Anthropic 接口
# upstream = "https://qianfan.baidubce.com/anthropic/coding/v1/messages"
# upstream_protocol = "anthropic"
```

填写完整端点后，trae-proxy 识别路径后缀（`/chat/completions` 或 `/v1/messages`）并直接转发，无需手动拼接路径。

### 配置优先级

CLI flags > 环境变量（`TRAE_LOG_LEVEL`、`TRAE_LOG_BODY`）> config.toml > 内置默认值

---

## 更新

```bash
# 更新到最新版本
trae-proxy update

# 更新到指定版本
trae-proxy update --version v0.3.3

# 强制重装（版本相同时也执行）
trae-proxy update --force
```

> Windows 暂不支持自更新，请手动从 Releases 页面下载。

---

## 卸载

```bash
# 交互式卸载（移除 CA、hosts、二进制，询问是否删除配置目录）
trae-proxy uninstall

# 自动确认，连同配置目录一并删除
trae-proxy uninstall -y
```

---

## 工作原理

```
Trae IDE
  │ HTTPS → https://openrouter.ai/api
  ↓
  /etc/hosts: openrouter.ai → 127.0.0.1
  ↓
trae-proxy :443（自签 TLS）
  ├── GET  /v1/models           → 返回本地模型列表（或透传真实域名）
  ├── POST /v1/chat/completions
  │     ├── anthropic 模式 → 转换为 Anthropic Messages → 上游
  │     └── openai 模式   → 模型名映射 → 直接透传
  └── 其他路径               → 透传到上游
  ↓
上游 API 服务
```

**关键机制：**
1. `/etc/hosts` 将 `openrouter.ai` 指向 `127.0.0.1`，拦截 Trae 的 API 请求
2. 本地 TLS 证书（由安装到系统信任库的自签 CA 签发）使 Trae 信任本地代理
3. 协议转换或透传后，将请求发到用户配置的上游
4. 流式响应（SSE）实时回传，延迟极低

---

## 日志级别

| 级别 | 输出内容 |
|---|---|
| `error` | 只记录错误（上游 5xx、TLS 握手失败等） |
| `warn` | + 降级行为（模型映射未命中等） |
| `info`（默认）| + 启动信息 + 每个请求一行摘要 |
| `debug` | + 请求结构化字段（URL、脱敏 headers、model、stream） |
| `trace` | + 完整原始工件（客户端请求 / 代理内部 / 上游请求 / 上游响应） |

`Authorization` 和 `x-api-key` 在任何级别下都打码为 `[REDACTED]`。

---

## 依赖

| 依赖 | 用途 |
|---|---|
| [cobra](https://github.com/spf13/cobra) | CLI 框架 |
| [toml](https://github.com/BurntSushi/toml) | 配置解析 |
| [survey/v2](https://github.com/AlecAivazis/survey) | 交互式向导（终端模式） |
| Go 标准库 | net/http、crypto/tls、crypto/x509 等 |

编译后为单个静态二进制，无运行时依赖。

---

## 更新计划

- [x] 支持 OpenAI Chat Completions
- [x] `init` 交互式命令行向导
- [x] macOS 无 sudo 运行（原生授权对话框）
- [x] 向导支持方向键 / 搜索过滤（survey/v2）
- [x] `/v1/models` 返回 OpenRouter 兼容格式
- [ ] 支持一键导入 Trae 配置
- [ ] 支持同时代理多个上游服务

---

## 注意事项

- 代理运行期间，`openrouter.ai`（或配置的 hijack 域名）在本机解析到 localhost，**真实 OpenRouter 服务不可访问**
- macOS 下 init/start/uninstall 涉及系统操作时会弹出授权对话框，属于正常行为
- Windows 和 Linux 仍需管理员权限（`sudo`）
- 自签 CA 仅影响本机，不会影响其他设备

---

## 许可证

MIT

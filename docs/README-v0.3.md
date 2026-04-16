# Trae Proxy — 旧版文档 (v0.3.x)

> **你正在查看的是 v0.3.x 的旧版文档。**
>
> 最新版本请访问 [README.md →](../README.md)
>
> 从 v0.3.x 升级到 v0.4.0？查看 [升级指南 →](upgrade-v0.4.0.md)

---

让 Trae 接入任意 Anthropic 或 OpenAI 兼容的自定义模型端点。

**特点：**

- 单二进制，零依赖，跨平台，一键启动

**支持的上游类型：**

- 各类中转站（sub2api、one-api、new-api 等）
- 支持 Anthropic Messages API / OpenAI Completions API 的云服务（讯飞星火、京东云、无问心穹、移动云等）
- 自建反代、中转（Antigravity 等）

---

## 快速开始

**注意：一定要在 trae-proxy 没有 start 的时候，在 Trae 添加模型、编辑模型，否则会一直提示错误的模型名称！**

---

## 安装

### macOS / Linux（一键安装）

```bash
sudo curl -fsSL https://raw.githubusercontent.com/DASungta/trae-proxy/main/install.sh | bash
```

> v0.3.x 安装脚本需要 `sudo`。

<details>
<summary>macOS 注意事项</summary>

- 支持 Apple Silicon（M 系列）和 Intel，脚本自动检测架构
- `init` 时需要 `sudo` 安装 CA 证书和修改 hosts

</details>

<details>
<summary>Linux 注意事项</summary>

- 目前支持 x86_64（amd64）架构
- CA 安装和 hosts 修改需 `sudo`
- RHEL/CentOS 无 `update-ca-certificates`：手动将 `~/.config/trae-proxy/ca/root-ca.pem` 复制到 `/etc/pki/ca-trust/source/anchors/` 并执行 `update-ca-trust`

</details>

### Windows（手动安装）

1. 从 [Releases](https://github.com/DASungta/trae-proxy/releases/latest) 页面下载 `trae-proxy-windows-amd64.exe`
2. 重命名为 `trae-proxy.exe`，放到任意目录（如 `C:\tools\`）
3. 将该目录添加到系统 `PATH` 环境变量

所有命令需在**管理员身份的 PowerShell** 中运行（右键 → 以管理员身份运行）。

---

## 初始化

```bash
sudo trae-proxy init
```

初始化会：
- 生成本地 Root CA 和服务端证书（存放在 `~/.config/trae-proxy/ca/`）
- 将 Root CA 安装到系统信任库（需要 `sudo`）
- 创建默认配置文件 `~/.config/trae-proxy/config.toml`

---

## 配置 Trae

1. 打开 Trae → 设置 → 模型 → 添加模型
2. 服务商选择 **OpenRouter**（默认劫持域名）
3. 选择对应模型（如 Anthropic: Claude Sonnet 4.5）
4. 填入你上游服务的 API 密钥
5. 点击添加，稍等片刻即可在自定义模型列表中看到

---

## 启动 / 停止

```bash
# 前台运行（Ctrl+C 停止）
sudo trae-proxy start

# 后台守护进程
sudo trae-proxy start -d

# 停止守护进程并清理 hosts
sudo trae-proxy stop

# 重启（重新加载配置）
sudo trae-proxy restart

# 查看状态
trae-proxy status
```

---

## 命令总览

| 命令 | 说明 | 常用标志 |
|---|---|---|
| `init` | 生成 CA 并安装信任、创建默认配置 | `-y` 跳过确认 |
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
# 上游 API 地址
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

# 模型名映射：Trae 发送的模型名 → 上游实际接受的模型名
[models]
"anthropic/claude-sonnet-4.5" = "claude-sonnet-4.6"
"anthropic/claude-opus-4.1" = "claude-opus-4.6"
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
  ├── GET  /v1/models           → 返回本地模型列表
  ├── POST /v1/chat/completions
  │     ├── anthropic 模式 → 转换为 Anthropic Messages → 上游
  │     └── openai 模式   → 模型名映射 → 直接透传
  └── 其他路径               → 透传到上游
  ↓
上游 API 服务
```

---

## 注意事项

- 代理运行期间，`openrouter.ai`（或配置的 hijack 域名）在本机解析到 localhost，**真实 OpenRouter 服务不可访问**
- macOS / Linux 下所有系统操作（init/start/uninstall）需要 `sudo`
- 自签 CA 仅影响本机，不会影响其他设备

---

## 许可证

MIT

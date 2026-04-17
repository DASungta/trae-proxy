# Changelog

All notable changes to this project will be documented in this file.

## [v0.4.5] - 2026-04-17

### Bug Fixes

- **macOS 26 init 仍报 SecTrustSettingsSetTrustSettings 授权失败**：v0.4.3 的 `InstallCA` 通过 osascript "with administrator privileges" 调用 `security add-trusted-cert`，但 macOS 15+/26 的 Authorization Services 在 `SecTrustSettingsSetTrustSettings` 写入系统域 trust setting 时需要再次出示用户交互式授权对话框——osascript 子进程无用户会话上下文，无法满足此要求（`errAuthorizationInteractionNotAllowed -60007`）。改为直接 `sudo security add-trusted-cert/remove-trusted-cert`，由用户在终端输入登录密码，Authorization Services 可复用 sudo 的授权会话。

### Features

- **兼容国内版 Trae 老模型**：`DefaultConfig().Models` 新增 12 个国内版 Trae 仍在使用的老模型 id（`anthropic/claude-sonnet-4.5`、`claude-3.7-sonnet`、`openai/gpt-5`、`google/gemini-3-pro-preview` 等），同时修正旧版拼写错误 `gemini-3-pro-perview → gemini-3-pro-preview`。`/v1/models` fake 数据现在同时返回 11 个新模型（海外版 Trae）和 12 个老模型（国内版 Trae）共 23 个 id。
- **`writeDefaultConfig` 去硬编码**：从 `DefaultConfig().Models` 动态生成 `[models]` 配置块（按 key 排序），消除与 `DefaultConfig` 的同步漂移风险。

---

## [v0.4.4] - 2026-04-17

### Bug Fixes

- **`update` 命令 permission denied**：二进制安装在 `/usr/local/bin`（root 所有）时，`update` 写临时文件和替换二进制均因权限不足失败。
  - `Download()` 改为写入系统临时目录（`os.TempDir()`），彻底绕开安装目录写权限问题
  - `Replace()` 失败且错误为 permission denied 时，自动通过 osascript（macOS GUI）或 sudo（SSH/Linux）提权执行 `mv + chmod 755`，与 `uninstall` 的处理模式一致

---

## [v0.4.3] - 2026-04-17

### Bug Fixes

- **macOS 26 / Network.framework TLS 握手 EOF**：v0.4.1 引入的"无管理员权限安装 CA"在 macOS 14/15/26 的 Network.framework / ATS 校验下失效，导致 Trae IDE 请求时服务端日志出现 `TLS handshake error: EOF`。根因：CA 仅写入用户 login keychain，而 Electron（硬化运行时）的网络栈只读取系统 keychain 的信任锚点。  
  已恢复为通过 osascript 管理员授权（GUI 会话）或 sudo（SSH 会话）将 CA 写入 `/Library/Keychains/System.keychain`，并新增 `-p ssl` 策略限定，与 Apple macOS 15+ 指引对齐。
- **UninstallCA 权限**：`uninstall` 时对系统 keychain 中的 CA 调用 `security remove-trusted-cert -d` 同步补充提权，避免卸载残留。
- **leaf 证书 basicConstraints**：服务端证书补充 `BasicConstraints: CA=false` 扩展，符合 Apple "Requirements for trusted certificates" 规范，避免部分严格校验器拒绝。

### Improvements

- **证书有效期自检**：`NeedsRegeneration` 新增两条规则——剩余有效期 < 30 天或总有效期 > 398 天（超出 Apple 当前上限）时自动重签，防止在下次 `init` 前悄然失效。
- **SSH 上下文兼容**：`privilege.RunPrivileged` 在检测到 SSH 会话时（`$SSH_TTY` / `$SSH_CONNECTION`）自动切换为 `sudo sh -c`，使远程维护场景下 `init` 可正常安装 CA。
- **init 失败兜底提示**：CA 安装失败时自动打印手动修复命令，方便用户在无授权环境下自行处理。

### Breaking Changes

- `trae-proxy init` 在 macOS 上将再次弹出系统管理员授权对话框（与 v0.4.0 一致）。这是修复 macOS 26 兼容性的必要代价。

---

## [v0.4.2] - 2026-04-17

### Bug Fixes

- **uninstall 无法删除 `/usr/local/bin` 下的二进制文件**：`os.Remove` 权限不足时，macOS 通过系统授权对话框提权删除，Linux 通过 `sudo rm` 处理。

---

## [v0.4.1] - 2026-04-17

### Features

- **Windows 一键安装脚本**：新增 `install.ps1`，普通用户 PowerShell 一行命令完成安装，无需手动配置环境变量。
  - 自动下载最新版本并校验 SHA256
  - 安装到 `%LOCALAPPDATA%\trae-proxy\`，无需管理员权限
  - 自动写入用户级 PATH 并广播 `WM_SETTINGCHANGE`，新开终端即可使用
  - 支持 `$env:VERSION` 指定版本

### Bug Fixes

- **macOS 15+ CA 证书安装**：移除 `-d` 和 `-k /Library/Keychains/System.keychain` 参数，改为写入用户登录 Keychain，修复 `SecTrustSettingsSetTrustSettings` 授权报错，不再需要管理员权限。

---

## [v0.4.0] - 2026-04-17

### Features

- **macOS 免 sudo**：使用 `osascript` 原生系统授权对话框替代 `sudo`，解决 `config.toml` 被 root 占有的问题。
- **向导方向键支持**：终端环境下使用 `survey/v2` 库，支持方向键选择、关键字过滤、自定义输入。
- **`/v1/models` OpenRouter 格式**：返回包含 `canonical_slug`、`architecture`、`pricing` 等字段的 OpenRouter 兼容格式，Trae IDE 可正常识别。
- **自定义模型支持**：init 向导中填入的自定义模型会自动写入配置并出现在 `/v1/models` 列表中。
- **请求/响应日志分离**：收到请求时立即打印 `request received`（含 model、stream），响应完成后打印 `response done`（含 status、耗时）。

### Improvements

- **密码弹框大幅减少**：合并同一操作中的多次 `RunPrivileged` 调用为单次，`start` 从 6+ 次降至 2 次，`uninstall` 从 4 次降至 1 次。
- **`start` 退出时防重复清理**：使用 `sync.Once` 确保信号处理和 defer 不会重复调用 `hosts.Remove()`。
- **向导模型列表自动同步**：模型列表从 `DefaultConfig` 动态读取，不再维护两份硬编码列表。
- **向导提示信息完善**：survey 路径直接显示示例 URL、协议说明、模型用途等引导文字，无需按 `?` 查看。
- **模型选择全部展示**：Select 列表设置 `PageSize` 为列表总数，无需滚动即可看到所有选项。

### Upgrade Guide

从 v0.3.x 升级请参阅 [升级指引](docs/upgrade-v0.4.0.md)。

---

## [v0.3.3] - 2026-04-16

### Improvements

- **同步 Trae 最新模型列表**：更新默认模型映射表，与 Trae IDE 当前 OpenRouter 模型列表保持一致。
  - 新增/更新 Anthropic 模型：`anthropic/claude-sonnet-4.6`、`anthropic/claude-opus-4.6`、`anthropic/claude-haiku-4.5`
  - 新增/更新 OpenAI 模型：`openai/gpt-oss-120b`、`openai/gpt-5.4`、`openai/gpt-5.4-mini`
  - 新增 Google 模型：`google/gemini-3.1-pro-preview`、`google/gemini-3.1-flash-lite-preview`
  - 新增 MiniMax 模型：`minimax/minimax-m2.7`
  - 新增 Qwen 模型：`qwen/qwen3-coder-next`
  - 新增智谱 AI 模型：`z-ai/glm-5`
- **upstream 配置说明更新**：注释说明 upstream 同时支持基础地址和完整端点 URL。

---

## [v0.3.2] - 2026-04-14

### Bug Fixes

- **修复 upstream 尾部斜线导致双斜线 404**：配置 `https://host/` 时，`parseUpstreamURL` 未将归一化后的地址写回 `cfg.Upstream`，拼接请求路径后产生 `//` 双斜线，上游返回 404。现已在 `default` 分支补充 `c.Upstream = raw`。

### Features

- **自动识别 `/v1/` 后缀**：upstream 填写 `https://host/v1/` 时，自动剥离末尾 `/v1`，避免拼出 `/v1/v1/messages` 或 `/v1/v1/chat/completions`。
- **Info 日志新增 `upstream_url` 字段**：每条请求完成日志现在打印实际转发的完整 URL，方便排查上游地址配置问题。

---

## [v0.3.1] - 2026-04-14

### Features

- **支持完整端点 URL 作为 upstream 地址**：`upstream` 配置项现在同时接受基础地址和完整端点 URL，trae-proxy 通过路径后缀自动识别，无需额外配置字段。
  - 填写基础地址时行为与旧版完全一致（向下兼容，存量配置文件无需修改）
  - 填写完整端点 URL 时，请求直接发往该 URL，不再拼接 `/v1/messages` 或 `/v1/chat/completions`
  - 适用于所有非标准路径的上游服务，例如百度千帆 Coding Plan：
    - OpenAI 协议：`https://qianfan.baidubce.com/v2/coding/chat/completions`（路径无 `/v1` 前缀）
    - Anthropic 协议：`https://qianfan.baidubce.com/anthropic/coding/v1/messages`
- 初始化向导同步更新，提示文字新增完整端点 URL 示例

---

## [v0.3.0] - 2026-04-14

### Features

- **交互式初始化向导**：`sudo trae-proxy init` 现在提供逐步交互式引导，支持配置上游地址、协议类型、模型名映射，自动生成 CA 证书并安装系统信任链。

### Documentation

- 新增 [快速开始指南](docs/quick-start.md)：面向 macOS/Linux 新用户的喂饭级别教程，含截图逐步说明。
- 完善 README：添加详细的配置说明、Trae 接入步骤，与快速开始指南互相链接。
- 同步默认模型列表与 `config.example.toml`。

---

## [v0.2.1] - 2026-04-13

### Features

- 同步 `config.example.toml` 默认模型配置。

---

## [v0.2.0] - 2026-04-11

### Features

- 支持 OpenAI Chat Completions 协议上游。
- 单二进制跨平台发布（macOS arm64/amd64、Linux amd64、Windows amd64）。
- 支持 `start`、`stop`、`restart`、`status`、`update`、`uninstall` 完整命令集。
- 守护进程（`-d`）后台运行，PID 文件管理。
- 自签 CA 生成与系统信任安装（macOS Keychain / Linux ca-certificates）。
- `/etc/hosts` DNS 劫持，拦截 `openrouter.ai` 请求。
- 三层模型名映射回退机制。
- 分级日志（error/warn/info/debug/trace），支持 `--log-body` 打印请求体。
- `update` 命令从 GitHub Releases 自更新，SHA256 校验原子替换。

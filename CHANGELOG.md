# Changelog

All notable changes to this project will be documented in this file.

## [v0.3.1-beta.1] - 2026-04-14

### Features

- **百度千帆 Coding Plan 兼容**：`upstream` 配置项现在同时支持填写基础地址或完整端点 URL。
  - OpenAI 协议示例：`https://qianfan.baidubce.com/v2/coding` 或 `https://qianfan.baidubce.com/v2/coding/chat/completions`
  - Anthropic 协议示例：`https://qianfan.baidubce.com/anthropic/coding` 或 `https://qianfan.baidubce.com/anthropic/coding/v1/messages`
  - 通过路径后缀自动识别，无需额外配置字段，存量配置文件无需修改。
- 初始化向导更新：提示文字新增千帆双形式示例，`writeWizardConfig` 注释同步更新。

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

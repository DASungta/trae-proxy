# Changelog

All notable changes to this project will be documented in this file.

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

# 百度千帆 Coding Plan 端点兼容设计

**日期**：2026-04-14  
**状态**：已批准  
**分支**：feat/qianfan-coding-endpoint

---

## 背景

百度千帆 Coding Plan 使用非标准 URL 结构，与常规 MaaS 服务不同：

| 协议 | Base URL | 完整端点 URL |
|------|----------|--------------|
| OpenAI 兼容 | `https://qianfan.baidubce.com/v2/coding` | `https://qianfan.baidubce.com/v2/coding/chat/completions` |
| Anthropic 兼容 | `https://qianfan.baidubce.com/anthropic/coding` | `https://qianfan.baidubce.com/anthropic/coding/v1/messages` |

**问题根源**：现有代码在 `handler.go` 和 `forward.go` 中用 `cfg.Upstream + "/v1/chat/completions"` 拼接 URL。对于千帆 OpenAI 协议，拼接结果为 `/v2/coding/v1/chat/completions`，与实际路径 `/v2/coding/chat/completions` 不符。

---

## 目标

- 支持用户在 `upstream` 配置项中填写**基地址**或**完整端点 URL**，两种方式均可正常工作
- 判断方式：按路径后缀自动识别，无需新增配置字段
- 向导展示两种填写方式的示例

---

## 设计

### Section 1：Config 层

**`Config` 结构体**新增两个私有字段（不序列化到 TOML）：

```go
upstreamOpenAIURL    string  // 用户指定的完整 OpenAI 端点 URL，"" 表示用 base 拼接
upstreamAnthropicURL string  // 用户指定的完整 Anthropic 端点 URL，"" 表示用 base 拼接
```

**`Load()` 新增解析逻辑**：在所有字段处理完毕后，对 `cfg.Upstream` 做一次后缀检测：

| 检测到后缀 | 处理方式 |
|------------|----------|
| `/chat/completions` | 存入 `upstreamOpenAIURL`；`Upstream` 剥离该后缀存 base |
| `/v1/chat/completions` | 同上 |
| `/v1/messages` | 存入 `upstreamAnthropicURL`；`Upstream` 剥离该后缀存 base |
| 无以上后缀 | `Upstream` 直接存 base，两个私有字段保持为 `""` |

`Upstream` 始终存归一化的 Base URL，确保日志输出有意义。

**新增方法**：

```go
// ResolveUpstreamURL 返回拼接后的完整请求 URL。
// apiPath 为内部 API 路径，如 "/v1/messages" 或 "/v1/chat/completions"。
// 若用户已指定对应协议的完整 URL，直接返回该 URL，忽略 apiPath。
func (c *Config) ResolveUpstreamURL(apiPath string) string
```

实现逻辑：
- `apiPath` 包含 `/messages` → 优先返回 `upstreamAnthropicURL`，否则返回 `c.Upstream + apiPath`
- `apiPath` 包含 `/chat/completions` → 优先返回 `upstreamOpenAIURL`，否则返回 `c.Upstream + apiPath`
- 其他路径 → 返回 `c.Upstream + apiPath`

---

### Section 2：Handler 层

**`handler.go`（Anthropic 转换路径）**

```go
// 旧
url := cfg.Upstream + "/v1/messages"
// 新
url := cfg.ResolveUpstreamURL("/v1/messages")
```

**`forward.go`（OpenAI 直转发路径）**

```go
// 旧
url := cfg.Upstream + upstreamPath
// 新
if strings.HasSuffix(upstreamPath, "/chat/completions") {
    url = cfg.ResolveUpstreamURL(upstreamPath)
} else {
    url = cfg.Upstream + upstreamPath
}
```

`server.go` 路由层无需变更。

---

### Section 3：Wizard & Validation 层

**`validateUpstreamURL` 变更**：

移除对 `/v1/messages`、`/v1/chat/completions`、`/chat/completions` 后缀的报错限制，这些后缀现在是合法输入，直接返回（保留用户原始输入）。

**`promptUpstream` 提示文字更新**：

- 说明行改为：`请输入上游 API 地址（基础地址或完整端点 URL 均可）`
- 新增千帆示例：

```
百度千帆(OpenAI):     https://qianfan.baidubce.com/v2/coding
                        或 https://qianfan.baidubce.com/v2/coding/chat/completions
百度千帆(Anthropic):  https://qianfan.baidubce.com/anthropic/coding
                        或 https://qianfan.baidubce.com/anthropic/coding/v1/messages
```

**`writeWizardConfig` 注释**：移除"路径不要包含 /v1/messages"的限制说明，替换为说明两种填写方式均可。

---

## 受影响文件

| 文件 | 变更类型 |
|------|----------|
| `internal/config/config.go` | 新增私有字段、解析逻辑、`ResolveUpstreamURL` 方法 |
| `internal/config/config_test.go` | 新增 `ResolveUpstreamURL` 测试用例 |
| `internal/proxy/handler.go` | 替换 URL 拼接（1 处） |
| `internal/proxy/forward.go` | 替换 URL 拼接（1 处，含路径分支） |
| `cmd/trae-proxy/wizard.go` | 更新 `validateUpstreamURL`、`promptUpstream`、`writeWizardConfig` |
| `cmd/trae-proxy/wizard_test.go` | 更新/新增完整 URL 测试用例 |

---

## 兼容性

- 现有配置文件无需修改：未填写完整 URL 的 `upstream` 字段行为完全不变
- `/v1/models` 端点已标记废弃，不在本次修改范围内

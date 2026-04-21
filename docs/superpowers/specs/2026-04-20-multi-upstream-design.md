# Multi-Upstream / Multi-Protocol / Multi-Model 路由设计

**日期**：2026-04-20  
**版本**：v0.5.0（计划）  
**状态**：已审批，待实现

---

## 背景与目标

现有 trae-proxy 只支持单一上游（`upstream` + `upstream_protocol`），所有请求无差别转发到同一目标。

新版本目标：
- 支持在 config 中定义**多个命名上游**，每个上游有独立的 URL 和协议
- 在 `[models]` 中，**按模型粒度**将请求路由到不同上游
- 对现有用户保持**零感知升级**（`update` 命令自动迁移 config）

---

## 约束

- API Key 不由 proxy 管理，由用户在 Trae IDE 中自行填写
- 不支持 `init` 向导配置多上游（提示用户手动编辑）
- 迁移失败不阻断 `update` 流程

---

## Config 数据结构

### 新增类型

```go
// Upstream 定义单个上游服务
type Upstream struct {
    URL      string `toml:"url"`
    Protocol string `toml:"protocol"` // "anthropic" | "openai"
    Default  bool   `toml:"default"`  // 有且仅有一个为 true
}

// ModelRoute 是模型映射的新格式（指向特定上游）
type ModelRoute struct {
    Upstream string `toml:"upstream"` // 引用 Upstreams map 的 key
    Model    string `toml:"model"`    // 目标模型名；空字符串 = 直通请求中的模型名
}

// ResolvedRoute 是路由解析结果
type ResolvedRoute struct {
    UpstreamModel string
    Upstream      *Upstream
}
```

### Config 结构体变化

```go
type Config struct {
    // 向后兼容字段（旧 config 读取后迁移，新 config 不写入）
    Upstream         string `toml:"upstream"`
    UpstreamProtocol string `toml:"upstream_protocol"`

    // 新字段
    Upstreams map[string]*Upstream `toml:"upstreams"`

    // Models 值类型为 string（旧格式）或内联 ModelRoute（新格式）
    // TOML 解码目标为 map[string]any，Load() 后规范化为内部结构
    RawModels map[string]any `toml:"models"`

    Listen     string `toml:"listen"`
    Hijack     string `toml:"hijack"`
    RealModels bool   `toml:"real_models"`
    LogLevel   string `toml:"log_level"`
    LogBody    bool   `toml:"log_body"`

    // 内部缓存，Load() 后填充
    defaultUpstream *Upstream
    resolvedModels  map[string]modelEntry // string-keyed, either plain rename or full route
}
```

---

## TOML 格式

### 新格式（v0.5.0+）

```toml
[upstreams.my_relay]
url      = "http://192.168.48.12:8080"
protocol = "anthropic"
default  = true

[upstreams.openai_official]
url      = "https://api.openai.com"
protocol = "openai"

# 多上游示例（手动添加）：
# [upstreams.another_relay]
# url      = "https://another.relay.example.com"
# protocol = "openai"

[models]
# 旧格式：字符串值，走 default upstream
"anthropic/claude-sonnet-4.6" = "claude-sonnet-4.6"
"anthropic/claude-opus-4.6"   = "claude-opus-4.6"

# 新格式：表格值，指定上游
"openai/gpt-4o" = { upstream = "openai_official", model = "gpt-4o" }
```

### 旧格式（v0.4.x，`update` 迁移前）

```toml
upstream          = "http://192.168.48.12:8080"
upstream_protocol = "anthropic"

[models]
"anthropic/claude-sonnet-4.6" = "claude-sonnet-4.6"
```

---

## 路由逻辑 `RouteModel()`

```
RouteModel(requestModelName) → ResolvedRoute
  │
  ├─ RawModels[name] 存在且为 ModelRoute
  │    → UpstreamModel = route.Model（空则用 requestModelName）
  │    → Upstream = Upstreams[route.Upstream]
  │
  ├─ RawModels[name] 存在且为 string
  │    → UpstreamModel = mapped string（空则用 requestModelName）
  │    → Upstream = defaultUpstream
  │
  └─ 无匹配
       → 三级 fallback：精确 → 去 anthropic/ 前缀 → 直通
       → Upstream = defaultUpstream
```

`Load()` 时验证：`Upstreams` 中有且仅有一个 `Default == true`，否则报错。

---

## Proxy 层变化

### `Server` 结构体

```go
type Server struct {
    Config       *config.Config
    Logger       *logging.Logger
    TLSConfig    *tls.Config
    BypassClient *http.Client

    // 按 upstream host 缓存 HTTP client，避免频繁建连
    clientCache map[string]*http.Client // key = upstream URL host
    clientMu    sync.RWMutex
}

func (s *Server) clientFor(u *config.Upstream) *http.Client
```

`NewServer` 时为所有已知 upstream 预建 client；`clientFor` 为 map lookup，按需懒建。

### `Handler()` 路由简化

```go
case r.Method == "POST" && norm == "v1/chat/completions":
    HandleChatCompletions(s)(w, r)
    // 统一入口，内部按 route.Upstream.Protocol 分支
```

### `HandleChatCompletions` 内部流程

```
读 body
→ 解析 OpenAI request，取 model 字段
→ cfg.RouteModel(model) → route
→ route.Upstream.Protocol == "anthropic"
    → 现有 ChatToAnthropic 转换路径
       → POST route.Upstream.URL/v1/messages
       → AnthropicToChat / StreamConverter
→ route.Upstream.Protocol == "openai"
    → forward 路径（原 HandleForward 逻辑）
       → POST route.Upstream.URL/v1/chat/completions
```

---

## `update` 自动迁移

迁移在替换二进制**之前**执行（读旧 config、写新 config）：

```
检测旧格式条件：cfg.Upstream != "" && len(cfg.Upstreams) == 0
  ↓ 是
构造 Upstreams["default"] = {URL: cfg.Upstream, Protocol: cfg.UpstreamProtocol, Default: true}
清空 cfg.Upstream / cfg.UpstreamProtocol
写回 config 文件
打印："✓ 配置已自动迁移至多上游格式（[upstreams.default]）"
  ↓ 否
跳过，无操作
```

迁移中任何错误：打印警告，原文件保持不变，继续执行 update 其余步骤。

---

## `init` 向导变更

向导流程不变，完成写入 config 后额外打印：

```
提示：如需添加多个上游，可手动编辑配置文件：
  在 [upstreams] 中添加新上游，在 [models] 中使用
  { upstream = "名称", model = "模型名" } 格式路由到指定上游。
```

生成的 config 文件头部包含注释掉的多上游示例块，便于用户参考。

---

## 向后兼容矩阵

| 场景 | 行为 |
|------|------|
| 旧 config + 旧二进制 | 完全不变 |
| 旧 config + 新二进制（未执行 update） | Load() 检测到旧格式，自动在内存中构造 defaultUpstream，正常运行 |
| 旧 config + 执行 update | 自动迁移写入新格式，后续以新格式运行 |
| 新 config + 新二进制 | 正常多上游路由 |

---

## 不在本版本范围内

- 上游健康检查 / 故障转移
- 按请求头或用户身份路由
- 上游权重 / 负载均衡
- `init` 向导多上游交互配置

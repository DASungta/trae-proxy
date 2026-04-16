# trae-proxy UX 改进设计文档

**日期**：2026-04-16  
**状态**：已批准，待实现  
**作者**：zhangyc

---

## 背景与动机

用户反馈集中在三个方向：

1. **操作繁琐**：向导 Step 3 只能从预设列表选模型，不能自由输入；换模型需手动改 config.toml
2. **sudo 权限污染**：`sudo trae-proxy init` 导致 config.toml 归 root 所有，用户无法直接编辑
3. **向导方向键乱码**：`bufio.Scanner` 不处理终端 escape sequence，左右/上下键输出乱码

此外，抓包发现 Trae 通过 `GET /api/v1/models` 校验模型合法性，当前返回的 OpenAI 格式无法通过校验，导致用户无法在 Trae UI 中自由添加、编辑模型。

---

## 特性清单

| # | 特性 | 模块 |
|---|---|---|
| F1 | macOS 原生权限弹窗，移除 sudo 前缀 | `internal/privilege`（新建）、`cmd/trae-proxy` |
| F2 | 向导重构：方向键支持 + 可搜索列表 + 自定义模型输入 | `cmd/trae-proxy/wizard.go` |
| F3 | `/api/v1/models` 返回 OpenRouter 兼容格式 | `internal/proxy/models.go` |
| F4 | `uninstall` 增加 443 端口进程检测与 kill | `cmd/trae-proxy/main.go` |
| F5a | 升级后 config 自动迁移 | `internal/config/migrate.go`（新建） |
| F5b | 升级后操作引导打印 | `internal/updater/updater.go`、`cmd/trae-proxy/migrations.go`（新建） |

Windows / Linux 暂不纳入本次范围，相关命令保留 sudo 前缀提示并标注"后续支持"。

---

## F1：权限管理重构（macOS）

### 设计原则

CLI 主进程始终以当前用户身份运行。需要系统权限的操作通过 `osascript` 弹出 macOS 原生密码对话框，以最小化权限范围。

### 权限边界

| 命令 | 操作 | 执行身份 |
|---|---|---|
| `init` | 生成 CA 证书文件 | 当前用户 |
| `init` | 安装 CA 到系统信任库 | osascript（root） |
| `init` | 写 config.toml | 当前用户 |
| `init` | 添加 /etc/hosts 条目 | osascript（root） |
| `start` | 添加 /etc/hosts 条目 | osascript（root） |
| `start` | 启动守护进程（绑定 :443） | osascript → 进程以 root 运行 |
| `stop` | 删除 /etc/hosts 条目 | osascript（root） |
| `stop` | SIGTERM 到守护进程 | 当前用户（读 PID 文件） |
| `uninstall` | 移除 CA、清理 hosts | osascript（root） |

### 新增模块：`internal/privilege`

```go
// RunPrivileged 通过 osascript 在 macOS 上以管理员身份执行 shell 命令。
// 弹出系统原生密码对话框，用户取消则返回 error。
func RunPrivileged(shellCmd string) error

// MustRunPrivileged 同上，失败时 fatal。
func MustRunPrivileged(shellCmd string) error

// IsPrivileged 返回当前进程是否已是 root。
func IsPrivileged() bool
```

`osascript` 调用模式：
```go
script := fmt.Sprintf(`do shell script %q with administrator privileges`, shellCmd)
exec.Command("osascript", "-e", script).Run()
```

### 平台适配

通过 build tags 隔离：

- `privilege_darwin.go`：osascript 实现
- `privilege_linux.go`：返回 `ErrNotSupported`，调用方打印"请使用 sudo 前缀"提示
- `privilege_windows.go`：同上

### 注意事项

- `osascript` 弹窗在 macOS 14+ 有沙箱限制，需确保二进制不在受限目录下运行
- 多次弹窗：单次 `init` 可能弹出 2 次（CA 安装 + hosts 修改），每次弹窗前打印说明文字告知用户原因
- 取消处理：用户点"取消"返回非零退出码，捕获后打印友好提示并中止流程

---

## F2：向导重构

### 依赖

引入 `github.com/AlecAivazis/survey/v2`（MIT 协议，无间接系统依赖）。

### 交互流程

**Step 1 — 上游地址**：`survey.Input` + Validate 函数内联校验，天然支持方向键、Home/End、退格。

**Step 2 — 协议选择**：`survey.Select`，上下键导航：
```
? 上游协议:
  ▸ anthropic — 自动转换 OpenAI→Anthropic 格式
    openai    — 直接转发（中转站、LM Studio、Ollama）
```

**Step 3 — 模型选择**：`survey.Select` 开启 filter，末尾追加 `[自定义输入...]` 选项：
```
? 选择 Trae 中要映射的模型 (输入可过滤):
  ▸ anthropic/claude-sonnet-4.5
    anthropic/claude-opus-4.1
    openai/gpt-4o
    ...
    [自定义输入...]
```
选中"自定义输入"后追加 `survey.Input` 接收任意字符串。

**Step 4 — 上游模型名**：`survey.Input`，保持现有逻辑。

### 向后兼容

`runWizard(in io.Reader, out io.Writer)` 签名不变，非终端环境（如测试、管道输入）降级到原 `bufio.Scanner` 路径，通过 `isTerminal(os.Stdin)` 判断。

---

## F3：`/api/v1/models` OpenRouter 格式兼容

### 问题

Trae 调用 `GET https://openrouter.ai/api/v1/models` 校验模型合法性。当前 `serveFakeModels` 返回 OpenAI 格式，Trae 不认，导致自定义模型无法通过校验。

### 请求特征（抓包）

```
GET /api/v1/models HTTP/1.1
Host: openrouter.ai
Authorization: Bearer 111111111
```

### 响应格式（OpenRouter）

```json
{
  "data": [
    {
      "id": "anthropic/claude-sonnet-4.5",
      "canonical_slug": "anthropic/claude-sonnet-4.5",
      "name": "Anthropic: Claude Sonnet 4.5",
      "created": 1744800000,
      "description": "",
      "context_length": 200000,
      "architecture": {
        "modality": "text+image->text",
        "input_modalities": ["text", "image"],
        "output_modalities": ["text"],
        "tokenizer": "Claude",
        "instruct_type": null
      },
      "pricing": { "prompt": "0", "completion": "0" },
      "top_provider": {
        "context_length": 200000,
        "max_completion_tokens": 128000,
        "is_moderated": false
      },
      "per_request_limits": null,
      "supported_parameters": [
        "max_tokens", "temperature", "tools", "tool_choice", "top_p",
        "response_format", "stop", "stream"
      ],
      "default_parameters": {
        "temperature": null, "top_p": null, "top_k": null,
        "frequency_penalty": null, "presence_penalty": null
      },
      "knowledge_cutoff": null,
      "expiration_date": null
    }
  ]
}
```

### 实现

`serveFakeModels` 修改为生成 OpenRouter 格式：

- 数据来源：`config.ModelIDs()`（即 `[models]` 表的所有 key）
- 每个 model 条目填充完整 OpenRouter 字段，`pricing` 统一填 `"0"`（对 Trae 校验无影响）
- `architecture` 字段根据模型 ID 前缀推断（`anthropic/` → Claude tokenizer，`openai/` → GPT，其他 → Other）
- 响应 Content-Type: `application/json`，不含 `object: "list"` 字段（OpenRouter 格式没有此字段）

### 路由

当前 `stripAPIPrefix` 已将 `/api/v1/models` 剥除为 `/v1/models` 后路由到 `HandleModels`，路由层无需修改。

---

## F4：`uninstall` 进程检测

在 `uninstall` 命令开头插入检测逻辑：

```
1. 运行 lsof -nP -iTCP:443 -sTCP:LISTEN（macOS）
2. 过滤输出，查找进程名含 "trae-proxy" 的行
3. 找到 → 打印提示 → 确认后 kill -9 <PID>
4. kill 失败（权限不足）→ osascript 弹窗执行 kill
5. 未找到 → 继续正常 uninstall 流程
```

与现有 `daemonStop` 逻辑的关系：`daemonStop` 依赖 PID 文件，此处是兜底检测（PID 文件丢失时也能清理）。两者顺序：先调 `daemonStop`（软停止），若端口仍被占用则走强制 kill。

---

## F5：升级引导

### F5a：config 自动迁移

新建 `internal/config/migrate.go`：

```go
// currentSchemaVersion 是当前二进制支持的 config schema 版本号。
// 每次新增/删除/重命名 config 字段时递增。
const currentSchemaVersion = 2

// Migrate 检测 cfg 的 schema 版本，逐步应用迁移函数，返回是否有变更。
func Migrate(cfg *Config) (changed bool, report []string)
```

迁移函数示例：
```go
// v1 → v2: 新增 real_models 字段，默认 false
func migrateV1toV2(cfg *Config) string {
    // real_models 字段在 v1 不存在，TOML 加载后为零值（false），无需写入
    // 但 config_version 需要更新
    return "新增字段 real_models（默认 false）"
}
```

`config_version` 存储在独立文件 `~/.config/trae-proxy/.schema_version`（纯整数内容），与 config.toml 完全分离，不污染用户配置。文件不存在时默认版本为 1。

**触发时机**：`config.Load()` 调用后、`proxy.NewServer()` 调用前，在 `cmd/trae-proxy/main.go` 的 `runProxy()` 中检测并应用。迁移成功后打印 report，config 自动写回原路径。

### F5b：升级后操作引导

新建 `cmd/trae-proxy/migrations.go`，内嵌版本迁移说明：

```go
// versionMigrations maps "fromVersion" → migration note shown after update.
var versionMigrations = map[string]migrationNote{
    "v0.3.x": {
        RequiresReinit: true,
        Steps: []string{
            "trae-proxy stop",
            "trae-proxy init   # 重新申请权限（权限模型已变更，不再需要 sudo）",
            "trae-proxy start -d",
        },
        Reason: "v0.4.0 移除了 sudo 依赖，需要重新初始化权限配置。",
    },
}
```

`update` 命令在二进制替换成功后：
1. 记录旧版本号（替换前读 `trae-proxy version`）
2. 替换完成后，调 `updater.PrintMigrationGuide(oldVersion, newVersion)`
3. 打印匹配的 migration note，包含 `Steps` 和 `Reason`

---

## 测试策略

| 模块 | 测试点 |
|---|---|
| `internal/privilege` | `IsPrivileged()` 在非 root 下返回 false；`RunPrivileged` 在测试中可 mock `osascriptRunner` 变量 |
| `wizard.go` | 非终端路径（`isTerminal=false`）降级到 `bufio.Scanner`，现有测试继续通过；新增 survey 路径的 mock 测试 |
| `models.go` | `serveFakeModels` 返回 `data` 数组，无 `object: "list"` 字段；字段完整性断言 |
| `config/migrate.go` | 各版本迁移函数的 golden file 对比；`Migrate` 幂等性（重复调用不重复修改） |
| `uninstall` | mock `lsof` 输出，验证进程名过滤逻辑 |

---

## 开发顺序建议

1. F3（models 格式）— 改动最小，独立，可立刻验证
2. F2（向导重构）— 核心 UX，引入 survey/v2
3. F1（权限重构）— 影响面最大，需要逐命令验证
4. F4（uninstall 检测）— 独立，短平快
5. F5（升级引导）— 依赖 F1 完成后确认版本号边界

---

## 变更记录

| 日期 | 事件 |
|---|---|
| 2026-04-16 | 初稿，经用户确认方向后定稿 |

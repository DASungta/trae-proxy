# Windows 证书安装校验与 TLS 握手日志 — 设计文档

**日期**: 2026-04-17
**状态**: 已批准，待实现
**作者**: Codex

---

## 背景与目标

Issue #9 暴露了两个直接问题：

1. Windows 用户执行 `trae-proxy init` 后看到 `CA installed successfully`，但 `curl https://openrouter.ai` 仍报 `SEC_E_UNTRUSTED_ROOT`
2. TLS 握手失败发生时，用户把日志级别改到 `debug` 也看不到有效信息

从现象判断，当前问题不在于 `hosts` 劫持或 HTTPS 服务未启动，而在于：

- Windows 上 CA 安装成功的判定过于宽松，只看 `certutil` 退出码
- TLS 握手错误没有接入项目现有 logger

本方案的目标是：

- 消除 Windows 下 “提示安装成功但系统未信任” 的误报
- 让 TLS 握手失败进入项目日志，便于用户自助排查

---

## 范围

### 在范围内

- Windows 平台 CA 安装后的真实校验
- Windows 平台 CA 安装失败时的可操作错误提示
- TLS 握手错误接入项目日志
- 对应单元测试

### 不在范围内

- `Missing Authentication header (401)` 的转发链路排查
- Windows 自动提权或 UAC 流程重构
- 证书安装方式切换到原生 Windows API
- 非 Windows 平台的证书安装逻辑调整

---

## 问题拆解

### P1. Windows CA 安装误报成功

当前 [`internal/tls/ca.go`](../../../internal/tls/ca.go) 的 Windows 分支仅执行：

```go
certutil -addstore -f ROOT <root-ca.pem>
```

只要命令返回 `nil`，上层 [`cmd/trae-proxy/main.go`](../../../cmd/trae-proxy/main.go) 就会打印 `CA installed successfully`。

这个判定存在两个缺陷：

- 没有回显 `certutil` 的标准输出/错误输出，诊断信息丢失
- 没有校验证书是否真的出现在 `ROOT` 证书存储中

### P2. TLS 握手失败不可观测

当前 [`internal/proxy/server.go`](../../../internal/proxy/server.go) 创建 `http.Server` 时没有配置 `ErrorLog`。Go TLS 握手错误会走服务端错误日志，而不是现有 request logger，因此用户只能看到客户端侧的 `SEC_E_UNTRUSTED_ROOT`，看不到服务端视角的失败原因。

---

## 设计决策

### D1. 保留 `certutil`，但增加安装后验证

不引入 Windows 原生证书 API，继续使用 `certutil` 完成安装，原因是：

- 当前工程已依赖命令行工具路径，改动最小
- 用户文档和手动排查命令已经基于 `certutil`
- 这轮目标是修复误报和增强可观测性，不做平台能力重构

安装动作之后追加一次验证：

```powershell
certutil -store ROOT "trae-proxy Root CA"
```

若验证结果中找不到目标证书，则视为安装失败。

### D2. 错误信息必须带上原始命令输出

Windows 安装失败时，返回值不能只保留 `exit status`。需要把 `certutil` 的合并输出一起返回，便于定位是：

- 未在管理员 PowerShell 中执行
- 导入被系统拒绝
- 存储查询失败
- 证书名称不匹配

### D3. `init` 层输出平台化指引

`init` 捕获 Windows 安装失败时，打印明确的后续操作，而不是泛化成 `you may need to manually trust the CA`。输出内容包括：

- 需要使用管理员 PowerShell
- 手动补装命令
- 手动验证命令

### D4. TLS 握手错误桥接到现有 logger

给 `http.Server` 配置一个桥接到 `logging.Logger` 的 `log.Logger`，把 Go runtime/server 层的错误输出纳入现有日志文件。这样握手失败即使没有进入 HTTP handler，也能被记录。

日志级别统一记为 `WARN`，避免把常见握手失败记成 `ERROR` 并污染正常运行日志。

---

## 详细设计

### 1. `internal/tls/ca.go`

新增内部辅助函数：

```go
func runCommandCombined(name string, args ...string) ([]byte, error)
func windowsRootCAInstalled(commonName string) (bool, string, error)
```

职责：

- `runCommandCombined` 统一执行外部命令并返回合并输出
- `windowsRootCAInstalled` 通过 `certutil -store ROOT "trae-proxy Root CA"` 判断证书是否存在，并返回原始输出用于错误信息

`InstallCA` 的 Windows 流程改为：

1. 执行 `certutil -addstore -f ROOT <caCertPath>`
2. 若命令失败，返回带输出的错误
3. 执行存储校验
4. 若证书不存在，返回 “install command succeeded but CA not found in ROOT store” 类错误，并附带校验输出
5. 仅当安装和校验都成功时返回 `nil`

`UninstallCA` 本轮不改逻辑，只保留现状，避免扩大范围。

### 2. `cmd/trae-proxy/main.go`

保持现有 `init` 控制流不变，只增强 Windows 失败提示。

Windows 分支失败时补充两条命令：

```powershell
certutil -addstore -f ROOT "%USERPROFILE%\.config\trae-proxy\ca\root-ca.pem"
certutil -store ROOT "trae-proxy Root CA"
```

原则：

- 不改变 `init` 成功/失败分支结构
- 不在这里重复实现安装逻辑
- 只负责把 `tlsutil.InstallCA` 的失败结果翻译成用户可执行的下一步

### 3. `internal/proxy/server.go`

新增一个适配器，把标准库 logger 输出桥接到 `logging.Logger`：

```go
type serverErrorLogWriter struct {
	logger *logging.Logger
}
```

行为：

- 实现 `Write(p []byte) (int, error)`
- 去掉结尾换行
- 空字符串直接忽略
- 统一调用 `logger.Warn("server error", "msg", trimmed)`

在 `ListenAndServe` 中创建 `http.Server` 时挂载：

```go
ErrorLog: log.New(&serverErrorLogWriter{logger: s.Logger}, "", 0)
```

这样 TLS 握手失败、bad request、server-level network error 都会进入项目日志。

### 4. 测试

#### `internal/tls/ca_test.go`

为外部命令执行引入可替换变量，便于测试 Windows 分支而不依赖真实系统命令。测试覆盖：

- `certutil -addstore` 失败时，错误中包含命令输出
- 安装命令成功但查询不到 `trae-proxy Root CA` 时，返回验证失败
- 安装和验证都成功时返回 `nil`

#### `internal/proxy/server_test.go`

新增针对 `serverErrorLogWriter` 的测试，覆盖：

- 普通 TLS 错误日志被写入
- 末尾换行被正确裁剪
- 空输入被忽略

---

## 兼容性与风险

### 兼容性

- macOS / Linux 路径不变
- Windows 用户的成功路径不变，只是成功条件更严格
- 日志格式仍沿用现有 `logging.Logger` 的文本格式

### 风险

- `certutil -store` 输出格式可能存在本地化差异，因此验证逻辑不能依赖固定英文句子，应优先检查是否包含 `trae-proxy Root CA`
- server 级错误日志会捕获更多噪声，需要使用 `WARN` 而不是 `ERROR`

---

## 验收标准

满足以下条件即可视为本次设计达成：

1. Windows 上 `certutil -addstore` 成功但证书未实际进入 `ROOT` 存储时，`trae-proxy init` 不再打印误导性的成功提示
2. Windows 安装失败信息中包含原始命令输出和明确的手动补装指引
3. TLS 握手失败会写入 `trae-proxy` 日志文件
4. 单元测试覆盖安装失败、验证失败、日志桥接三个关键分支

---

## 实施顺序

1. 在 `internal/tls/ca.go` 中抽象命令执行与 Windows 安装后校验
2. 在 `cmd/trae-proxy/main.go` 中补充 Windows 失败提示
3. 在 `internal/proxy/server.go` 中接入 `ErrorLog`
4. 增加对应单测并执行验证

# v0.4.0 升级指南

> 适用范围：从 **v0.3.x** 升级到 **v0.4.0**

---

## 主要变化

### 1. macOS 不再需要 `sudo`

v0.4.0 起，macOS 上所有需要系统权限的操作（安装 CA 证书、修改 `/etc/hosts`）均通过 **原生系统授权对话框** 完成，不再需要 `sudo`。

- `trae-proxy init` — 弹出对话框请求安装 CA 证书的权限
- `trae-proxy start` — 弹出对话框请求修改 `/etc/hosts` 的权限
- `trae-proxy stop` / `uninstall` — 弹出对话框请求移除相关条目的权限

> Linux 和 Windows 暂无变化，仍需管理员权限（`sudo` / 管理员 PowerShell）。

### 2. 交互式向导支持方向键和搜索过滤

`trae-proxy init` 的交互向导从基于文本输入的 `bufio.Scanner` 升级为 `survey/v2`：

- **方向键导航**：上下键在列表中移动，无需手动输入编号
- **搜索过滤**：输入关键词实时过滤模型列表
- **自定义模型输入**：在模型列表末尾选择「自定义输入」可填入任意模型名

### 3. `/v1/models` 改为 OpenRouter 兼容格式

代理返回的模型列表格式从 OpenAI 格式改为 **OpenRouter 格式**（`data` 数组，含 `canonical_slug`、`architecture`、`pricing` 等字段），与 Trae 期望格式一致，解决了部分用户遇到的模型无法识别问题。

### 4. 配置 schema 自动迁移

首次运行 v0.4.0 时，代理会自动检测配置 schema 版本并完成迁移（如有需要），无需手动修改配置文件。

### 5. `uninstall` 兜底进程清理

`trae-proxy uninstall` 现在会检测 443 端口是否仍被占用（PID 文件丢失场景），若有残留进程会自动终止，避免卸载不彻底。

---

## 升级步骤

### macOS 用户

```bash
# 1. 停止当前运行的代理（v0.3.x 需要 sudo）
sudo trae-proxy stop

# 2. 更新到最新版本
trae-proxy update
# 或手动下载替换二进制

# 3. 重新初始化（移除旧 CA，重新安装）
#    v0.4.0 不再需要 sudo，会弹出系统授权对话框
trae-proxy init

# 4. 启动（不再需要 sudo）
trae-proxy start -d
```

> **为什么需要重新 init？**
> v0.3.x 的 CA 证书可能由 `root` 用户安装。v0.4.0 使用 macOS 原生对话框，建议重新执行 `init` 确保 CA 以正确权限安装，并生成新的服务器证书。
>
> 如果跳过重新 init，代理仍然可以运行，但 `start` 时修改 hosts 仍会使用 v0.3.x 的旧逻辑（sudo），直到下次 init 完成切换。

### Linux 用户

Linux 用户权限机制无变化，常规升级即可：

```bash
# 停止代理
sudo trae-proxy stop

# 更新
trae-proxy update

# 重新启动
sudo trae-proxy start -d
```

### Windows 用户

Windows 暂不支持自动更新，请手动从 [Releases](https://github.com/DASungta/trae-proxy/releases/latest) 页面下载新版 `trae-proxy-windows-amd64.exe` 替换原有文件。

---

## 兼容性说明

| 项目 | 兼容性 |
|---|---|
| 配置文件 `config.toml` | **完全兼容**，无需修改，自动迁移 |
| CA 证书 | **兼容**，但建议重新 init（macOS 权限归属问题） |
| 模型映射 | **完全兼容** |
| CLI 命令 | **完全兼容**，所有命令保持不变 |
| `/v1/models` 响应格式 | 已变更为 OpenRouter 格式（对 Trae IDE 透明） |

---

## 已知问题

- **macOS：`init` 后对话框未弹出** — 检查二进制所在路径是否在受限目录（如 Downloads 文件夹）。建议将 `trae-proxy` 放在 `/usr/local/bin` 或 `~/bin`。
- **v0.3.x config.toml 由 root 拥有** — 若 `~/.config/trae-proxy/config.toml` 属主为 root，运行 `sudo chown $USER ~/.config/trae-proxy/config.toml` 修正后再运行 v0.4.0。

---

## 回滚

如需回滚到 v0.3.x：

```bash
# 停止 v0.4.0
trae-proxy stop

# 从 Releases 下载 v0.3.x 对应版本
trae-proxy update --version v0.3.3

# 重新启动（v0.3.x 需要 sudo）
sudo trae-proxy start -d
```

---

## 更多资源

- [新版 README →](../README.md)
- [旧版 README (v0.3.x) →](README-v0.3.md)
- [CHANGELOG →](../docs/CHANGELOG.md)
- [GitHub Releases →](https://github.com/DASungta/trae-proxy/releases)

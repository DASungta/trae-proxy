# Windows 一键安装脚本 (`install.ps1`) — 设计文档

**日期**: 2026-04-17
**状态**: 待实现

---

## 背景与目标

当前 Windows 安装流程要求用户手动将目录加入系统 PATH 环境变量，对非技术用户门槛过高。本方案提供一个 `install.ps1` 脚本，用户在 PowerShell 中运行一行命令即可完成安装，无需手动配置环境变量。

**目标用户**: 知道如何打开 PowerShell，但不熟悉 Windows 环境变量配置的用户。

---

## 安装命令

```powershell
irm https://raw.githubusercontent.com/DASungta/trae-proxy/main/install.ps1 | iex
```

---

## 设计决策

### 安装目录

选择 `$env:LOCALAPPDATA\trae-proxy\trae-proxy.exe`（即 `C:\Users\<用户名>\AppData\Local\trae-proxy\`）。

**理由**:
- 普通用户权限即可写入，安装脚本本身无需管理员
- 不污染 `C:\Program Files\` 或系统目录
- 与现有 `~/.config/trae-proxy/` 配置目录风格一致

### PATH 写入

写入**用户级注册表** `HKCU:\Environment` 的 `PATH` 键，无需管理员权限，新开 PowerShell 即生效，不影响其他用户。

写入前检查是否已存在，避免重复追加。

### 管理员权限分离

| 步骤 | 是否需要管理员 |
|------|--------------|
| 安装脚本（下载、放置、写 PATH）| 否 |
| `trae-proxy init`（写 hosts、安装 CA 证书）| 是 |

两步分离，UAC 提权仅在用户主动运行 `trae-proxy init` 时触发，时机明确。

---

## 脚本执行流程

```
1. 检测架构（当前仅支持 amd64）
2. 从 GitHub Releases API 获取最新版本号
3. 构造下载 URL：trae-proxy-windows-amd64.exe
4. 下载到临时目录
5. 下载 checksums.txt，SHA256 校验（使用 Get-FileHash）
6. 创建安装目录（若不存在）
7. 移动 .exe 到安装目录（若已有旧版本则覆盖）
8. 检查用户 PATH，若未包含安装目录则追加
9. 通知当前 Shell 刷新 PATH（$env:PATH 更新）
10. 打印后续操作提示
```

---

## 输出示例

```
  trae-proxy 安装脚本 (Windows)
  ==============================

正在获取最新版本...
正在下载 trae-proxy v0.4.0 (windows/amd64)...
正在校验文件完整性... 校验通过 ✓
安装到 C:\Users\Alice\AppData\Local\trae-proxy\trae-proxy.exe
已将安装目录添加到用户 PATH

✅ trae-proxy v0.4.0 安装完成！

下一步（需在管理员 PowerShell 中运行）：
  trae-proxy init       # 生成证书并安装到系统信任库
  trae-proxy start -d   # 启动代理（后台运行）
  trae-proxy status     # 查看运行状态

提示：请关闭当前 PowerShell 窗口，重新打开后 trae-proxy 命令即可使用。
```

---

## 与 `install.sh` 的对齐

| 特性 | `install.sh` | `install.ps1` |
|------|-------------|---------------|
| 版本获取 | GitHub Releases API | GitHub Releases API |
| 下载 | `curl` | `Invoke-WebRequest` |
| SHA256 校验 | `shasum` / `sha256sum` | `Get-FileHash` |
| 安装路径 | `/usr/local/bin/trae-proxy` | `%LOCALAPPDATA%\trae-proxy\trae-proxy.exe` |
| PATH 写入 | 系统级（已在 PATH） | 用户级注册表 `HKCU:\Environment` |
| 覆盖升级 | 支持 | 支持 |
| 需要管理员 | 否（INSTALL_DIR 可覆盖） | 否 |

---

## 文档更新

`docs/win-quick-start.md` 第一步「下载 trae-proxy」部分替换为：

```powershell
# 以普通用户身份在 PowerShell 中运行：
irm https://raw.githubusercontent.com/DASungta/trae-proxy/main/install.ps1 | iex
```

原有「重命名 → 放置 → 配置 PATH」三步合并为一行命令，原步骤作为「手动安装」折叠保留。

---

## 不在范围内

- 卸载脚本（`trae-proxy uninstall` 已处理）
- ARM64 Windows 支持（当前 Release 无 arm64 Windows 产物）
- 图形化安装向导（.msi / NSIS）
- Scoop / winget 包发布

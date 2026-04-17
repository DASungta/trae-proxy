Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$RepoOwner = 'DASungta'
$RepoName = 'trae-proxy'
$AssetName = 'trae-proxy-windows-amd64.exe'
$InstallDir = Join-Path $env:LOCALAPPDATA 'trae-proxy'
$InstallPath = Join-Path $InstallDir 'trae-proxy.exe'
$ApiLatest = "https://api.github.com/repos/$RepoOwner/$RepoName/releases/latest"
$MaxUserEnvVarLength = 32767
$script:PathUpdated = $false

function Fail {
    param([string]$Message)

    Write-Host "Error: $Message" -ForegroundColor Red
    exit 1
}

function Write-Info {
    param([string]$Message)

    Write-Host $Message
}

function Test-SupportedArchitecture {
    if ($env:OS -ne 'Windows_NT') {
        Fail '该安装脚本仅支持 Windows。'
    }

    # PROCESSOR_ARCHITEW6432 is set to the real OS arch when a 32-bit process runs
    # on a 64-bit OS (WoW64); fall back to PROCESSOR_ARCHITECTURE otherwise.
    # Both env vars have been available since Windows NT — no .NET version dependency.
    $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    if (-not $arch) {
        Fail '无法识别 CPU 架构（PROCESSOR_ARCHITECTURE 为空）。'
    }

    switch ($arch.ToUpperInvariant()) {
        'AMD64' { return }
        'ARM64' { Fail '暂不支持 Windows Arm64，请使用 x64 / amd64 机器。' }
        'X86'   { Fail '暂不支持 32 位 Windows，请使用 x64 / amd64 机器。' }
        default { Fail "不支持的 CPU 架构: $arch（仅支持 Windows x64 / amd64）" }
    }
}

function Get-TargetVersion {
    if ($env:VERSION) {
        Write-Info "使用指定版本: $($env:VERSION)"
        return $env:VERSION
    }

    Write-Info '正在获取最新版本...'
    try {
        $release = Invoke-RestMethod -Uri $ApiLatest
    } catch {
        Fail "无法获取最新版本。请检查 https://github.com/$RepoOwner/$RepoName/releases"
    }

    if (-not $release.tag_name) {
        Fail "无法获取最新版本。请检查 https://github.com/$RepoOwner/$RepoName/releases"
    }

    return [string]$release.tag_name
}

function New-TemporaryDirectory {
    $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
    New-Item -Path $tempDir -ItemType Directory -Force | Out-Null
    return $tempDir
}

function Download-ReleaseFiles {
    param(
        [string]$Version,
        [string]$TempDir
    )

    $baseUrl = "https://github.com/$RepoOwner/$RepoName/releases/download/$Version"
    $exePath = Join-Path $TempDir $AssetName
    $checksumsPath = Join-Path $TempDir 'checksums.txt'
    $requestArgs = @{}

    if ($PSVersionTable.PSVersion.Major -lt 6) {
        $requestArgs.UseBasicParsing = $true
    }

    Write-Info "正在下载 trae-proxy $Version (windows/amd64)..."

    try {
        Invoke-WebRequest -Uri "$baseUrl/$AssetName" -OutFile $exePath @requestArgs
        Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile $checksumsPath @requestArgs
    } catch {
        Fail "下载失败。请确认版本 $Version 存在并检查网络连接。"
    }

    return @{
        ExePath = $exePath
        ChecksumsPath = $checksumsPath
    }
}

function Get-ExpectedChecksum {
    param([string]$ChecksumsPath)

    $line = Get-Content -Path $ChecksumsPath |
        Where-Object { $_ -match ([regex]::Escape($AssetName) + '$') } |
        Select-Object -First 1

    if (-not $line) {
        Fail "checksums.txt 中未找到 $AssetName 的校验信息。"
    }

    $parts = $line -split '\s+'
    if ($parts.Count -lt 2 -or [string]::IsNullOrWhiteSpace($parts[0])) {
        Fail 'checksums.txt 格式无效，无法解析 SHA256。'
    }

    return $parts[0].ToLowerInvariant()
}

function Verify-Checksum {
    param(
        [string]$ExePath,
        [string]$ChecksumsPath
    )

    Write-Host '正在校验文件完整性...' -NoNewline

    $expected = Get-ExpectedChecksum -ChecksumsPath $ChecksumsPath
    $actual = (Get-FileHash -Path $ExePath -Algorithm SHA256).Hash.ToLowerInvariant()

    if ($actual -ne $expected) {
        Write-Host ''
        Fail "校验失败！文件可能已损坏。`n  期望: $expected`n  实际: $actual"
    }

    Write-Host ' 校验通过 ✓'
}

function Ensure-InstallDirectory {
    New-Item -Path $InstallDir -ItemType Directory -Force | Out-Null
}

function Install-Binary {
    param([string]$ExePath)

    try {
        Move-Item -Path $ExePath -Destination $InstallPath -Force
    } catch {
        Fail "无法覆盖现有文件：$InstallPath。若 trae-proxy 正在运行，请先执行 `trae-proxy stop` 后重试。"
    }

    Write-Info "安装到 $InstallPath"
}

function Normalize-PathEntry {
    param([string]$PathEntry)

    if ([string]::IsNullOrWhiteSpace($PathEntry)) {
        return ''
    }

    return $PathEntry.Trim().TrimEnd('\').ToLowerInvariant()
}

function Notify-EnvironmentChanged {
    if (-not ('TraeProxy.NativeMethods' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

namespace TraeProxy {
    public static class NativeMethods {
        [DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
        public static extern IntPtr SendMessageTimeout(
            IntPtr hWnd,
            uint Msg,
            UIntPtr wParam,
            string lParam,
            uint fuFlags,
            uint uTimeout,
            out UIntPtr lpdwResult);
    }
}
'@
    }

    $HWND_BROADCAST = [IntPtr]0xffff
    $WM_SETTINGCHANGE = 0x001A
    $SMTO_ABORTIFHUNG = 0x0002
    $result = [UIntPtr]::Zero
    $null = [TraeProxy.NativeMethods]::SendMessageTimeout(
        $HWND_BROADCAST,
        $WM_SETTINGCHANGE,
        [UIntPtr]::Zero,
        'Environment',
        $SMTO_ABORTIFHUNG,
        5000,
        [ref]$result
    )
}

function Update-UserPath {
    $envKey = 'HKCU:\Environment'
    $currentUserPath = (Get-ItemProperty -Path $envKey -Name Path -ErrorAction SilentlyContinue).Path
    $segments = @()

    if (-not [string]::IsNullOrWhiteSpace($currentUserPath)) {
        $segments = $currentUserPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    }

    $normalizedInstallDir = Normalize-PathEntry -PathEntry $InstallDir
    $exists = $false
    foreach ($segment in $segments) {
        if ((Normalize-PathEntry -PathEntry $segment) -eq $normalizedInstallDir) {
            $exists = $true
            break
        }
    }

    if ($exists) {
        Write-Info '安装目录已存在于用户 PATH'
        $script:PathUpdated = $true
        return
    }

    $newUserPath = if ([string]::IsNullOrWhiteSpace($currentUserPath)) {
        $InstallDir
    } else {
        "$currentUserPath;$InstallDir"
    }

    if ($newUserPath.Length -gt $MaxUserEnvVarLength) {
        Write-Warning "用户 PATH 超过 $MaxUserEnvVarLength 个字符，已跳过自动写入。请手动将 $InstallDir 添加到用户 PATH。"
        return
    }

    New-ItemProperty -Path $envKey -Name Path -Value $newUserPath -PropertyType ExpandString -Force | Out-Null
    try {
        Notify-EnvironmentChanged
    } catch {
        Write-Warning '已写入用户 PATH，但未能通知系统刷新环境变量；新开的终端可能需要重启后才会读取到新值。'
    }
    Write-Info '已将安装目录添加到用户 PATH'
    $script:PathUpdated = $true
}

function Refresh-CurrentSessionPath {
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')

    if ([string]::IsNullOrEmpty($machinePath)) {
        $env:Path = $userPath
        return
    }

    if ([string]::IsNullOrEmpty($userPath)) {
        $env:Path = $machinePath
        return
    }

    $env:Path = $machinePath + ';' + $userPath
}

function Show-NextSteps {
    param([string]$Version)

    Write-Host ''
    Write-Host "✅ trae-proxy $Version 安装完成！"
    Write-Host ''
    Write-Host '下一步（需在管理员 PowerShell 中运行）：'
    Write-Host '  trae-proxy init       # 生成证书并安装到系统信任库'
    Write-Host '  trae-proxy start -d   # 启动代理（后台运行）'
    Write-Host '  trae-proxy status     # 查看运行状态'
    Write-Host ''
    Write-Host '提示：若首次运行被 Windows Defender / SmartScreen 拦截，请确认发布来源后选择"仍要运行"。'
    if ($script:PathUpdated) {
        Write-Host '提示：若当前窗口提示找不到 trae-proxy 命令，请关闭并重新打开 PowerShell。'
    } else {
        Write-Host "提示：请确认用户 PATH 包含 $InstallDir；若当前窗口提示找不到 trae-proxy 命令，请关闭并重新打开 PowerShell。"
    }
}

$tempDir = $null
try {
    Write-Host ''
    Write-Host '  trae-proxy 安装脚本 (Windows)'
    Write-Host '  =============================='
    Write-Host ''

    Test-SupportedArchitecture
    $version = Get-TargetVersion
    $tempDir = New-TemporaryDirectory
    $dl = Download-ReleaseFiles -Version $version -TempDir $tempDir
    Verify-Checksum -ExePath $dl.ExePath -ChecksumsPath $dl.ChecksumsPath
    Ensure-InstallDirectory
    Install-Binary -ExePath $dl.ExePath
    Update-UserPath
    Refresh-CurrentSessionPath
    Show-NextSteps -Version $version
} finally {
    if ($tempDir -and (Test-Path -Path $tempDir)) {
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

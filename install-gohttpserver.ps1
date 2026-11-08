# @Author      ：幸运锦鲤
# @Time        : 2026-08-02 18:00:00
# @version     : powershell
# @Update time : 2026-11-08
# @Description : gohttpserver 一键安装脚本（Windows 版，对应 install-gohttpserver.sh）

<#
.SYNOPSIS
    gohttpserver 一键安装脚本（Windows PowerShell 版）

.DESCRIPTION
    从 GitHub / Gitee 镜像下载 gohttpserver 发布版二进制，交互式配置
    端口与功能开关，并可选择以「计划任务（开机自启）」或「手动运行」
    方式常驻。提供安装 / 升级 / 卸载 / 状态 / 日志 / 重启 / 停止菜单。

.EXAMPLE
    .\install-gohttpserver.ps1
    进入交互式主菜单

.EXAMPLE
    .\install-gohttpserver.ps1 -Help
    显示帮助信息

.NOTES
    需要以管理员身份运行 PowerShell（安装到 Program Files、注册计划任务）。
    若遇到执行策略限制，可先执行:
        Set-ExecutionPolicy Bypass -Scope Process
#>

[CmdletBinding()]
param(
    [switch] $Help
)

$ErrorActionPreference = 'Stop'

# GitHub / Gitee 均要求 TLS 1.2，Windows PowerShell 5.1 默认可能未启用
try {
    [Net.ServicePointManager]::SecurityProtocol =
        [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch { }

# -----------------------------------------------------------------------------
# 日志输出
# -----------------------------------------------------------------------------
function Write-Info    { param([string]$Msg); Write-Host "[INFO]  $Msg" -ForegroundColor Green }
function Write-Success { param([string]$Msg); Write-Host "[OK]    $Msg" -ForegroundColor Green }
function Write-Warn    { param([string]$Msg); Write-Host "[WARN]  $Msg" -ForegroundColor Yellow }
function Write-Err     { param([string]$Msg); Write-Host "[ERROR] $Msg" -ForegroundColor Red }
function Write-Step    { param([string]$Msg); Write-Host "[STEP]  $Msg" -ForegroundColor Cyan }

# Windows PowerShell 5.1 下，ErrorActionPreference=Stop 会把原生程序（exe）的
# stderr 输出包装成 NativeCommandError 并以终止性错误抛出（例如 gohttpserver
# 启动时的 WARNING 日志、schtasks 的本地化错误信息）。此函数临时切换到
# Continue 再执行原生命令，结果以 LASTEXITCODE 判断成败。
function Invoke-Native {
    param([scriptblock] $Command)
    $old = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try { & $Command } finally { $ErrorActionPreference = $old }
}

# -----------------------------------------------------------------------------
# 全局变量
# -----------------------------------------------------------------------------
$InstallDir   = Join-Path $env:ProgramFiles 'gohttpserver'
$DataDir      = Join-Path $InstallDir 'data'
$ServiceName  = 'gohttpserver'
$BinaryPath   = Join-Path $InstallDir 'gohttpserver.exe'
$DbFile       = Join-Path $InstallDir 'gohttpserver.db'
$LauncherPath = Join-Path $InstallDir 'start-gohttpserver.bat'
$LogFile      = Join-Path $InstallDir 'gohttpserver.log'
$DefaultPort  = 8000
$ListenPort   = 0
$Repo         = 'toluckykoi/gohttpserver'

# 镜像源: github 或 gitee（根据网络环境自动检测）
$Mirror  = 'github'

# 功能选项
$EnableUpload = $false
$EnableDelete = $false
$EnableEdit   = $false
$EnableLogin  = $false
$RootDir      = ''

# -----------------------------------------------------------------------------
# 帮助
# -----------------------------------------------------------------------------
function Show-Help {
    $help = @"
gohttpserver 一键安装脚本 (Windows PowerShell)

用法:
  在管理员 PowerShell 中执行:
      .\install-gohttpserver.ps1

说明:
  - 需要管理员权限（安装到 Program Files、注册计划任务）
  - 交互菜单: 安装 / 升级 / 卸载 / 状态 / 日志 / 重启 / 停止
  - 运行方式二选一: 计划任务（开机自启，推荐）/ 仅手动运行
  - 下载源自动检测: GitHub，访问不通或中国时区时回退 Gitee 镜像
  - 安装目录: $InstallDir

选项:
  -Help    显示此帮助信息

若遇到执行策略限制，可先执行:
  Set-ExecutionPolicy Bypass -Scope Process
"@
    Write-Host $help
}

if ($Help) { Show-Help; exit 0 }

# ==========================================================
# TUI / 交互层（纯文本菜单，对应 bash 版的 ui_* 函数）
# ==========================================================

# 菜单选择
# 用法: Read-Menu "标题" "提示" @("item1", "item2", ...)
# 返回: 用户输入的选项字符串
function Read-Menu {
    param(
        [string] $Title,
        [string] $Prompt,
        [string[]] $Items
    )
    Write-Host ''
    Write-Host '==============================================================' -ForegroundColor Cyan
    Write-Host "  $Title" -ForegroundColor Cyan
    Write-Host '==============================================================' -ForegroundColor Cyan
    Write-Host $Prompt
    Write-Host ''
    for ($i = 0; $i -lt $Items.Count; $i++) {
        Write-Host ('  {0}) {1}' -f ($i + 1), $Items[$i])
    }
    Write-Host ''
    return (Read-Host '输入选项').Trim()
}

# 输入框（带默认值）
function Read-Input {
    param(
        [string] $Prompt,
        [string] $Default
    )
    $v = (Read-Host "$Prompt [默认: $Default]").Trim()
    if ($v) { return $v } else { return $Default }
}

# 是/否确认（默认 是）
function Confirm-YesNo {
    param([string] $Prompt)
    $c = (Read-Host "$Prompt (Y/n)").Trim()
    return ($c -notmatch '^[Nn]$')
}

# 信息提示框（显示后暂停）
function Show-MessageBox {
    param(
        [string] $Title,
        [string] $Content
    )
    Write-Host ''
    Write-Host '==============================================================' -ForegroundColor Cyan
    Write-Host "  $Title" -ForegroundColor Cyan
    Write-Host '==============================================================' -ForegroundColor Cyan
    Write-Host $Content
    Write-Host '==============================================================' -ForegroundColor Cyan
    Read-Host '按回车键继续...' | Out-Null
}

# 暂停
function Show-Pause {
    Write-Host ''
    Read-Host '按回车键继续...' | Out-Null
}

# 显示横幅
function Show-Banner {
    Clear-Host
    $mirrorDisplay = if ($script:Mirror -eq 'gitee') { 'Gitee' } else { 'GitHub' }
    Write-Host '=============================================================='
    Write-Host '         GoHTTPServer Installer (Windows)'
    Write-Host '    https://github.com/toluckykoi/gohttpserver'
    Write-Host "    下载源: $mirrorDisplay"
    Write-Host '=============================================================='
    Write-Host ''
}

# ==========================================================
# 基础检查
# ==========================================================

# 检测管理员权限（对应 bash 版的 check_root）
function Test-Admin {
    $identity  = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Assert-Admin {
    if (-not (Test-Admin)) {
        Write-Err '请以管理员身份运行 PowerShell 后重试'
        Write-Info '提示: 开始菜单搜索 PowerShell -> 右键 -> 以管理员身份运行'
        exit 1
    }
}

# 检测系统架构
# 返回产物文件名中的架构标识（与 build.sh 的产物命名一致）
function Get-Arch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    # 32 位 PowerShell 跑在 64 位系统上时，真实架构在 PROCESSOR_ARCHITEW6432
    if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
    switch ($arch) {
        'AMD64' { return 'x64' }
        'ARM64' { return 'arm64' }
        default {
            Write-Err "不支持的架构: $arch"
            exit 1
        }
    }
}

# 检测网络环境，自动选择镜像源
function Select-Mirror {
    Write-Step '检测网络环境...'

    # 方法1: 检测时区（中国标准时间）
    $tz = [System.TimeZoneInfo]::Local.Id
    if ($tz -match 'China|Shanghai|Chongqing|Urumqi') {
        $script:Mirror = 'gitee'
        Write-Info "检测到中国时区: $tz，使用 Gitee 镜像"
        return
    }

    # 方法2: 尝试访问 GitHub，失败则回退 Gitee
    try {
        $null = Invoke-WebRequest -Uri 'https://github.com' -Method Head `
                    -TimeoutSec 5 -UseBasicParsing -ErrorAction Stop
    } catch {
        $script:Mirror = 'gitee'
        Write-Info '无法访问 GitHub，使用 Gitee 镜像'
        return
    }

    # 方法3: 检测系统语言环境
    if ((Get-Culture).Name -like 'zh-*') {
        $script:Mirror = 'gitee'
        Write-Info '检测到中文语言环境，使用 Gitee 镜像'
        return
    }

    $script:Mirror = 'github'
    Write-Info '使用 GitHub 镜像'
}

# 是否已安装
function Test-Installed {
    return (Test-Path $BinaryPath)
}

# 计划任务是否存在
function Test-TaskExists {
    Invoke-Native { & schtasks /Query /TN $ServiceName *> $null }
    return ($LASTEXITCODE -eq 0)
}

# 进程是否在运行（语言无关，不依赖 schtasks 的本地化输出）
function Test-ProcessRunning {
    return [bool] (Get-Process -Name 'gohttpserver' -ErrorAction SilentlyContinue)
}

# 将 git tag 转换为产物文件名中的版本号（与 build.sh 的产物命名规则一致）:
#   v1.2.0 -> 1.2.0
# 规则：去掉首尾空白和 v 前缀，非法字符统一替换为 -
function Get-VersionFromTag {
    param([string] $Tag)
    return $Tag.Trim() -replace '^[vV]', '' -replace '[^0-9A-Za-z._-]', '-'
}

# 获取下载链接（对应 bash 版的 get_download_url）
function Get-DownloadUrl {
    param([string] $Arch)

    $headers = @{ 'User-Agent' = 'gohttpserver-installer' }

    if ($script:Mirror -eq 'gitee') {
        $apiBase = "https://gitee.com/api/v5/repos/$Repo/releases"
        $dlBase  = "https://gitee.com/$Repo/releases/download"
    } else {
        $apiBase = "https://api.github.com/repos/$Repo/releases"
        $dlBase  = "https://github.com/$Repo/releases/download"
    }

    Write-Info "从 $($script:Mirror) 获取最新稳定版本..."
    $rel = Invoke-RestMethod "$apiBase/latest" -Headers $headers
    $tag = $rel.tag_name
    if (-not $tag) { throw '未找到稳定版本' }

    Write-Info "版本: $tag"
    $ver = Get-VersionFromTag $tag
    # 产物名: gohttpserver-<版本号>-<系统>-<架构>.exe
    return "$dlBase/$tag/gohttpserver-$ver-windows-$Arch.exe"
}

# ==========================================================
# 业务操作
# ==========================================================

# 选择功能选项
function Select-Features {
    # 先询问是否启用登录认证
    Write-Host ''
    if (Confirm-YesNo '是否启用用户名密码登录？(启用后自动开启上传/删除/编辑，默认账号 admin/admin，建议首次登录后修改)') {
        $script:EnableLogin  = $true
        # --login 会自动启用 upload/delete/edit，无需再单独询问
        $script:EnableUpload = $true
        $script:EnableDelete = $true
        $script:EnableEdit   = $true
        Write-Info '已启用登录，自动开启上传/删除/编辑功能'
    } else {
        if (Confirm-YesNo '是否启用文件上传功能？') { $script:EnableUpload = $true }
        if (Confirm-YesNo '是否启用文件删除功能？') { $script:EnableDelete = $true }
        if (Confirm-YesNo '是否启用文件编辑功能？') { $script:EnableEdit   = $true }
    }

    # 数据文件夹路径
    $script:RootDir = Read-Input '请输入数据文件夹路径（服务器上将对外提供的目录）' $DataDir
    New-Item -ItemType Directory -Force -Path $script:RootDir | Out-Null
    Write-Info "数据文件夹: $($script:RootDir)"
}

# 检测二进制文件支持的参数
function Get-SupportedFlags {
    # gohttpserver --help 会向 stderr 打印 WARNING 日志，必须用 Invoke-Native
    # 执行，否则在 ErrorActionPreference=Stop 下会抛出 NativeCommandError
    $helpOutput = Invoke-Native { & $BinaryPath --help 2>&1 | Out-String }
    $flags = @()
    if ($helpOutput -match '--upload') { $flags += 'upload' }
    if ($helpOutput -match '--delete') { $flags += 'delete' }
    if ($helpOutput -match '--edit')   { $flags += 'edit' }
    if ($helpOutput -match '--login')  { $flags += 'login' }
    return ,$flags
}

# 构建启动参数（只添加支持的参数）
function Get-ExecArgs {
    $supported = Get-SupportedFlags
    Write-Info "支持的参数: $($supported -join ' ')"

    $execArgs = "--port $ListenPort --root `"$RootDir`""
    if ($EnableUpload) {
        if ($supported -contains 'upload') { $execArgs += ' --upload' }
        else { Write-Warn '二进制文件不支持 --upload 参数，已跳过' }
    }
    if ($EnableDelete) {
        if ($supported -contains 'delete') { $execArgs += ' --delete' }
        else { Write-Warn '二进制文件不支持 --delete 参数，已跳过' }
    }
    if ($EnableEdit) {
        if ($supported -contains 'edit') { $execArgs += ' --edit' }
        else { Write-Warn '二进制文件不支持 --edit 参数，已跳过' }
    }
    if ($EnableLogin) {
        if ($supported -contains 'login') { $execArgs += ' --login' }
        else {
            Write-Warn '二进制文件不支持 --login 参数，已跳过'
            Write-Warn '提示：请重新编译并发布包含 --login 功能的新版本'
        }
    }
    return $execArgs
}

# 生成启动脚本 start-gohttpserver.bat
# 计划任务与手动运行共用此脚本；cd 到安装目录保证 gohttpserver.db 落在安装目录下
function New-Launcher {
    param([string] $ExecArgs)

    $batContent = @"
@echo off
cd /d "%~dp0"
echo ============================================================
echo  GoHTTPServer (port: $ListenPort)
echo  Log: %~dp0gohttpserver.log
echo ============================================================
gohttpserver.exe $ExecArgs >> "%~dp0gohttpserver.log" 2>&1
"@
    Set-Content -Path $LauncherPath -Value $batContent -Encoding ASCII
    Write-Success "启动脚本已生成: $LauncherPath"
}

# 注册计划任务（开机自启）
function Register-GohttpTask {
    Write-Step '创建计划任务...'
    $out = Invoke-Native { & schtasks /Create /TN $ServiceName /TR "`"$LauncherPath`"" `
               /SC ONSTART /RU SYSTEM /RL HIGHEST /F 2>&1 | Out-String }
    if ($LASTEXITCODE -ne 0) {
        Write-Err "创建计划任务失败: $out"
        return $false
    }
    Write-Success '计划任务创建完成（开机自启，SYSTEM 账户）'

    Write-Step '启动 GoHTTPServer...'
    Invoke-Native { & schtasks /Run /TN $ServiceName *> $null }
    Start-Sleep -Seconds 2
    return (Test-ProcessRunning)
}

# 停止运行中的进程（含计划任务）
function Stop-Gohttp {
    if (Test-TaskExists) {
        Invoke-Native { & schtasks /End /TN $ServiceName *> $null }
    }
    Get-Process -Name 'gohttpserver' -ErrorAction SilentlyContinue |
        Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 1
}

# 显示访问信息
function Show-AccessInfo {
    $ip = 'localhost'
    try {
        $addr = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction Stop |
            Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' } |
            Select-Object -First 1 -ExpandProperty IPAddress
        if ($addr) { $ip = $addr }
    } catch { }

    $content = "安装完成！`n`n"
    $content += "访问信息：`n"
    $content += "  URL: http://${ip}:$ListenPort`n"
    $content += "  数据文件夹: $RootDir`n"

    if ($EnableLogin) {
        $content += "`n登录信息：`n"
        $content += "  默认账号: admin`n"
        $content += "  默认密码: admin`n"
        $content += "  (建议首次登录后修改密码)`n"
    }

    $content += "`n管理命令 (管理员 PowerShell)：`n"
    $content += "  状态: schtasks /Query /TN $ServiceName /V /FO LIST`n"
    $content += "  启动: schtasks /Run /TN $ServiceName`n"
    $content += "  停止: schtasks /End /TN $ServiceName`n"
    $content += "  日志: Get-Content '$LogFile' -Wait -Tail 50"

    Show-MessageBox '安装完成' $content
}

# 安装
function Install-Binary {
    Write-Step '开始二进制安装...'

    if (Test-Installed) {
        Show-MessageBox '提示' "GoHTTPServer 已安装。`n如需升级，请使用主菜单中的升级选项。"
        return
    }

    # 监听端口输入，校验范围 1-65535
    while ($true) {
        $inputPort = Read-Input '请输入 GoHTTPServer 的监听端口 (1-65535)' "$DefaultPort"
        # 用 -as [int] 安全转换：输入超长数字（超出 Int32 范围）时返回 $null，
        # 而不会像 [int] 强制转换那样抛出溢出异常导致脚本崩溃；
        # ^\d+$ 仍用于拒绝小数/负数/字母等非法格式
        $port = $inputPort -as [int]
        if ($inputPort -match '^\d+$' -and $null -ne $port -and $port -ge 1 -and $port -le 65535) {
            $script:ListenPort = $port
            break
        }
        Write-Warn '端口号无效，请输入 1-65535 之间的数字。'
    }
    $busy = Get-NetTCPConnection -LocalPort $ListenPort -State Listen -ErrorAction SilentlyContinue
    if ($busy) {
        Write-Warn "端口 $ListenPort 当前已被占用，安装后服务可能无法启动"
    }

    # 选择功能选项
    Select-Features

    $arch = Get-Arch
    Write-Info "检测到架构: $arch"

    Write-Step "创建安装目录: $InstallDir"
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

    try {
        $downloadUrl = Get-DownloadUrl -Arch $arch
    } catch {
        Show-MessageBox '错误' "获取下载链接失败：$_`n`n请检查网络连接或稍后重试。"
        return
    }

    Write-Step '下载 GoHTTPServer 二进制文件...'
    Write-Info "URL: $downloadUrl"
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $BinaryPath -UseBasicParsing -ErrorAction Stop
    } catch {
        Remove-Item $BinaryPath -Force -ErrorAction SilentlyContinue
        Show-MessageBox '错误' "下载失败：$_`n`n请检查网络连接。"
        return
    }

    # 验证下载的文件是否为有效的 PE 二进制（MZ 头）
    $valid = $false
    try {
        $fs = [System.IO.File]::OpenRead($BinaryPath)
        if ($fs.Length -ge 2) {
            $b0 = $fs.ReadByte(); $b1 = $fs.ReadByte()
            $valid = ($b0 -eq 0x4D -and $b1 -eq 0x5A)  # 'M' 'Z'
        }
        $fs.Close()
    } catch { }
    if (-not $valid) {
        Remove-Item $BinaryPath -Force -ErrorAction SilentlyContinue
        Show-MessageBox '错误' "下载失败：获取到的不是有效的二进制文件。`n`n可能原因：`n1. 该版本的 Release 尚未发布`n2. 下载链接无效`n3. 网络连接问题`n`n请检查 GitHub/Gitee 仓库的 Releases 页面。"
        return
    }

    # 解除网络下载文件的 MOTW 标记，避免 SmartScreen 拦截
    Unblock-File -Path $BinaryPath -ErrorAction SilentlyContinue
    Write-Success "GoHTTPServer 二进制文件安装完成: $BinaryPath"

    # 检测支持的参数并生成启动脚本
    Write-Step '检测二进制文件支持的参数...'
    $execArgs = Get-ExecArgs
    New-Launcher -ExecArgs $execArgs

    # 显示配置信息
    Write-Info '服务配置:'
    Write-Info "  端口: $ListenPort"
    Write-Info "  数据文件夹: $RootDir"
    Write-Info "  上传: $EnableUpload"
    Write-Info "  删除: $EnableDelete"
    Write-Info "  编辑: $EnableEdit"
    Write-Info "  登录: $EnableLogin"

    # 选择运行方式
    $mode = Read-Menu '运行方式' '请选择 GoHTTPServer 的运行方式：' @(
        '计划任务（开机自启，推荐）',
        '仅手动运行（使用启动脚本）'
    )

    if ($mode -eq '2') {
        Show-MessageBox '安装完成' "已跳过服务注册。`n`n手动启动方式：`n  双击运行: $LauncherPath`n  或在命令行执行: `"$LauncherPath`"`n`n日志文件: $LogFile`n`n如需开机自启，可重新运行本脚本使用升级/重装，`n或将启动脚本快捷方式放入 shell:startup 文件夹。"
        return
    }

    if (Register-GohttpTask) {
        Write-Success 'GoHTTPServer 服务启动成功'
        Show-AccessInfo
    } else {
        Show-MessageBox '错误' "GoHTTPServer 未能启动。`n`n可尝试手动运行启动脚本排查：`n  $LauncherPath`n日志文件: $LogFile"
    }
}

# 升级
function Upgrade-GoHTTPServer {
    Write-Step '升级 GoHTTPServer...'

    if (-not (Test-Installed)) {
        Show-MessageBox '错误' 'GoHTTPServer 未安装。请先安装它。'
        return
    }

    $hasTask = Test-TaskExists

    Write-Step '停止 GoHTTPServer...'
    Stop-Gohttp

    Write-Step '备份当前二进制文件...'
    $ts = Get-Date -Format 'yyyyMMdd_HHmmss'
    $backupPath = "$BinaryPath.backup.$ts"
    Copy-Item $BinaryPath $backupPath

    $arch = Get-Arch
    try {
        $downloadUrl = Get-DownloadUrl -Arch $arch
    } catch {
        Write-Err '获取下载链接失败，正在从备份恢复'
        Move-Item $backupPath $BinaryPath -Force
        if ($hasTask) { Invoke-Native { & schtasks /Run /TN $ServiceName *> $null } }
        Show-MessageBox '错误' '获取下载链接失败，已从备份恢复。'
        return
    }

    Write-Step '下载最新版本...'
    $tmpBin = "$BinaryPath.download.tmp"
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tmpBin -UseBasicParsing -ErrorAction Stop
    } catch {
        Remove-Item $tmpBin -Force -ErrorAction SilentlyContinue
        Write-Err '下载失败，正在从备份恢复'
        Move-Item $backupPath $BinaryPath -Force
        if ($hasTask) { Invoke-Native { & schtasks /Run /TN $ServiceName *> $null } }
        Show-MessageBox '错误' "下载失败，已从备份恢复。`n$_"
        return
    }
    Unblock-File -Path $tmpBin -ErrorAction SilentlyContinue

    # 校验下载内容为有效 PE 二进制（MZ 头）后再替换：IWR 对 404/付费墙等
    # 畸形响应不一定会抛错（下载到的是 HTML 错误页），直接覆盖会损坏可用的旧二进制。
    $tmpValid = $false
    try {
        $fs = [System.IO.File]::OpenRead($tmpBin)
        if ($fs.Length -ge 2) {
            $b0 = $fs.ReadByte(); $b1 = $fs.ReadByte()
            $tmpValid = ($b0 -eq 0x4D -and $b1 -eq 0x5A)  # 'M' 'Z'
        }
        $fs.Close()
    } catch { }
    if (-not $tmpValid) {
        Remove-Item $tmpBin -Force -ErrorAction SilentlyContinue
        Write-Err '下载内容不是有效的 PE 二进制，正在从备份恢复'
        Move-Item $backupPath $BinaryPath -Force
        if ($hasTask) { Invoke-Native { & schtasks /Run /TN $ServiceName *> $null } }
        Show-MessageBox '错误' "下载失败：获取到的不是有效的二进制文件，已从备份恢复。"
        return
    }
    Move-Item $tmpBin $BinaryPath -Force

    if ($hasTask) {
        Write-Step '重启 GoHTTPServer...'
        Invoke-Native { & schtasks /Run /TN $ServiceName *> $null }
        Start-Sleep -Seconds 2
        if (Test-ProcessRunning) {
            # 服务确认启动成功后清理本次升级备份，避免长期堆积。
            Remove-Item $backupPath -Force -ErrorAction SilentlyContinue
            Show-MessageBox '升级完成' 'GoHTTPServer 升级成功。'
        } else {
            # 启动失败：保留备份文件，提示用户可手动恢复。
            Show-MessageBox '错误' "服务在升级后未能启动，请检查日志。`n`n旧版本备份保留在： $backupPath"
        }
    } else {
        Show-MessageBox '升级完成' "GoHTTPServer 二进制已升级。`n`n未检测到计划任务，请手动重新启动：`n  $LauncherPath"
    }
}

# 卸载
function Uninstall-GoHTTPServer {
    Write-Step '卸载 GoHTTPServer...'

    if (-not (Test-Installed)) {
        Show-MessageBox '提示' 'GoHTTPServer 未安装。'
        return
    }

    Write-Host ''
    if (-not (Confirm-YesNo '这将删除 GoHTTPServer 二进制文件和服务。您确定要继续吗？')) {
        Write-Info '卸载已取消'
        return
    }

    # 询问是否删除用户数据
    $deleteData = $false
    if (Confirm-YesNo "是否同时删除用户数据？(数据目录: $DataDir，包括共享文件等，此操作不可恢复！)") {
        $deleteData = $true
    }

    # 询问是否删除登录数据库（使用 --login 时生成）
    $deleteDb = $false
    if (Test-Path $DbFile) {
        if (Confirm-YesNo "检测到登录数据库: $DbFile (保存登录账号信息，使用 --login 时生成)。是否一并删除？此操作不可恢复！") {
            $deleteDb = $true
        }
    }

    if (Test-TaskExists) {
        Write-Step '停止并删除计划任务...'
        Stop-Gohttp
        Invoke-Native { & schtasks /Delete /TN $ServiceName /F *> $null }
        Write-Success '计划任务已删除'
    } else {
        Stop-Gohttp
    }

    Write-Step '删除二进制文件与启动脚本...'
    Remove-Item $BinaryPath -Force -ErrorAction SilentlyContinue
    Remove-Item $LauncherPath -Force -ErrorAction SilentlyContinue
    Remove-Item $LogFile -Force -ErrorAction SilentlyContinue
    Write-Success 'GoHTTPServer 二进制文件已删除'

    # 删除登录数据库（含 SQLite 的 -shm / -wal 附属文件）
    if ($deleteDb) {
        Write-Step '删除登录数据库...'
        Remove-Item $DbFile -Force -ErrorAction SilentlyContinue
        Remove-Item "$DbFile-shm" -Force -ErrorAction SilentlyContinue
        Remove-Item "$DbFile-wal" -Force -ErrorAction SilentlyContinue
        Write-Success '登录数据库已删除'
    }

    # 删除用户数据目录
    if ($deleteData) {
        Write-Step '删除用户数据...'
        if (Test-Path $DataDir) {
            Remove-Item $DataDir -Recurse -Force -ErrorAction SilentlyContinue
            Write-Success "用户数据已删除: $DataDir"
        } else {
            Write-Info "数据目录不存在: $DataDir"
        }
    }

    # 不保留任何数据时删除整个安装目录；否则尝试在目录为空时删除
    if ($deleteData -and -not (Test-Path $DbFile)) {
        Write-Step '删除安装目录...'
        Remove-Item $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
        if (-not (Test-Path $InstallDir)) {
            Write-Success "安装目录已删除: $InstallDir"
        }
    } else {
        try {
            Remove-Item $InstallDir -Force -ErrorAction Stop
            Write-Success "安装目录已删除: $InstallDir"
        } catch {
            Write-Info "安装目录 $InstallDir 不为空，未删除"
        }
    }

    # 卸载结果提示
    if (-not (Test-Path $InstallDir)) {
        Show-MessageBox '卸载完成' 'GoHTTPServer 卸载完成。二进制文件、用户数据及安装目录均已删除。'
    } elseif ($deleteData) {
        Show-MessageBox '卸载完成' "GoHTTPServer 卸载完成。`n`n用户数据已删除。`n安装目录保留: $InstallDir"
    } else {
        Show-MessageBox '卸载完成' "GoHTTPServer 卸载完成。`n`n数据文件保留在 $DataDir"
    }
}

# 查看状态
function Show-Status {
    if (-not (Test-Installed)) {
        Show-MessageBox '错误' 'GoHTTPServer 未安装。'
        return
    }
    if (Test-TaskExists) {
        Write-Step '计划任务信息:'
        $taskInfo = Invoke-Native { & schtasks /Query /TN $ServiceName /FO LIST /V 2>&1 | Out-String }
        Write-Host $taskInfo
    } else {
        Write-Info '未注册计划任务（手动运行模式）'
    }
    $proc = Get-Process -Name 'gohttpserver' -ErrorAction SilentlyContinue
    if ($proc) {
        Write-Success "GoHTTPServer 进程运行中 (PID: $(($proc | ForEach-Object { $_.Id }) -join ', '))"
    } else {
        Write-Warn 'GoHTTPServer 进程未运行'
    }
    Show-Pause
}

# 查看日志
function Show-Logs {
    if (-not (Test-Installed)) {
        Show-MessageBox '错误' 'GoHTTPServer 未安装。'
        return
    }
    if (-not (Test-Path $LogFile)) {
        Show-MessageBox '提示' "暂无日志文件: $LogFile"
        return
    }
    Write-Step "查看 GoHTTPServer 服务日志 (按 Ctrl+C 退出): $LogFile"
    Get-Content $LogFile -Tail 50 -Wait
}

# 重启服务
function Restart-Gohttp {
    if (-not (Test-Installed)) {
        Show-MessageBox '错误' 'GoHTTPServer 未安装。'
        return
    }
    Write-Step '重启 GoHTTPServer...'
    Stop-Gohttp
    if (Test-TaskExists) {
        Invoke-Native { & schtasks /Run /TN $ServiceName *> $null }
        Start-Sleep -Seconds 2
        if (Test-ProcessRunning) {
            Show-MessageBox '成功' '服务重启成功。'
        } else {
            Show-MessageBox '错误' '服务重启失败，请检查日志。'
        }
    } else {
        Show-MessageBox '提示' "未注册计划任务，请手动启动：`n  $LauncherPath"
    }
}

# 停止服务
function Stop-GohttpService {
    if (-not (Test-Installed)) {
        Show-MessageBox '错误' 'GoHTTPServer 未安装。'
        return
    }
    Write-Step '停止 GoHTTPServer...'
    Stop-Gohttp
    Show-MessageBox '成功' '服务已停止。'
}

# -----------------------------------------------------------------------------
# 主菜单
# -----------------------------------------------------------------------------
function Show-MainMenu {
    while ($true) {
        Show-Banner

        $choice = Read-Menu 'GoHTTPServer 安装器' '请选择操作：' @(
            '安装 GoHTTPServer',
            '升级 GoHTTPServer',
            '卸载 GoHTTPServer',
            '查看状态',
            '查看日志',
            '重启服务',
            '停止服务',
            '退出'
        )

        switch ($choice) {
            '1' { Install-Binary }
            '2' { Upgrade-GoHTTPServer }
            '3' { Uninstall-GoHTTPServer }
            '4' { Show-Status }
            '5' { Show-Logs }
            '6' { Restart-Gohttp }
            '7' { Stop-GohttpService }
            '8' { exit 0 }
            default { Write-Warn '无效选项' ; Start-Sleep -Seconds 1 }
        }
    }
}

# -----------------------------------------------------------------------------
# 主流程
# -----------------------------------------------------------------------------
Assert-Admin
Select-Mirror
Show-MainMenu

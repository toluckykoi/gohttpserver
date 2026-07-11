# @Author      ：幸运锦鲤
# @Time        : 2026-07-11 19:18:52
# @version     : powershell
# @Update time :
# @Description : gohttpserver 一键构建脚本

<#
.SYNOPSIS
    gohttpserver 一键构建脚本（PowerShell 版）

.DESCRIPTION
    构建前端（Vue 3 + Vite）并将产物嵌入到 Go 二进制中，
    交叉编译 5 个平台目标到 dist/ 目录。

.EXAMPLE
    .\build.ps1
    完整构建前端 + 多平台二进制

.EXAMPLE
    .\build.ps1 -SkipFrontend
    跳过前端构建（要求 frontend/dist 已存在）

.EXAMPLE
    .\build.ps1 -Clean
    清理 dist/ 与 frontend/dist 后退出

.EXAMPLE
    .\build.ps1 -Help
    显示帮助信息

.NOTES
    环境变量 EX_LDFLAGS 可追加额外的 Go ldflags。
#>

[CmdletBinding()]
param(
    [switch] $SkipFrontend,
    [switch] $Clean,
    [switch] $Help
)

$ErrorActionPreference = 'Stop'

# -----------------------------------------------------------------------------
# 日志输出
# -----------------------------------------------------------------------------
function Write-Info  { param([string]$Msg); Write-Host "[INFO]  $Msg" -ForegroundColor Green }
function Write-Warn  { param([string]$Msg); Write-Host "[WARN]  $Msg" -ForegroundColor Yellow }
function Write-Err   { param([string]$Msg); Write-Host "[ERROR] $Msg" -ForegroundColor Red }
function Write-Step  { param([string]$Msg); Write-Host "[STEP]  $Msg" -ForegroundColor Cyan }

# -----------------------------------------------------------------------------
# 显示帮助
# -----------------------------------------------------------------------------
function Show-Help {
    $help = @"
gohttpserver 一键构建脚本 (PowerShell)

用法:
  .\build.ps1 [选项]

选项:
  -SkipFrontend   跳过前端构建（要求 frontend/dist 已存在）
  -Clean          清理 dist/ 与 frontend/dist 后退出
  -Help           显示此帮助信息

环境变量:
  EX_LDFLAGS      追加额外的 Go ldflags（例: `$env:EX_LDFLAGS="-s -w"）

构建目标:
  linux-amd64, linux-arm64, darwin-amd64, darwin-arm64,
  windows-amd64, windows-arm64
"@
    Write-Host $help
}

if ($Help) { Show-Help; exit 0 }

# -----------------------------------------------------------------------------
# 切换到脚本所在目录
# -----------------------------------------------------------------------------
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

# -----------------------------------------------------------------------------
# 清理
# -----------------------------------------------------------------------------
if ($Clean) {
    Write-Step '清理构建产物'
    foreach ($p in @('dist', 'frontend/dist')) {
        if (Test-Path $p) {
            Remove-Item -Recurse -Force $p
            Write-Info "已删除 $p"
        }
    }
    Write-Info '清理完成'
    exit 0
}

# -----------------------------------------------------------------------------
# 依赖检查
# -----------------------------------------------------------------------------
Write-Step '检查构建依赖'

function Test-Command {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        Write-Err "未找到命令: $Name，请先安装"
        exit 1
    }
}

Test-Command go
Test-Command node
Test-Command npm

$GoVersion   = & go version 2>&1 | Out-String
$GoVersion   = $GoVersion -replace '^go version ', '' -replace "`r`n", ''
$NodeVersion = & node --version 2>&1 | Out-String
$NodeVersion = $NodeVersion.Trim()
Write-Info "Go: $GoVersion, Node: $NodeVersion"

# -----------------------------------------------------------------------------
# 前端构建
# -----------------------------------------------------------------------------
if ($SkipFrontend) {
    if (-not (Test-Path 'frontend/dist')) {
        Write-Err '--SkipFrontend 已指定，但 frontend/dist 不存在，请先执行一次完整构建'
        exit 1
    }
    Write-Warn '跳过前端构建，使用已有的 frontend/dist'
} else {
    Write-Step '构建前端 (frontend/)'
    Set-Location frontend

    # 没有装依赖时先安装。package-lock.json 存在则用 npm ci 获得可复现安装。
    if (-not (Test-Path 'node_modules')) {
        if (Test-Path 'package-lock.json') {
            Write-Info '安装依赖 (npm ci)'
            npm ci
            if ($LASTEXITCODE -ne 0) { Write-Err 'npm ci 失败'; exit 1 }
        } else {
            Write-Info '安装依赖 (npm install)'
            npm install
            if ($LASTEXITCODE -ne 0) { Write-Err 'npm install 失败'; exit 1 }
        }
    } else {
        Write-Info 'node_modules 已存在，跳过依赖安装'
    }

    Write-Info '执行 npm run build'
    npm run build
    if ($LASTEXITCODE -ne 0) { Write-Err '前端构建失败'; exit 1 }

    Set-Location $ScriptDir
    Write-Info '前端构建完成: frontend/dist'
}

# -----------------------------------------------------------------------------
# 版本信息
# -----------------------------------------------------------------------------
Write-Step '计算版本信息'

function Invoke-GitSafe {
    param([string]$Args)
    $output = git $Args.Split(' ') 2>$null
    if ($LASTEXITCODE -eq 0) { return $output } else { return $null }
}

$Version = Invoke-GitSafe 'describe --abbrev=0 --tags'
if (-not $Version) { $Version = if (Test-Path 'VERSION') { (Get-Content VERSION).Trim() } else { 'v0.0.0' } }

$RevCnt  = Invoke-GitSafe 'rev-list --count HEAD'
$DevCnt  = Invoke-GitSafe "rev-list --count $Version"
if (-not $RevCnt) { $RevCnt = '0' }
if (-not $DevCnt) { $DevCnt = '0' }

if ($RevCnt -ne $DevCnt) {
    $Version = "$Version.dev$([int]$RevCnt - [int]$DevCnt)"
}
Write-Host "VER: $Version"

$GitCommit = Invoke-GitSafe 'rev-parse HEAD'
if (-not $GitCommit) { $GitCommit = 'unknown' }
$BuildTime = (Get-Date).ToString('yyyy/MM/dd-HH:mm:ss')

# -s -w 裁掉调试信息与符号表，显著减小二进制体积
$LdFlags = "-s -w -X main.VERSION=$Version -X main.BUILDTIME=$BuildTime -X main.GITCOMMIT=$GitCommit"
if ($env:EX_LDFLAGS) {
    $LdFlags = "$LdFlags $env:EX_LDFLAGS"
}

# -----------------------------------------------------------------------------
# Go 多平台构建
# -----------------------------------------------------------------------------
Write-Step '构建多平台二进制'

if (-not (Test-Path 'dist')) { New-Item -ItemType Directory -Path 'dist' | Out-Null }

# 目标定义: GOOS, GOARCH, 输出文件名后缀
$Targets = @(
    @{ GOOS = 'linux';   GOARCH = 'amd64'; Name = 'linux-amd64' },
    @{ GOOS = 'linux';   GOARCH = 'arm64'; Name = 'linux-arm64' },
    @{ GOOS = 'darwin';  GOARCH = 'amd64'; Name = 'mac-amd64' },
    @{ GOOS = 'darwin';  GOARCH = 'arm64'; Name = 'mac-arm64' },
    @{ GOOS = 'windows'; GOARCH = 'amd64'; Name = 'win-amd64.exe' },
    @{ GOOS = 'windows'; GOARCH = 'arm64'; Name = 'win-arm64.exe' }
)

foreach ($t in $Targets) {
    $outFile = "dist/gohttpserver-$($t.Name)"
    Write-Host "  -> $($t.GOOS)/$($t.GOARCH) ..."

    $env:CGO_ENABLED = '0'
    $env:GOOS        = $t.GOOS
    $env:GOARCH      = $t.GOARCH

    & go build -trimpath -ldflags $LdFlags -o $outFile
    if ($LASTEXITCODE -ne 0) {
        Write-Err "构建失败: $($t.GOOS)/$($t.GOARCH)"
        exit 1
    }
}

# 还原环境变量，避免污染当前 shell
$env:CGO_ENABLED = $null
$env:GOOS        = $null
$env:GOARCH      = $null

# -----------------------------------------------------------------------------
# 汇总
# -----------------------------------------------------------------------------
Write-Step '构建完成'
Write-Host ''
Write-Host '产物清单:'
Get-ChildItem dist | ForEach-Object {
    $size = '{0:N1} KB' -f ($_.Length / 1KB)
    Write-Host ('  {0,-30} {1}' -f $_.Name, $size)
}
Write-Host ''
Write-Info "版本: $Version"
Write-Info "提交: $GitCommit"
Write-Info "构建时间: $BuildTime"

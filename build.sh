#!/bin/bash

# @Author      ：幸运锦鲤
# @Time        : 2026-07-11 19:18:52
# @version     : bash
# @Update time : 2026-10-24
# @Description : gohttpserver 一键构建脚本（产物名: gohttpserver-版本-通道-系统-架构）

# 用法:
#   ./build.sh                 # 构建前端 + 多平台 Go 二进制
#   ./build.sh --skip-frontend # 跳过前端构建（需 frontend/dist 已存在）
#   ./build.sh --clean         # 清理构建产物后退出
#   ./build.sh --help          # 显示帮助
#
# 环境变量:
#   EX_LDFLAGS  追加额外的 Go ldflags

set -euo pipefail

# -----------------------------------------------------------------------------
# 颜色输出
# -----------------------------------------------------------------------------
if [[ -t 1 ]]; then
    COLOR_RESET=$'\033[0m'
    COLOR_GREEN=$'\033[32m'
    COLOR_YELLOW=$'\033[33m'
    COLOR_RED=$'\033[31m'
    COLOR_BLUE=$'\033[34m'
else
    COLOR_RESET=""
    COLOR_GREEN=""
    COLOR_YELLOW=""
    COLOR_RED=""
    COLOR_BLUE=""
fi

log_info()  { echo "${COLOR_GREEN}[INFO]${COLOR_RESET}  $*"; }
log_warn()  { echo "${COLOR_YELLOW}[WARN]${COLOR_RESET}  $*"; }
log_error() { echo "${COLOR_RED}[ERROR]${COLOR_RESET} $*" >&2; }
log_step()  { echo "${COLOR_BLUE}[STEP]${COLOR_RESET}  $*"; }

# -----------------------------------------------------------------------------
# 脚本目录（兼容 macOS bash 3.x）
# -----------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# -----------------------------------------------------------------------------
# 参数解析
# -----------------------------------------------------------------------------
SKIP_FRONTEND=false
CLEAN_ONLY=false

show_help() {
    cat <<EOF
gohttpserver 一键构建脚本

用法:
  ./build.sh [选项]

选项:
  --skip-frontend   跳过前端构建（要求 frontend/dist 已存在）
  --clean           清理 dist/ 与 frontend/dist 后退出
  -h, --help        显示此帮助信息

环境变量:
  EX_LDFLAGS        追加额外的 Go ldflags（例: EX_LDFLAGS="-s -w"）

构建目标:
  linux-x64, linux-arm64, macos-x64, macos-arm64,
  windows-x64, windows-arm64（Windows 产物带 .exe 后缀）

产物命名:
  gohttpserver-<版本>-<stable|beta.N>-<系统>-<架构>[.exe]
  例: gohttpserver-1.2.0-stable-windows-x64.exe
      gohttpserver-1.3.0-beta.1-macos-arm64
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-frontend) SKIP_FRONTEND=true; shift ;;
        --clean)         CLEAN_ONLY=true; shift ;;
        -h|--help)       show_help; exit 0 ;;
        *) log_error "未知参数: $1"; show_help; exit 1 ;;
    esac
done

# -----------------------------------------------------------------------------
# 清理
# -----------------------------------------------------------------------------
if [[ "$CLEAN_ONLY" == "true" ]]; then
    log_step "清理构建产物"
    rm -rf dist
    rm -rf frontend/dist
    log_info "已清理 dist/ 与 frontend/dist"
    exit 0
fi

# -----------------------------------------------------------------------------
# 依赖检查
# -----------------------------------------------------------------------------
log_step "检查构建依赖"

check_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        log_error "未找到命令: $1，请先安装"
        exit 1
    fi
}

check_cmd go
check_cmd node
check_cmd npm

GO_VERSION=$(go version 2>/dev/null | awk '{print $3}')
NODE_VERSION=$(node --version 2>/dev/null)
log_info "Go: ${GO_VERSION}, Node: ${NODE_VERSION}"

# -----------------------------------------------------------------------------
# 前端构建
# -----------------------------------------------------------------------------
if [[ "$SKIP_FRONTEND" == "true" ]]; then
    if [[ ! -d frontend/dist ]]; then
        log_error "--skip-frontend 已指定，但 frontend/dist 不存在，请先执行一次完整构建"
        exit 1
    fi
    log_warn "跳过前端构建，使用已有的 frontend/dist"
else
    log_step "构建前端 (frontend/)"
    cd frontend

    # 没有装依赖时先安装。package-lock.json 存在则用 npm ci 获得可复现安装。
    if [[ ! -d node_modules ]]; then
        if [[ -f package-lock.json ]]; then
            log_info "安装依赖 (npm ci)"
            npm ci
        else
            log_info "安装依赖 (npm install)"
            npm install
        fi
    else
        log_info "node_modules 已存在，跳过依赖安装"
    fi

    log_info "执行 npm run build"
    npm run build

    cd "$SCRIPT_DIR"
    log_info "前端构建完成: frontend/dist"
fi

# -----------------------------------------------------------------------------
# 版本信息
# -----------------------------------------------------------------------------
log_step "计算版本信息"

# 优先从 git tag 获取版本，如果没有则从 VERSION 文件读取，最后使用默认值
VERSION=""
if git describe --abbrev=0 --tags >/dev/null 2>&1; then
    VERSION=$(git describe --abbrev=0 --tags)
elif [[ -f VERSION ]]; then
    VERSION=$(cat VERSION)
else
    VERSION="v0.0.0"
fi

# 检查 VERSION 是否是有效的 git ref
if git rev-parse "$VERSION" >/dev/null 2>&1; then
    # VERSION 是有效的 git ref，计算从该版本到 HEAD 的提交数
    DEVCNT=$(git rev-list --count "$VERSION"..HEAD 2>/dev/null || echo "0")
    if [[ "$DEVCNT" -gt 0 ]]; then
        VERSION="$VERSION.dev$DEVCNT"
    fi
fi
# 用于产物文件名的版本标识：去掉首尾空白和 "v" 前缀，其他非法字符统一替换为 "-"
# （例: 干净 tag -> v1.2.0 -> 1.2.0；tag 后有提交 -> v1.2.0.dev3 -> 1.2.0.dev3）
FILE_VERSION=$(echo "$VERSION" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^[vV]//' -e 's#[^0-9A-Za-z._-]#-#g')
if [[ -z "$FILE_VERSION" ]]; then
    FILE_VERSION="0.0.0"
fi

# 拆分基础版本与发布通道，通道只有 stable / beta 两种:
#   1.2.0        -> 基础版本 1.2.0，通道 stable
#   1.3.0-beta.1 -> 基础版本 1.3.0，通道 beta.1
#   其他情况（tag 后有提交的 dev 构建、rc 等）一律归入 beta 通道，
#   基础版本只取 X.Y.Z，dev/rc 等后缀不进产物名
BASE_VERSION=""
CHANNEL=""
if [[ "$FILE_VERSION" =~ ^([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
    BASE_VERSION="$FILE_VERSION"
    CHANNEL="stable"
elif [[ "$FILE_VERSION" =~ ^([0-9]+\.[0-9]+\.[0-9]+)-beta(\.[0-9A-Za-z.]+)?$ ]]; then
    BASE_VERSION="${BASH_REMATCH[1]}"
    CHANNEL="beta${BASH_REMATCH[2]}"
elif [[ "$FILE_VERSION" =~ ^([0-9]+\.[0-9]+\.[0-9]+) ]]; then
    BASE_VERSION="${BASH_REMATCH[1]}"
    CHANNEL="beta"
else
    BASE_VERSION="$FILE_VERSION"
    CHANNEL="beta"
fi

echo "VER: $VERSION  (产物名: gohttpserver-${BASE_VERSION}-${CHANNEL}-<系统>-<架构>)"

GITCOMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILDTIME=$(date +%Y/%m/%d-%H:%M:%S)

# -s -w 裁掉调试信息与符号表，显著减小二进制体积
LDFLAGS="-s -w -X main.VERSION=$VERSION -X main.BUILDTIME=$BUILDTIME -X main.GITCOMMIT=$GITCOMMIT"
if [[ -n "${EX_LDFLAGS:-}" ]]; then
    LDFLAGS="$LDFLAGS $EX_LDFLAGS"
fi

# -----------------------------------------------------------------------------
# Go 多平台构建
# -----------------------------------------------------------------------------
log_step "构建多平台二进制"

mkdir -p dist

build() {
    local goos=$1 goarch=$2 osname=$3 archname=$4 ext=$5
    echo "  -> $goos/$goarch ..."
    CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build \
        -trimpath \
        -ldflags "$LDFLAGS" \
        -o "dist/gohttpserver-${BASE_VERSION}-${CHANNEL}-${osname}-${archname}${ext}"
}

build linux   amd64 linux   x64   ""
build linux   arm64 linux   arm64 ""
build darwin  amd64 macos   x64   ""
build darwin  arm64 macos   arm64 ""
build windows amd64 windows x64   ".exe"
build windows arm64 windows arm64 ".exe"

# -----------------------------------------------------------------------------
# 汇总
# -----------------------------------------------------------------------------
log_step "构建完成"
echo
echo "产物清单:"
ls -lh dist/ | awk 'NR>1 {printf "  %-30s %s %s\n", $9, $5, $9}'
echo
log_info "版本: $VERSION"
log_info "提交: $GITCOMMIT"
log_info "构建时间: $BUILDTIME"

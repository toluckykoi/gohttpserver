#!/bin/bash

# @Author      ：幸运锦鲤
# @Time        : 2026-07-11 19:18:52
# @version     : bash
# @Update time :
# @Description : gohttpserver 一键构建脚本

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
  linux-amd64, linux-arm64, darwin-amd64, darwin-arm64,
  windows-amd64, windows-arm64
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

VERSION=$(git describe --abbrev=0 --tags 2>/dev/null || cat VERSION 2>/dev/null || echo "v0.0.0")
REVCNT=$(git rev-list --count HEAD 2>/dev/null || echo 0)
DEVCNT=$(git rev-list --count "$VERSION" 2>/dev/null || echo 0)
if [[ "$REVCNT" != "$DEVCNT" ]]; then
    VERSION="$VERSION.dev$((REVCNT - DEVCNT))"
fi
echo "VER: $VERSION"

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
    local goos=$1 goarch=$2 name=$3
    echo "  -> $goos/$goarch ..."
    CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build \
        -trimpath \
        -ldflags "$LDFLAGS" \
        -o "dist/gohttpserver-${name}"
}

build linux   amd64 linux-amd64
build linux   arm64 linux-arm64
build darwin  amd64 mac-amd64
build darwin  arm64 mac-arm64
build windows amd64 win-amd64.exe
build windows arm64 win-arm64.exe

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

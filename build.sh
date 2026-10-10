#!/bin/bash
# kshell 编译脚本（Linux / macOS）
# 用法：
#   ./build.sh              # vet + 编译 TUI 到 dist/kshell
#   ./build.sh -Test        # 先跑全量测试再编译
#   ./build.sh -Clean       # 先清空 dist 再编译
#   ./build.sh -Desktop     # wails build 桌面版（自动构建前端），产物拷到 dist/kshell-desktop
#   ./build.sh -Desktop -Version v0.2.0  # 注入版本号（GitHub Release 用）
#   ./build.sh -TUI         # 仅 TUI，等价于默认行为（保留与其他平台的参数一致）
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

DESKTOP=false
TEST=false
CLEAN=false
VERSION=''

while [[ $# -gt 0 ]]; do
    case "$1" in
        -Desktop) DESKTOP=true; shift ;;
        -Test)   TEST=true;   shift ;;
        -Clean)  CLEAN=true;  shift ;;
        -TUI)    shift ;;
        -Version)
            if [[ -z "$2" || "$2" == -* ]]; then
                echo "ERROR: -Version requires a value" >&2; exit 1
            fi
            VERSION="$2"; shift 2 ;;
        *)
            echo "Usage: $0 [-TUI] [-Desktop] [-Version vX.Y.Z] [-Test] [-Clean]" >&2
            exit 1 ;;
    esac
done

# ---- Go 探测 ----
if ! command -v go &>/dev/null; then
    for candidate in /usr/local/go/bin /opt/homebrew/bin/go/bin /usr/local/bin/go ~/go/bin/go; do
        if [[ -x "$candidate/go" ]]; then
            export PATH="$candidate:$PATH"
            break
        fi
    done
fi
if ! command -v go &>/dev/null; then
    echo '[ERROR] go not found. Install from https://go.dev/dl/' >&2
    exit 1
fi

GO_VERSION=$(go version | grep -oP 'go\d+\.\d+')
echo "[INFO] Go: $(go version)"

# ---- Desktop 模式：Wails 探测 ----
if $DESKTOP; then
    if ! command -v wails &>/dev/null; then
        for candidate in \
            "$HOME/go/bin/wails" \
            "/usr/local/go/bin/wails" \
            "/opt/homebrew/bin/wails"; do
            if [[ -x "$candidate" ]]; then
                export PATH="$(dirname "$candidate"):$PATH"
                break
            fi
        done
    fi
    if ! command -v wails &>/dev/null; then
        echo '[ERROR] wails CLI not found. Run: go install github.com/wailsapp/wails/v2/cmd/wails@latest' >&2
        exit 1
    fi
    echo "[INFO] Wails: $(wails version 2>/dev/null || wails version)"
fi

# ---- Clean ----
if $CLEAN && [[ -d dist ]]; then
    echo '==> rm -rf dist'
    rm -rf dist
fi

# ---- go vet ----
echo '==> go vet ./...'
go vet ./...

# ---- Test ----
if $TEST; then
    echo '==> go test ./... -count=1'
    go test ./... -count=1
fi

# ---- Build ----
if $DESKTOP; then
    # 构建桌面版；wails 会自动执行 frontend npm install + build
    # 成功与否由 exit code 判定
    WAILS_CMD="wails build"
    if [[ -n "$VERSION" ]]; then
        WAILS_CMD="wails build -ldflags \"-X github.com/yangk/kshell/internal/version.Version=$VERSION\""
        echo "==> Desktop + version $VERSION"
    else
        echo '==> wails build'
    fi

    eval $WAILS_CMD

    mkdir -p dist
    # wails 输出文件名由 wails.json "outputfilename": "kshell" 决定，
    # Linux/macOS 无 .exe 后缀；向用户呈现统一名称 kshell-desktop
    if [[ -f build/bin/kshell ]]; then
        cp build/bin/kshell "dist/kshell-desktop"
        echo "==> 完成: dist/kshell-desktop"
    else
        echo "[ERROR] wails build did not produce build/bin/kshell" >&2
        exit 1
    fi
else
    # 构建 TUI
    echo '==> go build -o dist/kshell ./cmd/kshell'
    mkdir -p dist
    go build -o dist/kshell ./cmd/kshell
    echo "==> 完成: dist/kshell"
fi

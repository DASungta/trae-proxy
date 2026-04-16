#!/bin/bash
set -euo pipefail

REPO_OWNER="DASungta"
REPO_NAME="trae-proxy"
BINARY_NAME="trae-proxy"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

fatal() {
    echo "Error: $1" >&2
    exit 1
}

detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        darwin) OS="darwin" ;;
        linux)  OS="linux" ;;
        *)      fatal "不支持的操作系统: $OS（仅支持 macOS 和 Linux）" ;;
    esac

    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *)             fatal "不支持的 CPU 架构: $ARCH" ;;
    esac

    if [ "$OS" = "linux" ] && [ "$ARCH" = "arm64" ]; then
        fatal "暂不支持 linux-arm64，请使用 x86_64 机器"
    fi
}

fetch_latest_version() {
    if [ -n "${VERSION:-}" ]; then
        echo "使用指定版本: $VERSION"
        return
    fi

    echo "正在获取最新版本..."
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest" \
        | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

    if [ -z "$VERSION" ]; then
        fatal "无法获取最新版本。请检查 https://github.com/${REPO_OWNER}/${REPO_NAME}/releases"
    fi
}

check_permissions() {
    if [ ! -d "$INSTALL_DIR" ]; then
        echo "安装目录 ${INSTALL_DIR} 不存在，正在创建..."
        if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
            if [ "$(id -u)" -ne 0 ]; then
                fatal "无法创建 ${INSTALL_DIR}，请用 sudo 运行:\n  curl -fsSL <url> | sudo bash"
            fi
            fatal "无法创建安装目录: ${INSTALL_DIR}"
        fi
    fi
    if [ ! -w "$INSTALL_DIR" ]; then
        if [ "$(id -u)" -ne 0 ]; then
            fatal "无权写入 ${INSTALL_DIR}，请用 sudo 运行:\n  curl -fsSL <url> | sudo bash"
        fi
    fi
}

download_and_verify() {
    FILENAME="${BINARY_NAME}-${OS}-${ARCH}"
    BASE_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${VERSION}"

    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    echo "正在下载 ${BINARY_NAME} ${VERSION} (${OS}/${ARCH})..."
    curl -fSL --progress-bar -o "${TMP_DIR}/${BINARY_NAME}" "${BASE_URL}/${FILENAME}"

    echo "正在校验文件完整性..."
    if curl -fsSL -o "${TMP_DIR}/checksums.txt" "${BASE_URL}/checksums.txt" 2>/dev/null; then
        EXPECTED=$(grep "${FILENAME}$" "${TMP_DIR}/checksums.txt" | awk '{print $1}')
        if [ -n "$EXPECTED" ]; then
            if command -v shasum >/dev/null 2>&1; then
                ACTUAL=$(shasum -a 256 "${TMP_DIR}/${BINARY_NAME}" | awk '{print $1}')
            elif command -v sha256sum >/dev/null 2>&1; then
                ACTUAL=$(sha256sum "${TMP_DIR}/${BINARY_NAME}" | awk '{print $1}')
            else
                echo "  跳过校验（未找到 shasum 或 sha256sum）"
                ACTUAL="$EXPECTED"
            fi

            if [ "$ACTUAL" != "$EXPECTED" ]; then
                fatal "校验失败！文件可能已损坏。\n  期望: $EXPECTED\n  实际: $ACTUAL"
            fi
            echo "  校验通过 ✓"
        fi
    else
        echo "  跳过校验（未找到 checksums.txt）"
    fi
}

install_binary() {
    chmod +x "${TMP_DIR}/${BINARY_NAME}"

    if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        EXISTING=$("${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null | tail -1 || echo "unknown")
        echo "覆盖已有版本: ${EXISTING}"
    fi

    mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
}

main() {
    echo ""
    echo "  trae-proxy 安装脚本"
    echo "  ===================="
    echo ""

    detect_platform
    check_permissions
    fetch_latest_version
    download_and_verify
    install_binary

    echo ""
    echo "✅ trae-proxy ${VERSION} 已安装到 ${INSTALL_DIR}/${BINARY_NAME}"
    echo ""
    echo "下一步："
    echo "  trae-proxy init       # 首次使用：生成证书并安装到系统信任库"
    echo "  trae-proxy start -d   # 启动代理（后台运行）"
    echo "  trae-proxy status          # 查看运行状态"
    echo ""
}

main "$@"

#!/bin/sh
set -e

REPO="${REPO:-bjarneo/cliamp}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
    linux|darwin) ;;
    mingw*|msys*|cygwin*) OS="windows" ;;
    *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

BASE_URL="https://github.com/${REPO}/releases/latest/download"
ARCHIVE="cliamp-${OS}-${ARCH}.tar.gz"
BARE="cliamp-${OS}-${ARCH}"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

download() {
    url="$1"
    dest="$2"
    if command -v curl > /dev/null; then
        curl -fSL -o "$dest" "$url"
    elif command -v wget > /dev/null; then
        wget -qO "$dest" "$url"
    else
        echo "Error: curl or wget required" >&2; exit 1
    fi
}

# Try GoReleaser archive (.tar.gz) first, fall back to bare binary
if download "${BASE_URL}/${ARCHIVE}" "${TMP_DIR}/${ARCHIVE}" 2>/dev/null; then
    echo "Extracting ${ARCHIVE}..."
    tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR" cliamp
    BINARY="${TMP_DIR}/cliamp"
else
    echo "Archive not found, trying bare binary..."
    download "${BASE_URL}/${BARE}" "${TMP_DIR}/cliamp"
    BINARY="${TMP_DIR}/cliamp"
fi

chmod +x "$BINARY"

if [ -w "$INSTALL_DIR" ]; then
    mv "$BINARY" "${INSTALL_DIR}/cliamp"
else
    sudo mv "$BINARY" "${INSTALL_DIR}/cliamp"
fi

echo "Installed cliamp to ${INSTALL_DIR}/cliamp"

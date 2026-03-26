#!/bin/sh
set -e

REPO="Zxela/claude-monitor"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin) OS="darwin" ;;
  linux)  OS="linux" ;;
  *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64)  ARCH="arm64" ;;
  *)              echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

BINARY="claude-monitor-${OS}-${ARCH}"

# Get latest release tag
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$TAG" ]; then
  echo "Error: could not determine latest release"
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${BINARY}.tar.gz"

# Download and extract
echo "Downloading claude-monitor ${TAG} (${OS}/${ARCH})..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL "$URL" -o "${TMPDIR}/${BINARY}.tar.gz"
tar xzf "${TMPDIR}/${BINARY}.tar.gz" -C "$TMPDIR"

# Install
mkdir -p "$INSTALL_DIR"
mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/claude-monitor"
chmod +x "${INSTALL_DIR}/claude-monitor"

# macOS: remove quarantine attribute
if [ "$OS" = "darwin" ]; then
  xattr -cr "${INSTALL_DIR}/claude-monitor" 2>/dev/null || true
fi

echo ""
echo "claude-monitor ${TAG} installed to ${INSTALL_DIR}/claude-monitor"

# Check if INSTALL_DIR is in PATH
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "NOTE: ${INSTALL_DIR} is not in your PATH."
    echo "Add it with:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    echo ""
    echo "Or add that line to your ~/.bashrc or ~/.zshrc"
    ;;
esac

echo ""
echo "Run:  claude-monitor"
echo "Open: http://localhost:7700"

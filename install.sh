#!/bin/sh
# Install db-agent (Linux / macOS) without Node or any runtime.
#   curl -fsSL https://raw.githubusercontent.com/frahjokhio/db-agent/main/install.sh | sh
# Options (env vars): DB_AGENT_VERSION=v0.1.0  DB_AGENT_INSTALL_DIR=$HOME/.local/bin
set -eu

REPO="frahjokhio/db-agent"
VERSION="${DB_AGENT_VERSION:-latest}"
INSTALL_DIR="${DB_AGENT_INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux) os=linux ;;
  darwin) os=darwin ;;
  *) echo "db-agent: unsupported OS: $os (use the Windows installer on Windows)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=x64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "db-agent: unsupported CPU: $arch" >&2; exit 1 ;;
esac

asset="db-agent-${os}-${arch}"
if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi

mkdir -p "$INSTALL_DIR"
tmp="$(mktemp)"
echo "Downloading $url"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$url" -o "$tmp"
else
  wget -qO "$tmp" "$url"
fi
chmod +x "$tmp"
mv "$tmp" "$INSTALL_DIR/db-agent"

echo "OK: installed $INSTALL_DIR/db-agent ($("$INSTALL_DIR/db-agent" version))"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "Add to PATH:  export PATH=\"$INSTALL_DIR:\$PATH\"  (put it in ~/.bashrc or ~/.zshrc)" ;;
esac
echo "Next: cd your-project && db-agent init"

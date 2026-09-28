#!/bin/sh
set -eu

GO_VERSION="${PV_MAN_GO_VERSION:-1.26.1}"
GO_ROOT="${HOME}/.local/go"
GO_BIN="${HOME}/go/bin"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Linux) GO_OS="linux" ;;
    Darwin) GO_OS="darwin" ;;
    *)
        echo "Unsupported operating system: $OS" >&2
        echo "Use WSL or Git Bash on Windows." >&2
        exit 1
        ;;
esac

case "$ARCH" in
    x86_64|amd64) GO_ARCH="amd64" ;;
    aarch64|arm64) GO_ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $ARCH" >&2
        exit 1
        ;;
esac

ARCHIVE="go${GO_VERSION}.${GO_OS}-${GO_ARCH}.tar.gz"
URL="https://go.dev/dl/${ARCHIVE}"
ARCHIVE_PATH="${TMP_DIR}/${ARCHIVE}"

if command -v curl >/dev/null 2>&1; then
    curl -fL --connect-timeout 15 --max-time 300 "$URL" -o "$ARCHIVE_PATH"
elif command -v wget >/dev/null 2>&1; then
    wget --timeout=15 --tries=1 -O "$ARCHIVE_PATH" "$URL"
else
    echo "Please install curl or wget first." >&2
    exit 1
fi

mkdir -p "${HOME}/.local"
tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR"
rm -rf "$GO_ROOT"
mv "${TMP_DIR}/go" "$GO_ROOT"

case "${SHELL:-}" in
    */zsh) SHELL_RC="${HOME}/.zshrc" ;;
    */bash) SHELL_RC="${HOME}/.bashrc" ;;
    *) SHELL_RC="${HOME}/.profile" ;;
esac

PATH_LINE='export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"'
touch "$SHELL_RC"
if ! grep -Fqx "$PATH_LINE" "$SHELL_RC" 2>/dev/null; then
    printf '\n# pvman Go installation\n%s\n' "$PATH_LINE" >> "$SHELL_RC"
fi

export PATH="${GO_ROOT}/bin:${GO_BIN}:${PATH}"
"${GO_ROOT}/bin/go" version
"${GO_ROOT}/bin/go" install github.com/tkzzzzzz6/pvman@latest

echo "pvman installed successfully."
echo "Run: source ${SHELL_RC} && pvman"

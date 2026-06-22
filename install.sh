#!/usr/bin/env bash
# AuraNode CLI — Installer (Linux and macOS)
# Repository: https://github.com/koyere/auranode-cli
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/koyere/auranode-cli/main/install.sh | bash
#
# Variables:
#   INSTALL_DIR  binary destination (default: /usr/local/bin, with sudo if needed)
#   VERSION      version to install (default: latest)

set -euo pipefail

GITHUB_REPO="koyere/auranode-cli"
BINARY_NAME="auranode"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

RED='\033[0;31m'; GREEN='\033[0;32m'; NC='\033[0m'
info()  { echo -e "${GREEN}[auranode-cli]${NC} $*"; }
error() { echo -e "${RED}[auranode-cli] ERROR:${NC} $*" >&2; exit 1; }

for cmd in curl tar sha256sum; do
  command -v "$cmd" >/dev/null 2>&1 || error "Required command not found: $cmd"
done

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux|darwin) ;;
  *) error "OS not supported by this installer: $OS (on Windows download the release .zip)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) error "Unsupported architecture: $(uname -m)" ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
    | grep '"tag_name"' | sed -E 's/.*"(v[^"]+)".*/\1/')
  [ -z "$VERSION" ] && error "Could not determine the latest version."
fi
info "Installing ${BINARY_NAME} ${VERSION} (${OS}/${ARCH})..."

BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}"
TARBALL="auranode_${VERSION#v}_${OS}_${ARCH}.tar.gz"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

curl -fsSL "${BASE_URL}/${TARBALL}"    -o "${TMP}/${TARBALL}"    || error "Could not download ${TARBALL}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP}/checksums.txt" || error "Could not download checksums.txt"

EXPECTED=$(grep " ${TARBALL}\$" "${TMP}/checksums.txt" | awk '{print $1}')
ACTUAL=$(sha256sum "${TMP}/${TARBALL}" | awk '{print $1}')
[ -z "$EXPECTED" ] && error "Checksum for ${TARBALL} not found."
[ "$EXPECTED" != "$ACTUAL" ] && error "SHA256 verification FAILED."
info "✓ SHA256 verified"

tar -xzf "${TMP}/${TARBALL}" -C "${TMP}"
if [ -w "$INSTALL_DIR" ]; then
  install -m 755 "${TMP}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
else
  info "Permissions required to write to ${INSTALL_DIR} (sudo)..."
  sudo install -m 755 "${TMP}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
fi

info "✓ Installed at ${INSTALL_DIR}/${BINARY_NAME}"
info "Run: ${BINARY_NAME} auth login"

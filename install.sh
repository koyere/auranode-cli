#!/usr/bin/env bash
# AuraNode CLI — Instalador (Linux y macOS)
# Repositorio: https://github.com/koyere/auranode-cli
#
# Uso:
#   curl -fsSL https://raw.githubusercontent.com/koyere/auranode-cli/main/install.sh | bash
#
# Variables:
#   INSTALL_DIR  destino del binario (default: /usr/local/bin, con sudo si hace falta)
#   VERSION      versión a instalar (default: latest)

set -euo pipefail

GITHUB_REPO="koyere/auranode-cli"
BINARY_NAME="auranode"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

RED='\033[0;31m'; GREEN='\033[0;32m'; NC='\033[0m'
info()  { echo -e "${GREEN}[auranode-cli]${NC} $*"; }
error() { echo -e "${RED}[auranode-cli] ERROR:${NC} $*" >&2; exit 1; }

for cmd in curl tar sha256sum; do
  command -v "$cmd" >/dev/null 2>&1 || error "Comando requerido no encontrado: $cmd"
done

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux|darwin) ;;
  *) error "SO no soportado por este instalador: $OS (en Windows descarga el .zip del release)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) error "Arquitectura no soportada: $(uname -m)" ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
    | grep '"tag_name"' | sed -E 's/.*"(v[^"]+)".*/\1/')
  [ -z "$VERSION" ] && error "No se pudo determinar la última versión."
fi
info "Instalando ${BINARY_NAME} ${VERSION} (${OS}/${ARCH})..."

BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}"
TARBALL="auranode_${VERSION#v}_${OS}_${ARCH}.tar.gz"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

curl -fsSL "${BASE_URL}/${TARBALL}"    -o "${TMP}/${TARBALL}"    || error "No se pudo descargar ${TARBALL}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP}/checksums.txt" || error "No se pudo descargar checksums.txt"

EXPECTED=$(grep " ${TARBALL}\$" "${TMP}/checksums.txt" | awk '{print $1}')
ACTUAL=$(sha256sum "${TMP}/${TARBALL}" | awk '{print $1}')
[ -z "$EXPECTED" ] && error "Checksum de ${TARBALL} no encontrado."
[ "$EXPECTED" != "$ACTUAL" ] && error "Verificación SHA256 FALLIDA."
info "✓ SHA256 verificado"

tar -xzf "${TMP}/${TARBALL}" -C "${TMP}"
if [ -w "$INSTALL_DIR" ]; then
  install -m 755 "${TMP}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
else
  info "Se requieren permisos para escribir en ${INSTALL_DIR} (sudo)..."
  sudo install -m 755 "${TMP}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
fi

info "✓ Instalado en ${INSTALL_DIR}/${BINARY_NAME}"
info "Ejecuta: ${BINARY_NAME} auth login"

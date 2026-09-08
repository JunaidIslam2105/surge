#!/bin/sh
# Surge installer: detects OS/arch, downloads the matching release asset,
# verifies its checksum, and installs the binary (plus shell completion).
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/SurgeDM/Surge/main/scripts/install.sh | sh
#
# Env overrides:
#   SURGE_INSTALL_DIR   install location for the binary (default: ~/.local/bin, falls back to /usr/local/bin with sudo)
#   SURGE_VERSION        install a specific tag instead of latest, e.g. v0.12.1

set -eu

REPO="SurgeDM/Surge"

log() { printf '%s\n' "$*" >&2; }
die() { log "error: $*"; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

need curl
need tar
need sha256sum || need shasum

detect_os() {
  case "$(uname -s)" in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    *) die "unsupported OS: $(uname -s) (use the Windows installers, or download a release manually)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    i386|i686) echo 386 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"

VERSION="${SURGE_VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name":' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')"
  [ -n "$VERSION" ] || die "could not determine latest version"
fi
VERSION_NUM="${VERSION#v}"

ASSET="Surge_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

log "Downloading ${ASSET} (${VERSION})..."
curl -sSL -o "${WORKDIR}/${ASSET}" "${BASE_URL}/${ASSET}" \
  || die "failed to download ${BASE_URL}/${ASSET} (this OS/arch combination may not be published — see https://github.com/${REPO}/releases)"

log "Verifying checksum..."
curl -sSL -o "${WORKDIR}/checksums.txt" "${BASE_URL}/Surge_${VERSION_NUM}_checksums.txt"
(
  cd "$WORKDIR"
  EXPECTED="$(grep " ${ASSET}\$" checksums.txt | awk '{print $1}')"
  [ -n "$EXPECTED" ] || die "no checksum entry for ${ASSET}"
  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL="$(sha256sum "${ASSET}" | awk '{print $1}')"
  else
    ACTUAL="$(shasum -a 256 "${ASSET}" | awk '{print $1}')"
  fi
  [ "$EXPECTED" = "$ACTUAL" ] || die "checksum mismatch for ${ASSET} (expected ${EXPECTED}, got ${ACTUAL})"
)

tar -xzf "${WORKDIR}/${ASSET}" -C "$WORKDIR"

INSTALL_DIR="${SURGE_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$INSTALL_DIR"
install -m 755 "${WORKDIR}/surge" "${INSTALL_DIR}/surge"
log "Installed surge to ${INSTALL_DIR}/surge"

case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *) log "note: ${INSTALL_DIR} is not on your PATH. Add it, e.g.: export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac

# Best-effort shell completion install (non-fatal if it fails).
install_completion() {
  shell="$1"
  case "$shell" in
    zsh)
      dir="${ZDOTDIR:-$HOME}/.zsh/completions"
      mkdir -p "$dir" 2>/dev/null && "${INSTALL_DIR}/surge" completion zsh > "${dir}/_surge" 2>/dev/null \
        && log "Installed zsh completion to ${dir}/_surge (add \"fpath=(${dir} \$fpath)\" before compinit in your .zshrc if not already present)"
      ;;
    bash)
      dir="$HOME/.local/share/bash-completion/completions"
      mkdir -p "$dir" 2>/dev/null && "${INSTALL_DIR}/surge" completion bash > "${dir}/surge" 2>/dev/null \
        && log "Installed bash completion to ${dir}/surge"
      ;;
    fish)
      dir="$HOME/.config/fish/completions"
      mkdir -p "$dir" 2>/dev/null && "${INSTALL_DIR}/surge" completion fish > "${dir}/surge.fish" 2>/dev/null \
        && log "Installed fish completion to ${dir}/surge.fish"
      ;;
  esac
}

case "${SHELL:-}" in
  */zsh) install_completion zsh ;;
  */bash) install_completion bash ;;
  */fish) install_completion fish ;;
esac

log "Done. Run 'surge --version' to confirm."

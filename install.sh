#!/bin/bash
set -euo pipefail

# yap install script
# Usage: curl -fsSL https://raw.githubusercontent.com/Enriquefft/yap/main/install.sh | bash

REPO="Enriquefft/yap"
GITHUB_BASE="https://github.com"
API_BASE="https://api.github.com"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="yap"

# Detect OS and architecture
detect_os_arch() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)

  case $arch in
    x86_64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) echo "Unsupported architecture: $arch"; exit 1 ;;
  esac

  case $os in
    linux) ;;
    darwin)
      echo "macOS support is coming soon. Follow progress at https://github.com/Enriquefft/yap"
      exit 1
      ;;
    mingw*|msys*|cygwin*)
      echo "Windows support is on the roadmap. Follow progress at https://github.com/Enriquefft/yap"
      exit 1
      ;;
    *) echo "Unsupported OS: $os"; exit 1 ;;
  esac

  echo "${os}_${arch}"
}

# Get latest release tag
get_latest_tag() {
  curl -fsSL "${API_BASE}/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"([^"]+)".*/\1/'
}

# Download, verify checksum, and install binary
download_and_install() {
  local tag=$1
  local os_arch=$2

  # Strip leading 'v' for the archive name (GoReleaser uses bare version).
  local version="${tag#v}"
  local archive_name="${BINARY_NAME}_${version}_${os_arch}.tar.gz"
  local archive_url="${GITHUB_BASE}/${REPO}/releases/download/${tag}/${archive_name}"
  local checksums_url="${GITHUB_BASE}/${REPO}/releases/download/${tag}/SHA256SUMS"

  local tmp_dir
  tmp_dir=$(mktemp -d)
  trap 'rm -rf "$tmp_dir"' EXIT

  echo "Downloading yap ${tag} (${os_arch})..."
  curl -fsSL "$archive_url" -o "${tmp_dir}/${archive_name}"

  # Verify SHA256 checksum if sha256sum is available.
  if command -v sha256sum > /dev/null 2>&1; then
    echo "Verifying checksum..."
    curl -fsSL "$checksums_url" -o "${tmp_dir}/SHA256SUMS"
    (cd "$tmp_dir" && grep "${archive_name}" SHA256SUMS | sha256sum -c --quiet)
    echo "Checksum verified."
  else
    echo "WARNING: sha256sum not found, skipping checksum verification."
  fi

  # Extract the binary from the archive.
  tar -xzf "${tmp_dir}/${archive_name}" -C "$tmp_dir" "$BINARY_NAME"
  chmod +x "${tmp_dir}/${BINARY_NAME}"

  # Install to target directory.
  mkdir -p "$INSTALL_DIR"
  mv "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"

  echo "Installed yap to ${INSTALL_DIR}/${BINARY_NAME}"
}

# Check PATH and suggest update
check_path() {
  if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
    echo "WARNING: ${INSTALL_DIR} is not in your PATH"
    echo "Add the following to your shell config:"
    case $SHELL in
      */bash) echo "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc && source ~/.bashrc" ;;
      */zsh) echo "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.zshrc && source ~/.zshrc" ;;
      */fish) echo "  echo 'fish_add_path ${INSTALL_DIR}' >> ~/.config/fish/config.fish && source ~/.config/fish/config.fish" ;;
      *) echo "  export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
    esac
  fi
}

main() {
  local os_arch
  os_arch=$(detect_os_arch)
  echo "Detected: ${os_arch}"

  local tag
  tag=$(get_latest_tag)
  echo "Latest release: ${tag}"

  download_and_install "$tag" "$os_arch"
  check_path

  echo "Installation complete!"
  echo "Run: yap --help"
}

main "$@"

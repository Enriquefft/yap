#!/bin/bash
set -euo pipefail

# yap install script
# Usage: curl -fsSL https://raw.githubusercontent.com/hybridz/yap/main/install.sh | bash

REPO="hybridz/yap"
GITHUB_BASE="https://github.com"
API_BASE="https://api.github.com"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="yap"

# Detect OS and architecture
detect_os_arch() {
  local os=$(uname -s | tr '[:upper:]' '[:lower:]')
  local arch=$(uname -m)

  case $arch in
    x86_64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) echo "Unsupported architecture: $arch"; exit 1 ;;
  esac

  case $os in
    linux) ;;
    *) echo "Unsupported OS: $os"; exit 1 ;;
  esac

  echo "${os}_${arch}"
}

# Get latest release tag
get_latest_tag() {
  curl -fsSL "${API_BASE}/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/'
}

# Download binary
download_binary() {
  local tag=$1
  local binary_url="${GITHUB_BASE}/${REPO}/releases/download/${tag}/${BINARY_NAME}"
  local temp_file=$(mktemp)

  echo "Downloading yap ${tag}..."
  curl -fsSL "$binary_url" -o "$temp_file"

  chmod +x "$temp_file"
  mv "$temp_file" "${INSTALL_DIR}/${BINARY_NAME}"

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
  local os_arch=$(detect_os_arch)
  echo "Detected: ${os_arch}"

  # Create install directory if needed
  mkdir -p "$INSTALL_DIR"

  # Get latest release and download
  local tag=$(get_latest_tag)
  echo "Latest release: ${tag}"

  download_binary "$tag"
  check_path

  echo "Installation complete!"
  echo "Run: yap --help"
}

main "$@"

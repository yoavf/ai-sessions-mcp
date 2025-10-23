#!/usr/bin/env bash
set -euo pipefail

# AI Sessions MCP Installer
# https://github.com/yoavf/ai-sessions-mcp

REPO="yoavf/ai-sessions-mcp"
BINARY_NAME="aisessions"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
ORANGE='\033[38;2;255;140;0m'
NC='\033[0m' # No Color

# Helper function for colored messages
print_message() {
    local level=$1
    local message=$2
    local color=""

    case $level in
        info) color="${GREEN}" ;;
        warning) color="${YELLOW}" ;;
        error) color="${RED}" ;;
    esac

    echo -e "${color}${message}${NC}"
}

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
IS_WINDOWS=false
case "$OS" in
  darwin*)
    OS="darwin"
    ;;
  linux*)
    OS="linux"
    ;;
  mingw*|msys*|cygwin*)
    OS="windows"
    BINARY_NAME="aisessions.exe"
    IS_WINDOWS=true
    ;;
  *)
    print_message error "Unsupported operating system: $OS"
    exit 1
    ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  arm64|aarch64)
    ARCH="arm64"
    ;;
  *)
    print_message error "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Validate platform combination
FILENAME="ai-sessions-$OS-$ARCH.zip"
case "$FILENAME" in
    *"-linux-"*)
        [[ "$ARCH" == "amd64" || "$ARCH" == "arm64" ]] || {
            print_message error "Unsupported architecture for Linux: $ARCH"
            exit 1
        }
        ;;
    *"-darwin-"*)
        [[ "$ARCH" == "amd64" || "$ARCH" == "arm64" ]] || {
            print_message error "Unsupported architecture for macOS: $ARCH"
            exit 1
        }
        ;;
    *"-windows-"*)
        [[ "$ARCH" == "amd64" ]] || {
            print_message error "Unsupported architecture for Windows: $ARCH (only amd64 supported)"
            exit 1
        }
        ;;
    *)
        print_message error "Unsupported OS/Arch combination: $OS/$ARCH"
        exit 1
        ;;
esac

print_message info "Detected platform: ${ORANGE}$OS-$ARCH"

# Fetch latest version
LATEST_VERSION=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')

if [[ $? -ne 0 || -z "$LATEST_VERSION" ]]; then
    print_message error "Failed to fetch version information"
    exit 1
fi

DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/$FILENAME"

print_message info "Installing ${ORANGE}aisessions ${GREEN}version: ${YELLOW}$LATEST_VERSION"

# Create temporary directory
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

# Download binary with progress bar
ZIP_FILE="$TMP_DIR/$FILENAME"
print_message info "Downloading..."
if ! curl -# -L -o "$ZIP_FILE" "$DOWNLOAD_URL"; then
  print_message error "Failed to download binary"
  exit 1
fi

# Download and verify checksums
CHECKSUMS_URL="https://github.com/$REPO/releases/download/$LATEST_VERSION/checksums.txt"
CHECKSUMS_FILE="$TMP_DIR/checksums.txt"
print_message info "Downloading checksums..."
if ! curl -fsSL "$CHECKSUMS_URL" -o "$CHECKSUMS_FILE"; then
  print_message error "Failed to download checksums file"
  print_message error "This may indicate a compromised or incomplete release"
  exit 1
fi

print_message info "Verifying checksum..."
EXPECTED_CHECKSUM=$(grep "$FILENAME" "$CHECKSUMS_FILE" | awk '{print $1}')
if [ -z "$EXPECTED_CHECKSUM" ]; then
  print_message error "Checksum not found for $FILENAME in checksums.txt"
  exit 1
fi

ACTUAL_CHECKSUM=$(shasum -a 256 "$ZIP_FILE" | awk '{print $1}')

if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
  print_message error "Checksum verification failed!"
  print_message error "Expected: $EXPECTED_CHECKSUM"
  print_message error "Got:      $ACTUAL_CHECKSUM"
  print_message error "This may indicate a corrupted download or security issue"
  exit 1
fi

print_message info "✓ Checksum verified"

# Extract binary
if ! unzip -q "$ZIP_FILE" -d "$TMP_DIR"; then
  print_message error "Failed to extract binary"
  exit 1
fi

# The extracted binary is named ai-sessions-$OS-$ARCH (or .exe for Windows)
EXTRACTED_BINARY="$TMP_DIR/ai-sessions-$OS-$ARCH"
if [ "$IS_WINDOWS" = true ]; then
  EXTRACTED_BINARY="${EXTRACTED_BINARY}.exe"
fi

# Verify extraction
if [ ! -f "$EXTRACTED_BINARY" ]; then
  print_message error "Extracted binary not found at $EXTRACTED_BINARY"
  exit 1
fi

# Determine installation directory
if [ -n "${INSTALL_DIR:-}" ]; then
  # User specified custom directory
  INSTALL_PATH="$INSTALL_DIR"
else
  # Default to ~/.aisessions/bin
  INSTALL_PATH="$HOME/.aisessions/bin"
fi

mkdir -p "$INSTALL_PATH"

# Install binary (rename to aisessions or aisessions.exe)
if ! mv "$EXTRACTED_BINARY" "$INSTALL_PATH/$BINARY_NAME"; then
  print_message error "Failed to install binary to $INSTALL_PATH"
  exit 1
fi

# Make executable
chmod 755 "$INSTALL_PATH/$BINARY_NAME"

print_message info "✓ Successfully installed ${ORANGE}aisessions ${YELLOW}$LATEST_VERSION"
echo ""
echo "Installation location: $INSTALL_PATH/$BINARY_NAME"
echo ""

# Check if directory is in PATH and provide instructions
if [[ ":$PATH:" != *":$INSTALL_PATH:"* ]]; then
  print_message warning "Note: $INSTALL_PATH is not in your PATH"
  echo ""

  # Detect shell
  CURRENT_SHELL=$(basename "${SHELL:-bash}")
  case $CURRENT_SHELL in
    fish)
      CONFIG_FILE="$HOME/.config/fish/config.fish"
      PATH_COMMAND="fish_add_path $INSTALL_PATH"
      ;;
    zsh)
      # Check for existing zsh config files
      for file in "$HOME/.zshrc" "$HOME/.zshenv" "${XDG_CONFIG_HOME:-$HOME/.config}/zsh/.zshrc"; do
        if [[ -f "$file" ]]; then
          CONFIG_FILE="$file"
          break
        fi
      done
      CONFIG_FILE="${CONFIG_FILE:-$HOME/.zshrc}"
      PATH_COMMAND="export PATH=\"$INSTALL_PATH:\$PATH\""
      ;;
    bash)
      # Check for existing bash config files
      for file in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
        if [[ -f "$file" ]]; then
          CONFIG_FILE="$file"
          break
        fi
      done
      CONFIG_FILE="${CONFIG_FILE:-$HOME/.bashrc}"
      PATH_COMMAND="export PATH=\"$INSTALL_PATH:\$PATH\""
      ;;
    *)
      CONFIG_FILE="$HOME/.profile"
      PATH_COMMAND="export PATH=\"$INSTALL_PATH:\$PATH\""
      ;;
  esac

  if [ "$IS_WINDOWS" = true ]; then
    echo "Add to your PATH:"
    echo ""
    echo "Option 1 - Git Bash/WSL:"
    echo "  echo '$PATH_COMMAND' >> ~/.bash_profile"
    echo "  source ~/.bash_profile"
    echo ""
    echo "Option 2 - Windows Environment Variables:"
    echo "  1. Search 'Environment Variables' in Windows Settings"
    echo "  2. Edit your user PATH"
    echo "  3. Add: %USERPROFILE%\\.aisessions\\bin"
  else
    echo "Add to your $CURRENT_SHELL profile:"
    echo ""
    echo "  echo '$PATH_COMMAND' >> $CONFIG_FILE"
    echo "  source $CONFIG_FILE"
  fi
  echo ""
fi

echo "Get started:"
echo "  aisessions login"
echo "  aisessions upload"
echo ""
echo "For help: aisessions --help"

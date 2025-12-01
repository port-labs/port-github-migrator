#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
REPO="omby8888/port-github-migrator"
LATEST_RELEASE_URL="https://api.github.com/repos/$REPO/releases/latest"

# Detect OS and architecture
OS=$(uname -s)
ARCH=$(uname -m)

# Map to release names
case "$OS" in
  Darwin)
    OS_NAME="macos"
    case "$ARCH" in
      x86_64)
        ARCH_NAME="x64"
        ;;
      arm64)
        ARCH_NAME="arm64"
        ;;
      *)
        echo -e "${RED}❌ Unsupported architecture: $ARCH${NC}"
        exit 1
        ;;
    esac
    ;;
  Linux)
    OS_NAME="linux"
    ARCH_NAME="x64"
    ;;
  MINGW*|MSYS*|CYGWIN*)
    OS_NAME="win"
    ARCH_NAME="x64"
    ;;
  *)
    echo -e "${RED}❌ Unsupported OS: $OS${NC}"
    exit 1
    ;;
esac

if [[ "$OS_NAME" == "win" ]]; then
  BINARY_NAME="port-github-migrator-${OS_NAME}-${ARCH_NAME}.exe"
else
  BINARY_NAME="port-github-migrator-${OS_NAME}-${ARCH_NAME}"
fi

echo -e "${YELLOW}📥 Fetching latest release...${NC}"

# Get the latest release download URL
RELEASE_DATA=$(curl -s "$LATEST_RELEASE_URL")
DOWNLOAD_URL=$(echo "$RELEASE_DATA" | grep "browser_download_url.*${BINARY_NAME}\"" | cut -d '"' -f 4 | head -1)

if [ -z "$DOWNLOAD_URL" ]; then
  echo -e "${RED}❌ Could not find release for $BINARY_NAME${NC}"
  exit 1
fi

echo -e "${GREEN}✅ Found release${NC}"

# Download to temporary location
TEMP_FILE=$(mktemp)

echo -e "${YELLOW}⬇️  Downloading...${NC}"
curl -L -o "$TEMP_FILE" "$DOWNLOAD_URL"

# Make it executable
chmod +x "$TEMP_FILE"

# Test the binary
echo -e "${YELLOW}🧪 Testing binary...${NC}"
if ! "$TEMP_FILE" --version > /dev/null 2>&1; then
  echo -e "${RED}❌ Binary test failed${NC}"
  "$TEMP_FILE" --version 2>&1 | head -5  # Show error details
  rm "$TEMP_FILE"
  exit 1
fi

echo -e "${GREEN}✅ Binary verified${NC}"

# Install
INSTALL_PATH="/usr/local/bin/port-github-migrator"
echo -e "${YELLOW}📍 Installing to $INSTALL_PATH${NC}"

if sudo mv "$TEMP_FILE" "$INSTALL_PATH"; then
  sudo chmod +x "$INSTALL_PATH"
  echo -e "${GREEN}✅ Installation complete!${NC}"
  echo ""
  echo -e "${GREEN}port-github-migrator --version${NC}"
  port-github-migrator --version
else
  echo -e "${RED}❌ Failed to install. Try running with sudo or check permissions.${NC}"
  rm "$TEMP_FILE"
  exit 1
fi

echo ""
echo -e "${GREEN}🎉 port-github-migrator is ready to use!${NC}"
echo -e "${YELLOW}Run 'port-github-migrator --help' for usage information${NC}"


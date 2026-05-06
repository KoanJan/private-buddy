#!/bin/bash
# Build Linux distribution with workaround for macOS provenance xattr issue.
#
# On macOS, the com.apple.provenance extended attribute prevents app-builder
# from extracting the `electron` ELF binary from the cached zip. This script
# uses electron-builder's --dir flag to unpack first, patches the missing
# binary, then runs the final packaging step.
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

ELECTRON_ZIP="$HOME/Library/Caches/electron/electron-v35.7.5-linux-arm64.zip"
UNPACKED_DIR="dist-electron/linux/linux-arm64-unpacked"

# Step 1: Build Go server for Linux
echo "=== Building Go server for Linux ==="
cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o private-buddy-server ./cmd/
cd "$SCRIPT_DIR"

# Step 2: Build web and electron
echo "=== Building web and electron ==="
npm run build

# Step 3: Clean previous Linux build output
rm -rf "$UNPACKED_DIR"

# Step 4: Unpack only (no packaging) - will fail on rename, but creates the directory
echo "=== Unpacking electron (expected to fail on rename) ==="
npx electron-builder --linux --dir --config.directories.output=dist-electron/linux || true

# Step 5: Patch the missing electron binary
if [ ! -f "$UNPACKED_DIR/private-buddy" ] && [ -f "$ELECTRON_ZIP" ]; then
    echo "=== Patching missing electron binary ==="
    cd "$UNPACKED_DIR"
    unzip -o "$ELECTRON_ZIP" electron
    mv electron private-buddy
    cd "$SCRIPT_DIR"
    echo "=== Patched successfully ==="
else
    echo "=== private-buddy binary already exists or zip not found, skipping patch ==="
fi

# Step 6: Package the pre-built directory into AppImage/deb
echo "=== Packaging Linux distribution ==="
npx electron-builder --linux --prepackaged "$UNPACKED_DIR" --config.directories.output=dist-electron/linux

echo "=== Linux distribution build complete ==="

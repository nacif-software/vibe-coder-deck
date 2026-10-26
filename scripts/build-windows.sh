#!/usr/bin/env bash
#
# build-windows.sh — cross-compile the Windows app into dist/ from any OS.
# -H windowsgui hides the console window: the app lives in the tray. For the
# command-line verbs on Windows build without that flag.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
mkdir -p dist
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-X main.version=$VERSION -H windowsgui" -o dist/vibe-coder-deck.exe .
echo "built dist/vibe-coder-deck.exe ($VERSION)"

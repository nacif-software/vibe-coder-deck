#!/usr/bin/env bash
#
# build-linux.sh — build the Linux binary and its release zip into dist/.
# Runs on Linux (GTK4 + WebKitGTK through cgo): natively, in CI, or in the
# container of scripts/docker-linux.sh. The zip holds the binary, a .desktop
# entry, the icon, the udev rule and a README with the install steps.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
ARCH="${GOARCH:-$(go env GOARCH)}"
OUT=dist/linux
rm -rf "$OUT"
mkdir -p "$OUT"
echo "building $VERSION for linux/$ARCH"
CGO_ENABLED=1 GOOS=linux GOARCH="$ARCH" go build -trimpath -ldflags "-X main.version=$VERSION" -o "$OUT/vibe-coder-deck" .
cp scripts/linux/vibe-coder-deck.desktop scripts/linux/vibe-coder-deck.png scripts/linux/README.md "$OUT/"
cp internal/platform/udev/70-vibe-coder-deck.rules "$OUT/"
ZIP="vibe-coder-deck-$VERSION-linux-$ARCH.zip"
rm -f "dist/$ZIP"
(cd "$OUT" && zip -q "../$ZIP" vibe-coder-deck vibe-coder-deck.desktop vibe-coder-deck.png README.md 70-vibe-coder-deck.rules)
echo "built dist/$ZIP"

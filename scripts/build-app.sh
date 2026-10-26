#!/usr/bin/env bash
#
# build-app.sh — build "Vibe Coder Deck.app" for macOS into dist/.
#
# The bundle wraps the Go binary: LSUIElement keeps it out of the Dock (it
# lives in the menu bar), the icon comes from assets/icon-1024.png, and an
# signature keeps Gatekeeper and TCC (Accessibility, Input Monitoring) happy
# on this machine. Signed with the local certificate (see below) the grants
# survive rebuilds; an ad-hoc signature changes with the binary.
#
# Usage: scripts/build-app.sh [--install]      --install copies it to /Applications
set -euo pipefail

cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
# Architectures in the binary: this machine's by default (quick), both for a
# release (VCD_ARCHES="arm64 amd64" makes a universal binary with lipo).
ARCHES="${VCD_ARCHES:-$(uname -m)}"
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"
APP="dist/Vibe Coder Deck.app"
CONTENTS="$APP/Contents"

rm -rf "$APP"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"

echo "building $VERSION for $ARCHES"
BIN="$CONTENTS/MacOS/vibe-coder-deck"
slices=()
for arch in $ARCHES; do
  [ "$arch" = "x86_64" ] && arch=amd64
  CGO_ENABLED=1 GOARCH="$arch" go build -trimpath -ldflags "-X main.version=$VERSION" -o "dist/vibe-coder-deck-$arch" .
  slices+=("dist/vibe-coder-deck-$arch")
done
if [ "${#slices[@]}" -gt 1 ]; then
  lipo -create "${slices[@]}" -output "$BIN"
else
  cp "${slices[0]}" "$BIN"
fi
rm -f "${slices[@]}"

cat > "$CONTENTS/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>Vibe Coder Deck</string>
	<key>CFBundleDisplayName</key>
	<string>Vibe Coder Deck</string>
	<key>CFBundleIdentifier</key>
	<string>dev.lucasabreu.vibe-coder-deck</string>
	<key>CFBundleExecutable</key>
	<string>vibe-coder-deck</string>
	<key>CFBundleIconFile</key>
	<string>AppIcon</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>$VERSION</string>
	<key>CFBundleVersion</key>
	<string>$VERSION</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSAppleEventsUsageDescription</key>
	<string>Macros type keystrokes through System Events.</string>
</dict>
</plist>
PLIST

# Icon: every size iconutil wants, from the 1024px master.
ICONSET="dist/AppIcon.iconset"
rm -rf "$ICONSET"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" assets/icon-1024.png --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" assets/icon-1024.png --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$CONTENTS/Resources/AppIcon.icns"
rm -rf "$ICONSET"

# Sign with the local "Vibe Coder Deck Local Signing" certificate when the
# keychain has it: macOS ties Accessibility and Input Monitoring to the
# certificate, so they survive rebuilds. Without it, sign ad hoc (the grants
# then have to be given again after every build).
# VCD_SIGN_IDENTITY names another identity (a Developer ID, or the one a CI
# run imported); "-" signs ad hoc.
IDENTITY="${VCD_SIGN_IDENTITY:-$(security find-identity -p codesigning 2>/dev/null | awk '/"Vibe Coder Deck Local Signing"/ { print $2; exit }')}"
codesign --force --deep --sign "${IDENTITY:--}" "$APP"
echo "signed with ${IDENTITY:-ad hoc}"
echo "built $APP"

if [ "${1:-}" = "--install" ]; then
  rm -rf "/Applications/Vibe Coder Deck.app"
  cp -R "$APP" /Applications/
  echo "installed /Applications/Vibe Coder Deck.app"
fi

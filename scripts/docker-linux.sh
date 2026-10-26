#!/usr/bin/env bash
#
# docker-linux.sh — build or test the Linux port in a container, so no Linux
# machine is needed. The image (scripts/linux/Dockerfile) has Go, GTK4,
# WebKitGTK and an X11 test bench (Xvfb, openbox, xdotool).
#
#   scripts/docker-linux.sh build              → dist/vibe-coder-deck-<version>-linux-<arch>.zip
#   scripts/docker-linux.sh test [go test args] → go test ./... under Xvfb, with device
#                                                access (uinput, evdev) for the pad tests
#   scripts/docker-linux.sh shell              → a shell in the container
#
# The container is privileged and sees the Docker VM's /dev, which is what the
# uinput tests need; the Go caches live in named volumes. WebKit's bubblewrap
# sandbox cannot start inside a container, so the window test runs without it.
set -euo pipefail
cd "$(dirname "$0")/.."
IMAGE=vcd-linux-builder
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker build -t "$IMAGE" -f scripts/linux/Dockerfile scripts/linux
fi
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
tty=""
if [ -t 0 ]; then tty="-it"; fi
run() {
  # shellcheck disable=SC2086
  docker run --rm $tty --privileged -v /dev:/dev -v "$PWD":/src -w /src \
    -v vcd-go-cache:/root/.cache -v vcd-go-mod:/go/pkg/mod \
    -e VERSION="$VERSION" -e GSK_RENDERER=cairo -e WEBKIT_DISABLE_DMABUF_RENDERER=1 \
    -e WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 -e GTK_A11Y=none \
    "$IMAGE" "$@"
}
case "${1:-}" in
  build) run scripts/build-linux.sh ;;
  test) shift; run dbus-run-session -- xvfb-run -a sh -c 'openbox >/dev/null 2>&1 & exec go test ./... "$@"' sh "$@" ;;
  shell) run bash ;;
  *) sed -n '3,12p' "$0"; exit 64 ;;
esac

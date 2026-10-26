#!/usr/bin/env bash
#
# release.sh — tag a version and push the tag. GitHub Actions
# (.github/workflows/release.yml) then builds the macOS and Windows apps and
# publishes the GitHub Release with the version's section of CHANGELOG.md.
#
# Usage: scripts/release.sh 0.2.0 [remote]     (default remote: origin)
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:?version, like 0.2.0}"
V="${V#v}"
REMOTE="${2:-origin}"
[ -z "$(git status --porcelain)" ] || { echo "commit your changes first" >&2; exit 1; }
grep -q "^## \[$V\] - [0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}" CHANGELOG.md || {
  echo "CHANGELOG.md has no \"## [$V] - YYYY-MM-DD\" section: write the notes first" >&2
  exit 1
}
git tag -a "v$V" -m "Vibe Coder Deck v$V"
git push "$REMOTE" "v$V"
echo "tagged v$V and pushed it to $REMOTE: GitHub Actions is building the release"

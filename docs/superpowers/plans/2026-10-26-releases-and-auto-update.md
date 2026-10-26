# Releases and auto-update — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** tagged versions of Vibe Coder Deck are built for macOS and Windows by GitHub Actions and published as GitHub Releases; the app shows the patch notes, finds a newer release by itself, downloads it, verifies it, replaces itself and restarts.

**Architecture:** a `release.yml` workflow runs on a `vX.Y.Z` tag on a macOS runner, builds a universal `.app` (zip) and a Windows `.exe` (zip), a `checksums.txt`, and creates the Release with the tag's section of `CHANGELOG.md` as its body. A new Go package `internal/update` knows versions, the GitHub releases API, the changelog, asset choice, download + SHA-256, bundle/exe replacement and a small state machine (`Updater`). The web server exposes it as `/api/update*` and `/api/releases`; the page gets an update banner and an **Updates** drawer section with **What's new**; the tray menu gets **Check for updates…**.

**Tech stack:** Go 1.26 standard library only (net/http, archive/zip, crypto/sha256); GitHub Actions (`actions/checkout`, `actions/setup-go`, `gh release create`); vanilla JS page.

**Spec:** this document (the user skipped the design conversation; decisions below are the builder's, stated so they can be rejected).

## Decisions

- **Versions** are git tags `vX.Y.Z` on `nacif-software/vibe-coder-deck`. The binary's `main.version` is `git describe --tags --always --dirty` (CI: the tag). `v1.2.3-4-gabc` and `-dirty` are *dev builds*: updates are offered to them on request but never downloaded automatically; `dev` / a bare hash never compares.
- **Patch notes** live in `CHANGELOG.md` (Keep a Changelog: `## [X.Y.Z] - YYYY-MM-DD`). The release body is that section; the page shows GitHub's release bodies when reachable and the embedded changelog otherwise, so notes work offline and for a private repo.
- **Assets:** `Vibe-Coder-Deck-vX.Y.Z-macos.zip` (universal arm64+amd64 `.app`, `ditto -c -k --keepParent`), `vibe-coder-deck-vX.Y.Z-windows-amd64.zip` (the `.exe`), `checksums.txt` (`shasum -a 256`). The updater refuses an asset whose SHA-256 is not in `checksums.txt`.
- **Signing in CI:** with secrets `MACOS_CERT_P12` + `MACOS_CERT_PASSWORD` the bundle is signed with that identity (a self-signed "Vibe Coder Deck Local Signing" cert keeps TCC grants across updates; a Developer ID would too); without them, ad hoc, and macOS asks for Accessibility / Input Monitoring again after an update. Not notarized.
- **Install:** macOS: unzip next to the bundle, `rename(app, app.old)`, `rename(new, app)`, remove `.old`, then the existing relaunch. Windows: `rename(exe, exe.old)`, move the new exe in, relaunch; `.old` is deleted at the next start. Only the installed app (`.app` bundle or `.exe`) updates itself; `go run` / a bare binary reports `unsupported`.
- **Policy:** check at start (after 20 s) and every 6 h; `auto_update` (meta, default on) downloads a newer release in the background and the page/tray offer **Restart to update**; nothing is installed without a click. **Check for updates** works whatever the policy. First start of a new version opens **What's new** once (`meta last_seen_version`).
- **Private repo:** the GitHub API needs a token for a private repo: `VCD_GITHUB_TOKEN` (env) or `vibe-coder-deck update --token TOKEN` (meta `github_token`, plain text in the local DB). The repo is meant to become public; then no token is needed.
- **Repo and API base** are variables (`update.Source{Repo, API}`), set in `main.go` to `nacif-software/vibe-coder-deck` / `https://api.github.com`, overridable with `-ldflags -X main.updateRepo=...`.

## Global constraints

- Go 1.26, standard library only; `gofmt`, `go vet ./...`, `go test ./...`, `CGO_ENABLED=0 GOOS=windows go build .` stay green.
- No native `alert/confirm/prompt` in the page; use `ask/askText/tell`.
- Tests never touch the network, the pad, hotkeys or `/Applications`.
- Commits to the company repo carry no co-author trailer.

## Review focus

1. A release whose `checksums.txt` is missing or does not list the asset: the updater must stop with a clear error, never install (updater test).
2. A zip with `__MACOSX/` resource forks or entries outside the bundle (`../`): skipped / refused (install test).
3. The old bundle cannot be renamed (permissions, another volume): the new bundle is cleaned up and the error shown; the running app stays intact (install test).
4. The API unreachable / rate limited / 404 on a private repo: state `error` with the message, the check retried at the next interval, patch notes fall back to the embedded changelog (github + web tests).
5. A dev build (`v0.1.0-3-gabc`, `dev`): no automatic download, manual install still possible (updater test).

## Tasks

1. `internal/update/version.go` — `Parse`, `Version.Less/String`, `Newer(current, latest)`, `IsRelease`.
2. `internal/update/changelog.go` — `ParseChangelog(md) []Release` (version, date, notes) and `Release` / `Asset` types.
3. `internal/update/github.go` — `Source{Repo, API, Token, Client}`, `Latest(ctx)`, `Releases(ctx, n)`, `Checksums(ctx, release)`; `ErrNoRelease`.
4. `internal/update/asset.go` — `PickAsset(assets, goos)`, `ChecksumFor(text, name)`.
5. `internal/update/install.go` — `Install(zipPath, target)` for `.app` and `.exe`, `TargetFromExecutable(exe)`, `extractZip` safety.
6. `internal/update/updater.go` — `Updater` state machine: `Status`, `Check`, `Download` (sha256 verified, progress), `Install`, `Run(ctx, delay, interval)`, `SetAuto`, `OnChange`.
7. `internal/deck/store.go` — meta helpers `AutoUpdate/SetAutoUpdate`, `LastSeenVersion/SetLastSeenVersion`, `GitHubToken/SetGitHubToken`.
8. `internal/web/updates.go` — `Updates` interface; `GET /api/update`, `POST /api/update/check|download|install|auto|seen`, `GET /api/releases`.
9. Page — banner, **Updates** drawer section (version, check, auto switch, What's new with a minimal markdown renderer), first-run What's new.
10. `internal/app/app.go` — tray item **Check for updates…** / **Update to vX…** / **Restart to update**.
11. `main.go` — embed `CHANGELOG.md`, build the updater, `update [--check|--install|--token T]` command.
12. `scripts/build-app.sh` (universal, `VCD_SIGN_IDENTITY`, `MACOSX_DEPLOYMENT_TARGET`), `scripts/build-windows.sh` (`VERSION` env), `scripts/release.sh`, `.github/workflows/release.yml`, `CHANGELOG.md`, README.
13. Tag `v0.1.0` on the company repo, watch the workflow, fix, verify assets and checksums; test `update --check` against the real release.

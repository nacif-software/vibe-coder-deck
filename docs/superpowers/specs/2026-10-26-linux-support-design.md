# Linux support — design

Date: 2026-10-26. Status: decided by the assistant after the user asked for Linux ("vai", "você
decide"); the user has no Linux machine, so everything is verified in Docker (Linux arm64 VM with
uinput + evdev in the kernel, no USB) and in GitHub Actions (Ubuntu). Hardware (a real pad on
hidraw) stays unverified until a Linux user confirms: the release is labelled a **preview**.

## Intent

Vibe Coder Deck on Linux with the same features as macOS wherever the kernel allows, working the
same on X11, Wayland and even a text console. The one feature that depends on the desktop —
"which app is in front" for profile switching — works on X11 first; Wayland compositors come later.

## Goals

- `vibe-coder-deck` builds and runs on Linux amd64 (arm64 later): CLI (`sync`, `set`, `keys`,
  `press`, `mcp`, `update`), the page/API server, and the desktop app (window + tray) if Wails v3
  works with GTK3/WebKit2GTK 4.1 — otherwise `app` serves the page and opens the browser.
- Pad keys reach the app without a display server: the app reads the pad's own input device
  (evdev) and grabs it, so dispatched hotkeys never reach the desktop, while everything else the pad
  types is forwarded unchanged through a virtual keyboard.
- Macros, keys-mode actions (with switching on), media keys and mouse clicks are typed through a
  uinput virtual keyboard/mouse: works under X11 and Wayland.
- The pad is written over hidraw (already implemented, untested on hardware).
- Device access is set up once with a udev rule the app can install for the user (pkexec) and
  explains in the Permissions section when it is missing.
- Releases carry `vibe-coder-deck-vX.Y.Z-linux-amd64.zip` (binary, .desktop, icon, udev rule,
  README) and the in-app updater replaces the binary in place.

## Non-goals (this round)

- Wayland "app in front" (GNOME extension, KDE DBus, wlroots foreign-toplevel): later, per compositor.
- Native file chooser (Browse… for an app): the installed-apps list from .desktop files is enough.
- Typing non-US characters with `text` steps: uinput sends key codes, the user's layout decides
  the character; v1 supports the US-layout ASCII set and refuses the rest with a clear error.
- Packages (.deb, AppImage, Flatpak), arm64 builds, Linux code signing.

## Anti-goals

- Requiring root at run time. Access comes from a udev rule (`uaccess` tag → the logged-in user).
- Swallowing keystrokes the pad types in keys mode without switching (the grab must forward them).
- Leaving a modifier stuck down on the virtual keyboard when a macro stops half way.
- Letting the watcher grab the app's own virtual keyboard (it must not look like the pad).

## Constraints

- Go 1.26, no cgo in `internal/platform` on Linux (cgo only where Wails needs it); pure-Go ioctls via
  `golang.org/x/sys/unix`. X11 via `github.com/jezek/xgb` (pure Go), no `xprop`/`xdotool` at run time.
- `golang.design/x/hotkey` is not used on Linux (X11-only, cgo): `hotkeys.go` gets
  `//go:build darwin || windows`; Linux provides its own `Listen`, `RunMain`, `HotkeyModifiers`.
- The pad's HID key names stay the single vocabulary (`internal/pad` tables); Linux key codes map
  to those names in one table.
- Tests that need `/dev/uinput` skip themselves when it is not writable; Docker runs them with
  `--privileged`, CI with `sudo -E`.
- Company repo commits carry no co-author lines (see HANDOFF.md); personal repo commits do.

## How it works (decided)

**Pad keys (`internal/platform/padwatch_linux.go`, `evdev_linux.go`).** Every 2 s (and at start)
the watcher lists `/sys/class/input/event*` whose `device/id/{bustype,vendor,product}` is USB
0x1189:0x8890 and opens the ones with keyboard capability (EVIOCGBIT has KEY_1). Each is grabbed
(EVIOCGRAB 1) and read in a goroutine (struct input_event, 24 bytes on 64-bit). Key codes become
pad key names (`keycodes_linux.go`) and feed `pad.Tracker.FeedKey` → the combo string, recorded in
`PadPresses` exactly like macOS (same 30 ms dedupe across interfaces). A combo equal to
`HotkeyModifiers+"+"+digit` is a dispatched slot: `Listen`'s callback gets `(digit, down)` on the
digit's down and up; those events and their modifiers are swallowed. Everything else is forwarded
to the virtual keyboard. Modifier downs are buffered until the next non-modifier event so a hotkey
never leaks a bare Super tap to the desktop (GNOME opens the overview on it): hotkey → drop the
buffer; other key → flush the buffer first; modifier up with its down still buffered → flush
down+up. The forwarder is a pure state machine (`forward_linux.go`, tested without devices).
`PadStatus` reports product/manufacturer from sysfs (`device/name`, `../manufacturer`).

**Typing (`uinput_linux.go`).** One virtual device "Vibe Coder Deck" (bus BUS_VIRTUAL, vendor
0x0001, product 0x0001) with EV_KEY for every known key, BTN_LEFT/RIGHT/MIDDLE, EV_REL X/Y/WHEEL
(so compositors accept it as a mouse too). Created once, at the first use or when the watcher
starts forwarding. `PostKey(name, down)`, `PostClick(button)`, `SendKeys(action)` (combos, media,
clicks; 10 ms between combos like macOS) and `PlayMacro` mirror the macOS Runner; `text` steps go
through a US-layout rune → (shift, key) table.

**Pad write.** Unchanged (`internal/pad/usb_linux.go`). The udev rule file
`scripts/linux/70-vibe-coder-deck.rules` (also embedded) grants the seat user access to the pad's
input and hidraw nodes and to `/dev/uinput`:
```
SUBSYSTEM=="input", ATTRS{idVendor}=="1189", ATTRS{idProduct}=="8890", TAG+="uaccess"
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="1189", ATTRS{idProduct}=="8890", TAG+="uaccess"
KERNEL=="uinput", SUBSYSTEM=="misc", TAG+="uaccess", OPTIONS+="static_node=uinput"
```
`vibe-coder-deck udev` prints it; `vibe-coder-deck udev --install` writes it to
`/etc/udev/rules.d/` through `pkexec` (or `sudo` without a display) and runs `udevadm control
--reload` + `udevadm trigger`.

**Permissions (`permissions_linux.go`).** Three entries: `uinput` (type macros), `pad-input`
(read the pad's keys), `pad-hidraw` (write the pad), granted = the node opens (pad entries are
granted while the pad is absent, so nothing nags). `PermAccessibility` = `"uinput"`,
`PermInputMonitoring` = `"pad-input"` keep main.go's logic meaningful. `RequestPermission` of any
name installs the udev rule (as above) and returns the manual steps when pkexec/sudo fail.
A fourth entry `recording` (read all keyboards, for the recorder) is granted when at least one
keyboard event node is readable; its request explains `sudo usermod -aG input $USER` + re-login.

**App in front (`frontmost_linux.go`).** X11 only: `_NET_ACTIVE_WINDOW` → `_NET_WM_PID` + `WM_CLASS`
through one persistent xgb connection; `path` = `/proc/<pid>/exe`, identity resolved against the
.desktop index (StartupWMClass, then Exec basename, then WM_CLASS class lower-cased).
`FrontmostApp` returns empty values when `DISPLAY` is unset or the session is Wayland
(`XDG_SESSION_TYPE=wayland`, logged once): the Desktop profile stays active.

**Installed apps (`apps_linux.go`).** .desktop files from `$XDG_DATA_HOME/applications` and every
`$XDG_DATA_DIRS/applications` (Type=Application, not NoDisplay/Hidden): `InstalledApp{Name,
BundleID: desktop id ("org.mozilla.firefox"), Path: the .desktop file}`. `AppIconPNG` resolves
`Icon=`: absolute PNG, else `hicolor` and the other themes under the XDG icon dirs at the closest
size ≥ requested, else `/usr/share/pixmaps`; SVG-only icons return an error (the page shows its
placeholder). `ChooseApp` returns "not supported on Linux" like Windows.

**Login item, notifications, dock.** `~/.config/autostart/vibe-coder-deck.desktop`
(`Exec=<exe> app`, `VCK_LOGIN_START=1` through `env`); `notify-send` when present; dock no-op.

**Recorder (`recorder_linux.go`).** Reads every keyboard (and mouse when asked) event node
without grabbing; emits keydown/keyup with pad key names and click left/right/middle. Needs the
`recording` permission.

**Updater.** `PickAsset` picks `-linux-<goarch>` zips; the target on Linux is the running binary
unless it lives in a `go-build` directory; `findPayload` accepts a bare `vibe-coder-deck` file;
`swap` keeps 0755. Package: `vibe-coder-deck-vX-linux-amd64.zip` with `vibe-coder-deck`,
`vibe-coder-deck.desktop`, `vibe-coder-deck.png` (256 px from assets/icon-1024.png),
`70-vibe-coder-deck.rules`, `README.md` (install steps).

**Desktop app.** `internal/app` with Wails v3 on GTK if the spike builds and the tray works
(StatusNotifier over DBus); otherwise `app` on Linux = `serve` + `xdg-open`. Decided by the spike
result recorded in the plan.

**Build and CI.** `scripts/linux/Dockerfile` (golang:1.26-bookworm + libgtk-3-dev,
libwebkit2gtk-4.1-dev, xvfb, openbox, xdotool) and `scripts/build-linux.sh` (native: build, icon,
zip). `scripts/docker-linux.sh build|test` runs them in the container (`--privileged` for device
tests). `release.yml` gains a `linux` job on `ubuntu-24.04` whose zip is downloaded by the macOS
job before Package/Publish; `test.yml` gains the same job without packaging.

## Verification without hardware

- Pure logic: forwarder state machine, key tables, .desktop/icon parsing, udev text, updater.
- Docker `--privileged`: a virtual pad made with uinput (bus USB, 0x1189:0x8890) → the watcher
  finds, grabs and reads it; `ctrl+opt+cmd+1` reaches `Listen` as digit 1 down/up and is recorded
  as a press; `cmd+shift+4` is forwarded and observed on the virtual keyboard's own node;
  `SendKeys("ctrl+c")` is observed there too. X11 frontmost under Xvfb + openbox + xdotool.
- CI runs the same with `sudo -E`.

## Left to the builder

File layout inside `internal/platform` beyond the names above; exact sysfs/ioctl helpers; the
Dockerfile details; README wording.

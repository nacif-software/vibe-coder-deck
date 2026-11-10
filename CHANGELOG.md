# Changelog

All notable changes to Vibe Coder Deck. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); every version is a git tag `vX.Y.Z` and a
GitHub Release, and the section below the version heading is that release's notes, shown in the
app under **What's new**.

## [0.1.8] - 2026-11-10

### Fixed
- Updates failed with "GitHub 403: API rate limit exceeded". Without a token GitHub's API answers
  only 60 requests an hour per IP address, which a shared network uses up. The app no longer uses
  the API for the public repository: it reads the newest version from github.com's release page,
  the files from the release's checksums.txt and the notes from that version's CHANGELOG.md. A
  token (`vibe-coder-deck update --token`) still goes through the API.

## [0.1.7] - 2026-11-10

### Fixed
- Linux: the app could not write the pad ("pad not connected" from Apply, the CLI and MCP with the
  pad plugged in). The pad's config interface has only an output endpoint, so the kernel's HID
  driver makes no hidraw node for it. The app now writes it through usbfs (`/dev/bus/usb`), as
  libusb does, and uses hidraw only where a node exists. The udev rule now also covers the pad's
  USB device: run `vibe-coder-deck udev --install` again (or **Allow** next to "Write the pad")
  and replug the pad.

## [0.1.6] - 2026-11-10

### Fixed
- Recording a macro on macOS: holding a key recorded the keyboard's auto-repeat as a burst of key
  presses. Now a held key is one key down, the wait and one key up, so holding only makes the
  delay longer (Linux already did this).
- The pad's labels and lines disappeared when the window was narrower than about 1310 px. The
  layout is fixed rather than responsive: the window can no longer be made smaller than 1360 × 760,
  where every label fits.

## [0.1.5] - 2026-10-28

### Fixed
- A combo with the comma key ("opt+,", "cmd+,") was read as two empty steps of a sequence and
  refused. A comma that follows "+" (or starts a step) is now the comma key, in the pad, in "Run
  now", in macros and with profile switching; "opt+comma" works too.

## [0.1.4] - 2026-10-27

### Changed
- Settings left the side drawer: the gear in the header opens a Settings view laid out like
  Logitech G HUB's, with General (launch at login, appearance, macOS permissions with their state,
  restart), Updates (version, check for update, automatic updates, release notes), Logs, AI setup
  and About. Settings switches are drawn as toggles.
- What's new is a card like G HUB's: "What's New in Version …" with that version's notes, **See
  release notes** and **Got it**. It opens once on the first start of a new version, and from the
  update banner, Updates and About.

### Added
- **Launch app on login** in Settings › General (it was only in the menu bar menu).

## [0.1.3] - 2026-10-27

### Fixed
- A keys-mode slot that is only modifiers ("ropt", "ctrl+shift") now holds them while the pad's key is
  held when profile switching is on (the pad sends a hotkey and the app presses the modifiers on its
  behalf); before, the app tapped the modifier once on the way down, which no app could use.
  Works on macOS, Linux and Windows; "Run now" still taps it once.

### Changed
- Releases are signed with the project's own certificate when the repository carries it
  (`MACOS_CERT_P12`/`MACOS_CERT_PASSWORD`), so macOS keeps the Accessibility and Input Monitoring
  grants across updates; the first signed build asks for them once more. The self-signed certificate
  is "not trusted" to macOS, so the workflow no longer filters identities with `-v`.

## [0.1.2] - 2026-10-27

### Added
- Linux (preview): the app, the page, scripts, shell commands, macros and the recorder run on
  Linux. The pad's keys are read from its own input device (evdev, grabbed and forwarded) and
  macros are typed through a uinput virtual keyboard, so X11 and Wayland behave the same; "app in
  front" (profile switching) works on X11. `vibe-coder-deck udev --install` sets up device access.
  Releases carry `vibe-coder-deck-vX.Y.Z-linux-amd64.zip` and the app updates itself in place.

### Fixed
- Windows: writing the pad failed with "The parameter is incorrect" (error 87) on every packet.
  The HID class driver only takes a buffer of exactly the collection's output report length that
  starts with a report ID the descriptor declares; the app now reads that length from the driver,
  tries every collection of the config interface and both report forms (the packet as report 3,
  or behind report ID 0), and names the collection and report length when a write still fails.

## [0.1.1] - 2026-10-26

### Changed
- The app writes the pad itself over USB (IOKit on macOS, hidraw on Linux, HID on Windows): the
  `macropad` CLI of vibe-coder-keyboard is no longer required. `VCK_MACROPAD` still names it to use
  it instead.

### Fixed
- Closing the menu no longer flashes scrollbars behind it.
- What's new shows the newest release and unfolds the rest with **View more**, instead of scrolling
  inside the menu.

## [0.1.0] - 2026-10-26

The first release.

### Added
- The pad drawn as it looks, with Assignments for each of the six keys and the knob.
- Modes: keys (the pad types it), script, shell and macro, with cooldowns and a single-instance guard.
- A macro library with a recorder, a block editor with drag and drop, standard delays and four macro types.
- Profiles per application, switched automatically by the app in front.
- A native app with a menu bar icon, start at login, logs and a debug mode.
- An MCP server over HTTP and stdio with an MCP App view of the pad, and one-click install into Claude Code, Claude Desktop, Cursor, Windsurf and Codex.
- Releases for macOS (universal) and Windows, patch notes in the app, and automatic updates.

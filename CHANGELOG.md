# Changelog

All notable changes to Vibe Coder Deck. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); every version is a git tag `vX.Y.Z` and a
GitHub Release, and the section below the version heading is that release's notes, shown in the
app under **What's new**.

## [Unreleased]

### Fixed
- A keys-mode slot that is only modifiers ("ropt", "ctrl+shift") now holds them while the pad's key is
  held when profile switching is on (the pad sends a hotkey and the app presses the modifiers on its
  behalf); before, the app tapped the modifier once on the way down, which no app could use.
  Works on macOS, Linux and Windows; "Run now" still taps it once.

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

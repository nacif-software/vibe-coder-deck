# Native pad writer — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the app writes the macropad itself, over USB, so installing the `macropad` CLI (Python, uv, libusb) is no longer a requirement.

**Architecture:** `internal/pad` gains a port of the CLI's action syntax and packet protocol (`action.go`, `protocol.go`), verified byte for byte against the Python tool's dry run (`testdata/packets.json`), and a `USB` Writer whose transport is per system: IOKit `IOUSBInterfaceInterface` on macOS (`usb_darwin.go`), the config interface's hidraw node on Linux (`usb_linux.go`), the HID device of interface 1 on Windows (`usb_windows.go`). `main.go` uses it unless `VCK_MACROPAD` names the CLI.

**Spec:** docs/protocol.md of vibe-coder-keyboard: 64-byte packets (`0x03` + payload) on USB interface 1, endpoint `0x02` (interrupt OUT); bind = select layer, body, commit; clear = one empty keystroke; the pad keeps the last write to a slot whatever the layer.

## Decisions

- macOS: match `IOUSBHostInterface` (then `IOUSBInterface`) by class and filter by the `idVendor` / `idProduct` properties (keys in the matching dictionary match nothing for interfaces); try every interface of the pad, open it (`USBInterfaceOpen`, then `USBInterfaceOpenSeize` as libusb does), use the first with an interrupt OUT pipe; `WritePipe` (the `…TO` variant is for bulk pipes only); the Go side waits at most 5 s.
- Linux: `/sys/class/hidraw/*/device/uevent` with `HID_ID=0003:00001189:00008890` and `HID_PHYS=…/input1` → `/dev/hidrawN`; a permission error tells the udev rule. Only `internal/pad` builds on Linux today; the app does not.
- Windows: `CM_Get_Device_Interface_ListW` (HID GUID) → path with `vid_1189&pid_8890&mi_01` → `WriteFile`, report ID 0 in front first, the raw packet second. Untested on hardware.
- The CLI (`pad.CLI`) stays as an opt-in fallback: `VCK_MACROPAD=/path/to/macropad` or `=macropad`.

## Tasks

1. [x] Fixtures from the Python tool (`macropad set … --dry-run --json`), tests for `ParseAction` / `BindPackets` / `LEDPackets`.
2. [x] `action.go` (tables inverted from hid.go), `protocol.go`.
3. [x] `usb.go` + transports; `padWriter()` in main.go.
4. [x] Live check on macOS: `vibe-coder-deck sync` writes the nine hotkeys over IOKit.
5. [ ] User: press a pad key after the native write (the pad has no read command).
6. [x] README, CHANGELOG.

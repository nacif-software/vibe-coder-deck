# Linux Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Vibe Coder Deck builds, runs and self-updates on Linux amd64, reading the pad's keys and typing macros below the display server so X11 and Wayland behave the same.

**Architecture:** A new Linux half of `internal/platform`: the pad's evdev nodes are found in sysfs, grabbed and read (hotkeys dispatched, everything else forwarded through a uinput virtual keyboard); the same uinput device types macros; X11 (pure-Go xgb) answers "app in front"; .desktop files are the app index. Updater, packaging, Docker builder and CI gain a Linux target.

**Tech Stack:** Go 1.26, `golang.org/x/sys/unix` (ioctl, InputEvent), `github.com/jezek/xgb` (X11), `golang.design/x/mainthread`, Wails v3 on GTK3/WebKit2GTK 4.1 (if Task 11's spike holds), Docker (`scripts/linux/Dockerfile`), GitHub Actions `ubuntu-24.04`.

**Spec:** `docs/superpowers/specs/2026-10-26-linux-support-design.md`

## Global Constraints

- No cgo in `internal/platform` on Linux; `golang.design/x/hotkey` is not imported on Linux (`hotkeys.go` gets `//go:build darwin || windows`).
- Key names are the pad's vocabulary (`internal/pad/hid.go` tables) plus macOS's spellings (`[`, `]`, `'`, `\`, `` ` ``, `delete` = backspace, `forward-delete`); Linux must accept both and emit the macOS spelling in `KeyName`.
- `HotkeyModifiers = "ctrl+opt+cmd"` on Linux (the pad's cmd is the HID GUI key = Super).
- The virtual keyboard is bus `BUS_VIRTUAL` (0x06), vendor 0x0001, product 0x0001, name `Vibe Coder Deck`; the watcher matches only bus USB (0x03) 0x1189:0x8890.
- Device tests skip (`t.Skip`) unless `/dev/uinput` opens for writing; run them in Docker with `--privileged` (`scripts/docker-linux.sh test`) or in CI with `sudo -E`.
- Every Go change: tests first, `gofmt`, `go vet ./...`, `go test ./...` on macOS, `scripts/docker-linux.sh test` on Linux, `CGO_ENABLED=0 GOOS=windows go build .` before committing.
- Commits on this branch end with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`; the company mirror drops that line (HANDOFF.md recipe).

## Review Focus

1. A keys-mode slot without profile switching (`cmd+shift+4` typed by the pad) must reach the desktop unchanged through the grab + forward path — Task 5 test `TestForwarderPassesTypedCombos`.
2. The pad unplugged and replugged while the app runs: the watcher must drop the dead node and grab the new one without a restart — Task 6 test `TestWatcherFollowsReplug`.
3. A macro cancelled mid-way (toggle stopped) must release every modifier it pressed on the virtual keyboard — Task 3 test `TestPlayMacroReleasesHeldModifiersWhenCancelled`.
4. Starting without the udev rule: `Listen` fails with a message that names `vibe-coder-deck udev --install`, the Permissions list shows `uinput` and `pad-input` as not granted, and nothing panics — Task 7 test `TestPermissionsWithoutAccess`.
5. The updater on a `go run` binary or a dev build must refuse to replace itself — Task 10 test `TestTargetForLinuxSkipsGoBuildDirs`.

---

### Task 1: `pad.Tracker.FeedKey` — combos from key names

**Files:**
- Modify: `internal/pad/hid.go:71-110`
- Test: `internal/pad/hid_test.go`

**Interfaces:**
- Produces: `func (t *Tracker) FeedKey(name string, down bool) (combo string, ok bool)` — `name` is a pad key name (`ctrl`, `opt`, `cmd`, `shift`, `rctrl`…, `a`, `1`, `enter`, `kp-1`, `f13`, media `play`/`volume-up`…, buttons `click`/`right-click`/`middle-click`). Modifiers update the held set and return `("", false)`; other names return the combo (`modifierOrder` prefix + name) on down only.
- `Feed(in HIDInput)` keeps its signature and becomes: resolve `in` to a name with the existing tables, then `FeedKey`.

- [ ] **Step 1: Write the failing test** in `hid_test.go`:
```go
func TestFeedKeyBuildsCombosFromNames(t *testing.T) {
	var tr Tracker
	if c, ok := tr.FeedKey("ctrl", true); ok || c != "" { t.Fatal("modifier down must not report") }
	tr.FeedKey("opt", true); tr.FeedKey("cmd", true)
	if c, ok := tr.FeedKey("1", true); !ok || c != "ctrl+opt+cmd+1" { t.Fatalf("got %q %v", c, ok) }
	if _, ok := tr.FeedKey("1", false); ok { t.Fatal("key up must not report") }
	tr.FeedKey("ctrl", false); tr.FeedKey("opt", false); tr.FeedKey("cmd", false)
	tr.FeedKey("shift", true); tr.FeedKey("cmd", true)
	if c, _ := tr.FeedKey("4", true); c != "shift+cmd+4" { t.Fatalf("got %q", c) }
	var m Tracker
	if c, ok := m.FeedKey("volume-up", true); !ok || c != "volume-up" { t.Fatalf("got %q %v", c, ok) }
	if c, ok := m.FeedKey("right-click", true); !ok || c != "right-click" { t.Fatalf("got %q %v", c, ok) }
	if _, ok := m.FeedKey("no-such-key", true); ok { t.Fatal("unknown names are ignored") }
}
```
- [ ] **Step 2: Run** `go test ./internal/pad/ -run TestFeedKey` — FAIL: `tr.FeedKey undefined`.
- [ ] **Step 3: Implement** `FeedKey` in `hid.go` (modifier names = values of `modifierUsages`; key names = values of `keyUsages`, `consumerUsages`, `buttonUsages`; build the two reverse sets in `init`). Rewrite `Feed` to map `(Page, Usage)` → name and call `FeedKey`.
- [ ] **Step 4: Run** `go test ./internal/pad/` — PASS (existing `Feed` tests included).
- [ ] **Step 5: Commit** `feat(pad): Tracker.FeedKey builds combos from key names`.

### Task 2: Linux key tables

**Files:**
- Create: `internal/platform/keycodes_linux.go`, `internal/platform/keycodes_linux_test.go`
- Modify: `internal/platform/hotkeys.go:1` (add `//go:build darwin || windows`)

**Interfaces:**
- Produces: `func KeyCode(name string) (uint16, error)` (pad and macOS spellings, aliases `alt/option/control/command/win/super/meta/return/escape/delete`, `code:N`); `func KeyName(code int) string` (macOS spelling, `code:N` fallback); `func IsModifier(name string) bool`; `func MediaKey(name string) (int, bool)` and `func MediaKeyName(code int) string` (media names → their KEY_ codes, `media:N` accepted); `func ButtonCode(button string) (uint16, error)` (`left`→0x110, `right`→0x111, `middle`→0x112); `var usLayout map[rune]keyChord` with `type keyChord struct{ Code uint16; Shift bool }` for printable ASCII + `\n` (enter) + `\t`.

- [ ] **Step 1: Write the failing test** `keycodes_linux_test.go` (build tag linux):
```go
func TestKeyCodesPinLinuxValues(t *testing.T) {
	for name, want := range map[string]uint16{"a": 30, "z": 44, "1": 2, "0": 11, "enter": 28, "esc": 1, "space": 57,
		"ctrl": 29, "shift": 42, "opt": 56, "cmd": 125, "rcmd": 126, "left": 105, "f12": 88, "f13": 183, "kp-enter": 96,
		"[": 26, "lbracket": 26, "delete": 14, "forward-delete": 111, "volume-up": 115, "play": 164, "alt": 56, "super": 125} {
		if got, err := KeyCode(name); err != nil || got != want { t.Errorf("%s: %d %v, want %d", name, got, err, want) }
	}
	if _, err := KeyCode("no-such"); err == nil { t.Error("unknown name must fail") }
	if c, _ := KeyCode("code:200"); c != 200 { t.Error("code:N") }
	for code, want := range map[int]string{30: "a", 26: "[", 14: "backspace", 125: "cmd", 115: "volume-up", 999: "code:999"} {
		if got := KeyName(code); got != want { t.Errorf("KeyName(%d)=%q want %q", code, got, want) }
	}
	if !IsModifier("shift") || IsModifier("a") { t.Error("IsModifier") }
	if ch := usLayout['A']; ch.Code != 30 || !ch.Shift { t.Error("A = shift+a") }
	if ch := usLayout['!']; ch.Code != 2 || !ch.Shift { t.Error("! = shift+1") }
	if ch := usLayout['\n']; ch.Code != 28 { t.Error("newline = enter") }
	if b, _ := ButtonCode("right"); b != 0x111 { t.Error("right button") }
}
```
- [ ] **Step 2: Run** in Docker: `scripts/docker-linux.sh test ./internal/platform/ -run TestKeyCodes` — FAIL (until Task 11's script exists, use `docker run --rm -v "$PWD":/src -w /src vcd-linux-builder go test ./internal/platform/ -run TestKeyCodes`). Expected: undefined symbols.
- [ ] **Step 3: Implement** the tables (values from `linux/input-event-codes.h`), the two maps and the functions; add the build tag to `hotkeys.go`.
- [ ] **Step 4: Run** the test — PASS. Also `go vet ./...` on macOS still passes.
- [ ] **Step 5: Commit** `feat(platform): Linux key code tables`.

### Task 3: uinput virtual keyboard and the Runner

**Files:**
- Create: `internal/platform/uinput_linux.go`, `internal/platform/platform_linux.go`, `internal/platform/uinput_linux_test.go`, `internal/platform/evdev_linux.go` (the reader half is used by the test and by Task 4)
- Modify: `internal/pad/usb_linux.go:40` (error text → `run \`vibe-coder-deck udev --install\``)

**Interfaces:**
- Produces (uinput): `func virtualKeyboard() (*vkbd, error)` (sync.Once, creates the device: EV_KEY for every code in `keyCodeNames` + BTN_LEFT/RIGHT/MIDDLE, EV_REL X/Y/WHEEL, `UI_DEV_SETUP` + `UI_DEV_CREATE`, then 150 ms settle); `func (v *vkbd) Emit(code uint16, down bool) error` (key or button event + `EV_SYN`); `func PostKey(name string, down bool) error`; `func PostClick(button string) error`; `func typeText(s string) error` (via `usLayout`; unknown rune → `fmt.Errorf("cannot type %q: only US-keyboard characters can be typed on Linux", r)`); `func AccessibilityAllowed() bool` = `/dev/uinput` opens O_WRONLY (also used by Task 7).
- Produces (evdev, reader half): `type inputReader struct{ f *os.File }`; `func openInput(path string) (*inputReader, error)` (O_RDONLY|O_NONBLOCK → blocking read loop via `unix.Read` with poll); `func (r *inputReader) Grab(on bool) error` (EVIOCGRAB); `func (r *inputReader) HasKey(code uint16) bool` (EVIOCGBIT EV_KEY); `func (r *inputReader) Name() string` (EVIOCGNAME); `func (r *inputReader) Read(fn func(ev unix.InputEvent)) error` (until Close); `func findInputByName(name string) (string, bool)` (scan `/sys/class/input/event*/device/name`).
- Produces (platform_linux.go): `const HotkeyModifiers = "ctrl+opt+cmd"`; `type Runner struct{}` with `RunScript` (executable → direct, else `bash` then `sh`), `RunShell` (`bash -c`, `sh -c` fallback), `PlayMacro` (same step semantics as `platform_darwin.go:117-205`: keydown/keyup track held modifiers and release them on return; `keys` combos → `SendKeys`; `text` → `typeText`; `launch` → `xdg-open`; `delay`), `SendKeys(action string) error` (combos, media names via `MediaKey`, `click`/`right-click`/`middle-click`, 10 ms between combos); `func Notify(title, message string)` (`notify-send -a "Vibe Coder Deck" title message`, ignored when missing); `func Frontmost() (string, error)` (name from Task 9's `FrontmostApp`, error when empty).

- [ ] **Step 1: Write the failing device test** `uinput_linux_test.go`:
```go
func requireUinput(t *testing.T) { f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0); if err != nil { t.Skip("no /dev/uinput:", err) }; f.Close() }

func TestVirtualKeyboardTypesAndIsObservable(t *testing.T) {
	requireUinput(t)
	v, err := virtualKeyboard(); if err != nil { t.Fatal(err) }
	path, ok := findInputByName("Vibe Coder Deck"); if !ok { t.Fatal("virtual keyboard not in /sys/class/input") }
	r, err := openInput(path); if err != nil { t.Fatal(err) }
	defer r.Close()
	got := make(chan unix.InputEvent, 16)
	go r.Read(func(ev unix.InputEvent) { if ev.Type == unix.EV_KEY { got <- ev } })
	if err := Runner{}.SendKeys("ctrl+c"); err != nil { t.Fatal(err) }
	want := []struct{ code uint16; value int32 }{{29, 1}, {46, 1}, {46, 0}, {29, 0}}
	for i, w := range want {
		select {
		case ev := <-got: if ev.Code != w.code || ev.Value != w.value { t.Fatalf("event %d: %+v want %+v", i, ev, w) }
		case <-time.After(2 * time.Second): t.Fatalf("event %d never arrived", i)
		}
	}
	if err := typeText("Hi!"); err != nil { t.Fatal(err) }   // shift+h, i, shift+1 → 42↓ 35↓ 35↑ 42↑ 23↓ 23↑ 42↓ 2↓ 2↑ 42↑
	if err := typeText("é"); err == nil { t.Fatal("non-US rune must be refused") }
	if err := PostClick("right"); err != nil { t.Fatal(err) }
	_ = v
}

func TestPlayMacroReleasesHeldModifiersWhenCancelled(t *testing.T) {
	requireUinput(t)
	// observe the virtual keyboard as above; steps: keydown shift, delay 5000; cancel ctx after 50 ms
	// expect: 42↓ then 42↑ within 1 s, exit code 0.
}
```
- [ ] **Step 2: Run** in Docker with `--privileged` — FAIL (undefined).
- [ ] **Step 3: Implement** `uinput_linux.go`, `evdev_linux.go` (reader half), `platform_linux.go`. ioctl numbers: `EVIOCGRAB = 0x40044590`, `EVIOCGNAME(n) = unix.IOC(unix.IOC_READ, 'E', 0x06, n)`, `EVIOCGBIT(EV_KEY, n) = unix.IOC(unix.IOC_READ, 'E', 0x20+unix.EV_KEY, n)`, `UI_SET_EVBIT = 0x40045564`, `UI_SET_KEYBIT = 0x40045565`, `UI_SET_RELBIT = 0x40045566`, `UI_DEV_SETUP = 0x405c5503` (struct: `Id unix.InputId; Name [80]byte; FFEffectsMax uint32`), `UI_DEV_CREATE = 0x5501`, `UI_DEV_DESTROY = 0x5502`. Prefer the `unix.UI_*`/`unix.EVIOC*` constants when `go doc` shows them.
- [ ] **Step 4: Run** the two tests in Docker `--privileged` — PASS; `go test ./internal/platform/` on macOS still PASS (files are linux-only).
- [ ] **Step 5: Commit** `feat(platform): Linux runner types through a uinput virtual keyboard`.

### Task 4: finding the pad's input nodes in sysfs

**Files:**
- Create: `internal/platform/padnodes_linux.go`, `internal/platform/padnodes_linux_test.go`

**Interfaces:**
- Produces: `type padNode struct{ Event, Name, Manufacturer, Product string; Vendor, ProductID, Bus int }`; `var sysInput = "/sys/class/input"` (tests point it at a temp tree); `func padNodes(vendor, product int) []padNode` — every `eventN` whose `device/id/bustype` is `0003` and `device/id/vendor`/`product` match (hex, 4 digits); `Name` from `device/name`; `Manufacturer`/`Product` from the first ancestor directory of `device/device` that has `idVendor` (files `manufacturer`, `product`), empty when absent.

- [ ] **Step 1: Write the failing test** with a fake tree: `event3` (bus 0003, 1189/8890, name "Macro Keyboard", usb ancestor with manufacturer "Makerlab", product "Macro Pad") → one node; `event4` same ids but bus `0006` → excluded; `event5` vendor `046d` → excluded. Assert `padNodes(0x1189, 0x8890)` returns exactly `[{Event: "/dev/input/event3", Name: "Macro Keyboard", Manufacturer: "Makerlab", Product: "Macro Pad", Vendor: 0x1189, ProductID: 0x8890, Bus: 3}]`.
- [ ] **Step 2: Run** `go test ./internal/platform/ -run TestPadNodes` (Docker, no privilege needed) — FAIL.
- [ ] **Step 3: Implement** `padNodes` (`os.ReadDir(sysInput)`, `filepath.EvalSymlinks` not required: read files relative to `sysInput/eventN/`).
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `feat(platform): find the pad's input nodes in sysfs`.

### Task 5: the grab-and-forward state machine

**Files:**
- Create: `internal/platform/forward_linux.go`, `internal/platform/forward_linux_test.go` (no build tag needed beyond linux; pure logic)

**Interfaces:**
- Produces: `type forwarder struct{ ... }`; `func newForwarder(modifiers string, hotkey func(digit int, down bool), emit func(code uint16, down bool), combo func(string)) *forwarder`; `func (f *forwarder) Event(code uint16, value int32)` — value 0 up, 1 down, 2 repeat (ignored). Uses `KeyName` + `pad.Tracker.FeedKey`. Rules: modifier downs are buffered; a reported combo equal to `modifiers+"+"+digit` (digit 1–9) → `hotkey(digit, true)`, buffer dropped, those modifiers and the digit marked swallowed (their ups swallowed, digit up → `hotkey(digit, false)`); any other non-modifier down → flush buffered modifier downs in order, then emit the key; non-modifier up → emit; modifier up: swallowed → consume, still buffered → emit down then up, else emit up. `combo` is called for every combo the tracker reports (for `PadPresses`).

- [ ] **Step 1: Write the failing tests**:
```go
func TestForwarderDispatchesHotkeysWithoutLeakingModifiers(t *testing.T) {
	// 29↓ 56↓ 125↓ 2↓ 2↑ 125↑ 56↑ 29↑ → hotkey(1,true), hotkey(1,false); emit never called; combo "ctrl+opt+cmd+1" once
}
func TestForwarderPassesTypedCombos(t *testing.T) {
	// 125↓ 42↓ 5↓ 5↑ 42↑ 125↑ (cmd+shift+4) → emit 125↓ 42↓ 5↓ 5↑ 42↑ 125↑ in that order; hotkey never called; combo "shift+cmd+4"
}
func TestForwarderFlushesABareModifierTap(t *testing.T) {
	// 125↓ 125↑ → emit 125↓ 125↑
}
func TestForwarderIgnoresRepeats(t *testing.T) {
	// 30↓ 30(2) 30(2) 30↑ → emit 30↓ 30↑
}
func TestForwarderHotkeyNeedsAllThreeModifiers(t *testing.T) {
	// 29↓ 125↓ 2↓ … (no opt) → forwarded, hotkey not called
}
```
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `forward_linux.go`.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `feat(platform): grab-and-forward state machine for the pad's keys`.

### Task 6: pad watcher and `Listen`

**Files:**
- Create: `internal/platform/padwatch_linux.go`, `internal/platform/padwatch_linux_test.go`
- Modify: `internal/platform/evdev_linux.go` (add `func createVirtualPad(name string, vendor, product uint16) (*vkbd, error)` test helper? No — put it in `padwatch_linux_test.go` using the uinput code with bus USB)

**Interfaces:**
- Produces: `type PadInfo struct{ Product, Manufacturer string; VendorID, ProductID int }`, `type PadPress struct{ Combo string; At int64 }` (same as darwin); `func WatchPad()` (every 2 s: `padNodes(pad.VendorID, pad.ProductID)`; new nodes with `HasKey(KEY_1)` → open, grab, start a reader feeding one `forwarder` per node; vanished nodes closed; `inputAllowed` = every present node opened); `func PadStatus() ([]PadInfo, bool)`; `func PadPresses(since int) ([]PadPress, int)` (200 kept, 30 ms dedupe across nodes, as darwin); `func Listen(onKey func(digit int, down bool)) (stop func(), err error)` — errors with `fmt.Errorf("cannot open /dev/uinput to forward the pad's keys: %w; run `vibe-coder-deck udev --install`", err)` when the virtual keyboard cannot be created, otherwise installs `onKey` (stop uninstalls) and calls `WatchPad()`; `func RunMain(f func())` = `mainthread.Init(f)`; `func InputMonitoringAllowed() bool` = no pad node present, or every present node opened.

- [ ] **Step 1: Write the failing device tests** (skip without uinput): create a virtual pad with uinput (bus 0x03, 0x1189, 0x8890, name "Macro Keyboard", keys ctrl/opt/cmd/shift/1..9/a..z) → `WatchPad()`; poll `PadStatus()` ≤ 3 s until connected (`Product == "Macro Keyboard"` is acceptable when sysfs has no USB ancestor: assert `VendorID == 0x1189`). `TestWatcherDispatchesHotkey`: send 29↓56↓125↓2↓2↑125↑56↑29↑ from the virtual pad → `Listen` callback gets (1,true),(1,false); `PadPresses(0)` contains `ctrl+opt+cmd+1`; the virtual keyboard's own node saw nothing. `TestWatcherForwardsTypedCombo`: send cmd+shift+4 → the virtual keyboard node sees 125↓42↓5↓5↑42↑125↑. `TestWatcherFollowsReplug`: destroy the virtual pad (`UI_DEV_DESTROY`), poll until `PadStatus` disconnected (≤ 5 s), create it again, poll until connected, send the hotkey again → callback fires.
- [ ] **Step 2: Run** in Docker `--privileged` — FAIL.
- [ ] **Step 3: Implement** `padwatch_linux.go`.
- [ ] **Step 4: Run** — PASS (three tests).
- [ ] **Step 5: Commit** `feat(platform): Linux pad watcher grabs the pad and dispatches hotkeys`.

### Task 7: permissions and the udev rule

**Files:**
- Create: `internal/platform/udev/70-vibe-coder-deck.rules`, `internal/platform/permissions_linux.go`, `internal/platform/permissions_linux_test.go`, `internal/platform/dock_linux.go` (`SetDockVisible` no-op)
- Modify: `main.go` (add `udev` command: `udev` prints the rule, `udev --install` installs it; help text at `main.go:60-75`), `internal/pad/usb_linux.go` (error text)

**Interfaces:**
- Produces: `//go:embed udev/70-vibe-coder-deck.rules` → `var UdevRule string`; `const UdevRulePath = "/etc/udev/rules.d/70-vibe-coder-deck.rules"`; `func InstallUdevRule() error` — writes with `pkexec sh -c '...'` when `DISPLAY` or `WAYLAND_DISPLAY` is set, else `sudo sh -c '...'`; the script: `cat > PATH`, `udevadm control --reload`, `udevadm trigger`; error wraps the manual steps; `type Permission struct{ Name, Label, Why string; Granted bool }`; `const PermAccessibility = "uinput"`, `PermInputMonitoring = "pad-input"`, `PermPadWrite = "pad-hidraw"`, `PermRecording = "recording"`; `func Permissions() []Permission` (labels: "Virtual keyboard (/dev/uinput)", "Read the pad's keys", "Write the pad", "Record keystrokes"; Why: one sentence each; pad entries granted when the pad is absent); `func RequestPermission(name string) error` → `InstallUdevRule()` for the first three, and for `recording` returns `errors.New("add yourself to the input group: sudo usermod -aG input $USER, then log out and in")`; `func hidrawWritable() bool` uses `pad.HidrawNode()` (export `hidrawNode` as `HidrawNode(vid, pid, iface int) string` in `internal/pad/usb_linux.go`).
- Rule text exactly:
```
# Vibe Coder Deck: the macropad's keys (evdev), its config interface (hidraw)
# and a virtual keyboard to type macros with. Installed by `vibe-coder-deck udev --install`.
SUBSYSTEM=="input", ATTRS{idVendor}=="1189", ATTRS{idProduct}=="8890", TAG+="uaccess"
SUBSYSTEM=="hidraw", ATTRS{idVendor}=="1189", ATTRS{idProduct}=="8890", TAG+="uaccess"
KERNEL=="uinput", SUBSYSTEM=="misc", TAG+="uaccess", OPTIONS+="static_node=uinput"
```

- [ ] **Step 1: Write the failing tests**: `TestUdevRuleMentionsThePadAndUinput` (rule contains `1189`, `8890`, `uinput`, `uaccess`); `TestPermissionsWithoutAccess`: with `sysInput` pointed at the Task 4 fake tree and `uinputPath`/`devInput` vars pointed at unreadable temp files, `Permissions()` has 4 entries, `uinput` and `pad-input` not granted, `pad-hidraw` not granted, names in order `uinput, pad-input, pad-hidraw, recording`; `TestRequestRecordingExplainsTheInputGroup` (error text contains `usermod -aG input`).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement**; add `udevCmd(args []string) error` to `main.go` and its help lines; update the hidraw error text.
- [ ] **Step 4: Run** `go test ./internal/platform/` in Docker and `go build .` for linux — PASS; macOS `go test ./...` PASS.
- [ ] **Step 5: Commit** `feat(linux): permissions, udev rule and the udev command`.

### Task 8: installed apps, icons, login item

**Files:**
- Create: `internal/platform/apps_linux.go`, `internal/platform/apps_linux_test.go`, `internal/platform/login_linux.go`, `internal/platform/login_linux_test.go`

**Interfaces:**
- Produces: `type InstalledApp struct{ Name, BundleID, Path string }` (json tags as darwin); `type desktopEntry struct{ ID, Path, Name, Exec, Icon, WMClass string; NoDisplay bool }`; `func parseDesktopEntry(path string, r io.Reader) (desktopEntry, bool)` (false unless `[Desktop Entry]` with `Type=Application` and a `Name`; `ID` = file name without `.desktop`); `func desktopDirs() []string` (`$XDG_DATA_HOME` or `~/.local/share`, then `$XDG_DATA_DIRS` or `/usr/local/share:/usr/share`, each + `/applications`); `func installedEntries() []desktopEntry` (first ID wins, sorted by Name, cached 30 s); `func InstalledApps() []InstalledApp` (skips NoDisplay); `func AppInfo(path string) (InstalledApp, error)` (a `.desktop` path → parsed; another path → `Name` = base name, `BundleID` = base name, `Path` = path); `func findIcon(icon string, size int, iconDirs []string) (string, bool)` (absolute `.png` → itself; else `<dir>/hicolor/<WxH>/apps/<icon>.png` for sizes `256,128,96,64,48,32` preferring the smallest ≥ size, then any theme dir, then `<dir>/../pixmaps/<icon>.png`); `func AppIconPNG(path string, size int) ([]byte, error)`; `func ChooseApp() (InstalledApp, bool, error)` → `errors.New("choosing a file is not supported on Linux; pick an installed app")`.
- Login: `const LoginItemLabel = "vibe-coder-deck"`; `func autostartPath() string` (`$XDG_CONFIG_HOME` or `~/.config` + `/autostart/vibe-coder-deck.desktop`); `func LoginItemEnabled() bool`; `func SetLoginItem(enabled bool, executable, logDir string) error` writes
```
[Desktop Entry]
Type=Application
Name=Vibe Coder Deck
Exec=env VCK_LOGIN_START=1 "<executable>" app
X-GNOME-Autostart-enabled=true
```

- [ ] **Step 1: Write the failing tests**: `TestParseDesktopEntry` (sample with `Name=Firefox`, `Exec=firefox %u`, `Icon=firefox`, `StartupWMClass=firefox`, `NoDisplay=false` → fields; a `Type=Link` file → false); `TestInstalledAppsFromXdgDirs` (temp `XDG_DATA_HOME` + `XDG_DATA_DIRS` with two entries and one NoDisplay → two apps sorted by name, IDs `org.mozilla.firefox`, `code`); `TestFindIconPrefersSmallestLargeEnoughPng` (temp icon tree with 48 and 128 → size 64 picks 128; size 200 picks 128; pixmaps fallback); `TestLoginItemWritesAutostartDesktopFile` (temp `XDG_CONFIG_HOME`; enabled → file with `Exec=env VCK_LOGIN_START=1 "/opt/vcd" app`; disabled → removed; `LoginItemEnabled` follows).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — PASS (Docker, unprivileged).
- [ ] **Step 5: Commit** `feat(linux): installed apps from .desktop files, icons, login item`.

### Task 9: app in front on X11

**Files:**
- Create: `internal/platform/frontmost_linux.go`, `internal/platform/frontmost_linux_test.go`
- Modify: `go.mod` (`go get github.com/jezek/xgb@latest`)

**Interfaces:**
- Produces: `type x11 struct{ conn *xgb.Conn; root xproto.Window; atoms map[string]xproto.Atom }`; `func openX11() (*x11, error)` (sync.Once, nil when `DISPLAY` empty or `XDG_SESSION_TYPE == "wayland"` — log once `frontmost: Wayland session, profile switching needs X11`); `func (x *x11) active() (pid int, class, instance string, err error)` (`_NET_ACTIVE_WINDOW` → `_NET_WM_PID`, `WM_CLASS`); `func identify(pid int, class string, entries []desktopEntry) (bundleID, name, path string)` — match `StartupWMClass == class`, then `Exec` basename == `/proc/<pid>/exe` basename or `class` lower-cased == entry ID, else `(strings.ToLower(class), class, exe)`; `func FrontmostApp() (bundleID, name, path string)`.

- [ ] **Step 1: Write the failing tests**: `TestIdentifyMatchesDesktopEntries` (pure: StartupWMClass match, Exec basename match, fallback); `TestFrontmostAppUnderXvfb` (skip without `DISPLAY`; start `xdotool` is not needed: open our own window with xgb — `xproto.CreateWindow` + `MapWindow` + set `_NET_WM_PID` and `WM_CLASS` to `vcdtest`; openbox (started by the test harness in `scripts/docker-linux.sh test`) makes it active; poll `FrontmostApp()` ≤ 3 s for `bundleID == "vcdtest"`).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement**; `scripts/docker-linux.sh test` (Task 11) runs tests under `xvfb-run -a sh -c 'openbox & exec go test …'`.
- [ ] **Step 4: Run** in Docker — PASS; without DISPLAY the X test skips.
- [ ] **Step 5: Commit** `feat(linux): app in front on X11`.

### Task 10: updater for a bare Linux binary

**Files:**
- Modify: `internal/update/asset.go:10-27`, `internal/update/install.go:17-24,59-85`, `internal/update/updater.go` (add `GOARCH string` next to `GOOS`, default `runtime.GOARCH`), `main.go` (none; `TargetFromExecutable` keeps its name)
- Test: `internal/update/asset_test.go`, `internal/update/install_test.go`

**Interfaces:**
- `func PickAsset(assets []Asset, goos, goarch string) (Asset, bool)` — darwin `-macos`, windows `-windows`, linux `-linux-<goarch>`; all `.zip`. Update the two callers (`updater.go`, tests).
- `func TargetFromExecutable(exe string) string` → `targetFor(exe, runtime.GOOS)`; `func targetFor(exe, goos string) string`: `.app` and `.exe` rules as before on every OS; on `linux` a plain path is itself unless it contains `/go-build` or is under `os.TempDir()`.
- `findPayload(dir, target)`: when `target` has neither `.app` nor `.exe`, the payload is the regular file named `filepath.Base(target)`; `swap` chmods the new file `0755` when it is a regular file.

- [ ] **Step 1: Write the failing tests**: `TestPickAssetLinuxByArch` (`vibe-coder-deck-v0.2.0-linux-amd64.zip` chosen for `linux/amd64`, not for `linux/arm64`); `TestTargetForLinuxSkipsGoBuildDirs` (`/usr/local/bin/vibe-coder-deck` → itself; `/tmp/go-build123/b001/exe/main` → `""`; `/home/u/.cache/go-build/…` → `""`); `TestInstallReplacesABareBinary` (zip with `vibe-coder-deck` 0755 → installed over a target file, mode 0755, old removed).
- [ ] **Step 2: Run** `go test ./internal/update/` — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — PASS (macOS).
- [ ] **Step 5: Commit** `feat(update): Linux assets and in-place binary replacement`.

### Task 11: desktop app, build script, Docker runner, CI, packaging

**Files:**
- Create: `scripts/build-linux.sh`, `scripts/docker-linux.sh`, `scripts/linux/vibe-coder-deck.desktop`, `scripts/linux/README.md`
- Modify: `internal/app/app.go` (Linux: see spike note), `main.go:384-403` (`appCmd` on Linux), `.github/workflows/release.yml`, `.github/workflows/test.yml`, `.gitignore` (dist/ already)

**Spike note (fill in before implementing):** result of building `./internal/app/` in `vcd-linux-builder`: ___. If it builds: keep `app.Run` on Linux (GTK window, StatusNotifier tray) and verify under Xvfb that the window opens (`xdotool search --name "Vibe Coder Deck"` within 10 s). If it does not: `appCmd` on Linux = `serve` + `openBrowser` and `internal/app` gets `//go:build darwin || windows`.

**Interfaces:**
- `scripts/build-linux.sh`: `VERSION` env (default `git describe`), builds `dist/linux/vibe-coder-deck` (`CGO_ENABLED=1 GOOS=linux`, `-ldflags "-X main.version=$VERSION"`), makes `dist/linux/vibe-coder-deck.png` (256 px, from `assets/icon-1024.png` via `convert` when present, else copies the 1024), copies `.desktop`, `README.md`, the udev rule from `internal/platform/udev/`, zips to `dist/vibe-coder-deck-$VERSION-linux-$GOARCH.zip`.
- `scripts/docker-linux.sh build|test|shell [args]`: builds `vcd-linux-builder` from `scripts/linux/Dockerfile` when missing; `test` runs `docker run --privileged … xvfb-run -a sh -c 'openbox >/dev/null 2>&1 & exec go test ./... "$@"'` with the Go caches as named volumes; `build` runs `scripts/build-linux.sh`.
- `release.yml`: job `linux` (`runs-on: ubuntu-24.04`): checkout, setup-go, `sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev xvfb openbox`, `go test ./...`, `sudo -E env PATH=$PATH xvfb-run -a go test ./internal/platform/` (device tests), `scripts/build-linux.sh`, `actions/upload-artifact` of `dist/*.zip`; the macOS job gets `needs: linux` and `actions/download-artifact` into `dist/` before Package (checksums cover all zips). `test.yml`: same job without build/upload.
- `.desktop`: `Name=Vibe Coder Deck`, `Exec=vibe-coder-deck app`, `Icon=vibe-coder-deck`, `Categories=Utility;`, `Terminal=false`, `Type=Application`.
- `scripts/linux/README.md`: unzip, copy the binary to `~/.local/bin`, `vibe-coder-deck udev --install`, copy the .desktop and icon to `~/.local/share/{applications,icons/hicolor/256x256/apps}`, Wayland note, layout note.

- [ ] **Step 1:** Fill the spike note; if GTK builds, add a test `internal/app/app_linux_test.go` that is skipped without DISPLAY and otherwise runs `app.Run` with a stub `Start` for 10 s in a goroutine and asserts `xdotool search --name` finds the window.
- [ ] **Step 2: Run** `scripts/docker-linux.sh test` — expect the app test FAIL/undefined.
- [ ] **Step 3: Implement** the scripts, workflows, `appCmd`/`app.go` changes.
- [ ] **Step 4: Run** `scripts/docker-linux.sh test` → all PASS; `scripts/docker-linux.sh build` → the zip exists and `unzip -l` lists the five files; macOS `go test ./...`, Windows cross-build PASS.
- [ ] **Step 5: Commit** `feat(linux): desktop app, build script, Docker runner and CI job`.

### Task 12: recorder

**Files:**
- Create: `internal/platform/recorder_linux.go`, `internal/platform/recorder_linux_test.go`

**Interfaces:**
- Produces: `type RecordedEvent struct{ Type, Value string; At int64 }` (json tags as darwin); `func StartRecording(mouse bool) error` (opens every `/dev/input/event*` with `HasKey(KEY_A)` — keyboards — plus, when `mouse`, those with `BTN_LEFT`; no grab; `errors.New("no keyboard can be read: see the recording permission")` when none opens); `func StopRecording()`; `func Recording(since int) (bool, []RecordedEvent)`; events: `keydown`/`keyup` with `KeyName`, `click` with `left/right/middle` on button down.

- [ ] **Step 1: Write the failing device test**: start recording, type `ctrl+c` through the virtual keyboard (which is itself an event node) → events `keydown ctrl, keydown c, keyup c, keyup ctrl`; with `mouse=true` a `PostClick("left")` → `click left`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `feat(linux): keystroke recorder from evdev`.

### Task 13: docs, changelog, handoff

**Files:**
- Modify: `README.md` (Linux section: download, udev, Wayland limits, layout note, build in Docker), `CHANGELOG.md` (`## [Unreleased]` → `### Added` Linux preview), `HANDOFF.md`

- [ ] **Step 1:** Write the sections; `scripts/release.sh` still finds the version header.
- [ ] **Step 2:** `go test ./...` (macOS), `scripts/docker-linux.sh test`, Windows cross-build — PASS.
- [ ] **Step 3: Commit** `docs: Linux preview`.

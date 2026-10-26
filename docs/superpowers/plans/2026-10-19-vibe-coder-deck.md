# vibe-coder-deck — plan

Standalone, compiled port of nehacloud's `/macropad` panel: one binary that configures the
vibe-coder-keyboard macropad like a Stream Deck and runs what its keys do. macOS first, Windows
next; behaviour identical to the nehacloud panel.

## Decisions

- **Language/runtime:** Go. One binary, `vibe-coder-deck serve` runs the web UI, the hotkey
  listener and the dispatcher in the same process.
- **Database:** SQLite via `modernc.org/sqlite` (pure Go, no cgo, cross-compiles). File at
  `os.UserConfigDir()/vibe-coder-deck/deck.db`. Tables: `keys` (one row per slot), `runs` (one per
  press), `cooldowns` (slot → until), `meta` (synced_at).
- **Cache / guardrail:** no Redis. The gate lives in the serving process: an in-memory lock per key
  while a run is in flight (single instance) and a cooldown persisted in SQLite so it survives a
  restart. Same semantics as nehacloud's `KeyRunGate`: `off`, `not-dispatched`, `running`,
  `cooldown`; runs end as `ok`, `failed` or `skipped`.
- **Hotkeys:** registered in-process with `golang.design/x/hotkey` (Carbon `RegisterEventHotKey`
  on macOS, `RegisterHotKey` on Windows). No skhd, no Accessibility permission. Dispatched slots
  make the pad send `ctrl+opt+cmd+<digit>` (Windows: ctrl+alt+win+<digit>, the pad's cmd is the
  GUI key); digits are the slot positions, key1..key6 = 1..6, knob left/press/right = 7/8/9.
- **Pad writes:** behind a `pad.Writer` interface. Phase 1 shells out to the `macropad` CLI
  (`set`/`clear`, exit 2 = not connected stops the sync). Phase 2 (Windows) implements the 64-byte
  protocol from vibe-coder-keyboard/docs/protocol.md over libusb.
- **Macro steps:** `keys` (pad syntax, comma-separated combos), `text`, `delay` (ms). macOS plays
  them with `osascript` (System Events); Windows with `SendInput`. System shortcuts (screenshots,
  Spotlight) ignore synthetic keys: the UI says to use Keys mode for those.
- **App scope:** frontmost app name; macOS `lsappinfo`, Windows `GetForegroundWindow`.
- **UI:** embedded static page (vanilla JS + CSS), same layout as nehacloud: 3×2 key grid + knob
  column, one form per slot, sync banner, plus an "Apply to pad" button (the binary can reach the
  pad, nehacloud could not).
- **Import:** `vibe-coder-deck import < keys.json` takes `php artisan macropad:keys --json` output
  so the current configuration moves over.

## Layout

```
main.go                      commands: serve, sync, keys, set, import, press, version
internal/deck/               Slot, Mode, Key, Step, Store (sqlite), Gate, Dispatcher, Sync
internal/pad/                Writer interface, CLI writer (macropad), fake for tests
internal/platform/           frontmost app, keystrokes, notifications, hotkey listener (per OS)
internal/web/                HTTP API + embedded static UI
```

## Steps (TDD, each red → green)

1. `deck`: Slot/Mode/Key/Step parsing, validation, hotkey and pad action.
2. `deck.Store`: migrations, EnsureAll in pad order, SaveKey, runs, cooldowns, synced/changed.
3. `deck.Gate`: acquire/release/skip semantics above, with an injectable clock.
4. `deck.Dispatcher`: press → gate → app scope → runner (fake) → finish.
5. `pad.CLI`: command lines, exit-code mapping; `deck.Sync` plan + stop at not-connected.
6. `platform` (darwin): AppleScript combo builder (tested), lsappinfo parsing (tested), osascript
   runner, notify, hotkey listener with mainthread.
7. `web`: JSON API (tested with httptest) + static page.
8. `main.go` commands; import from nehacloud JSON; README.
9. Live: import current config, `sync`, press key1 with the handoff dry-run marker, key2 screenshot.

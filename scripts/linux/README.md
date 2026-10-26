# Vibe Coder Deck on Linux (preview)

Works on X11 and Wayland alike for the pad itself: the app reads the pad's own
input device and types macros through a virtual keyboard (uinput), nothing goes
through the display server. "Which app is in front" (profile switching) works on
X11 only for now.

## Install

```sh
unzip vibe-coder-deck-*-linux-*.zip -d vibe-coder-deck && cd vibe-coder-deck
install -Dm755 vibe-coder-deck ~/.local/bin/vibe-coder-deck
install -Dm644 vibe-coder-deck.desktop ~/.local/share/applications/vibe-coder-deck.desktop
install -Dm644 vibe-coder-deck.png ~/.local/share/icons/hicolor/256x256/apps/vibe-coder-deck.png
~/.local/bin/vibe-coder-deck udev --install   # asks for your password: device access without root
```

Then unplug and replug the pad, and start **Vibe Coder Deck** from your app menu
(or `vibe-coder-deck app`). The udev rule (`70-vibe-coder-deck.rules`) lets the
logged-in user read the pad, write it, and use `/dev/uinput`; the Permissions
section of the app shows what is still missing.

- GTK 4.10+ and WebKitGTK 6.0 are needed at run time (`libgtk-4-1`, `libwebkitgtk-6.0-4`
  on Debian/Ubuntu); the tray icon needs a StatusNotifier host (KDE has one, GNOME
  needs the AppIndicator extension).
- The macro recorder reads every keyboard: `sudo usermod -aG input $USER`, then log out and in.
- `text` steps type as on a US keyboard; combos by key name work on any layout.
- Updates: the app replaces its own binary in place (`vibe-coder-deck update --install` does the same).

//go:build linux

package platform

import (
	"errors"
	"os"
	"path/filepath"
)

// LoginItemLabel names the autostart entry.
const LoginItemLabel = "vibe-coder-deck"

// autostartPath is the XDG autostart entry that starts the app at login.
func autostartPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", LoginItemLabel+".desktop")
}

// LoginItemEnabled is whether the autostart entry exists.
func LoginItemEnabled() bool {
	_, err := os.Stat(autostartPath())
	return err == nil
}

// SetLoginItem writes or removes the autostart entry. logDir is unused: the
// app writes its own log.
func SetLoginItem(enabled bool, executable, logDir string) error {
	path := autostartPath()
	if !enabled {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=Vibe Coder Deck\nComment=Configure and run the macropad\n" +
		"Exec=env VCK_LOGIN_START=1 \"" + executable + "\" app\nX-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(path, []byte(entry), 0o644)
}

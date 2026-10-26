//go:build windows

package platform

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// LoginItemLabel names the Run value that starts the app at login.
const LoginItemLabel = "VibeCoderDeck"

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func LoginItemEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(LoginItemLabel)
	return err == nil
}

// SetLoginItem adds or removes the app from the current user's Run key.
func SetLoginItem(enabled bool, executable, logDir string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		err := k.DeleteValue(LoginItemLabel)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	return k.SetStringValue(LoginItemLabel, `"`+executable+`"`)
}

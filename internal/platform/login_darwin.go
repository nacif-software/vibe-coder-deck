//go:build darwin

package platform

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LoginItemLabel names the launch agent that starts the app at login.
const LoginItemLabel = "dev.lucasabreu.vibe-coder-deck"

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", LoginItemLabel+".plist"), nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// LaunchAgentPlist is the launchd job that starts executable at login, with
// VCK_LOGIN_START so the app knows not to open the page by itself.
func LaunchAgentPlist(executable, logDir string) string {
	logFile := xmlEscape(filepath.Join(logDir, "login.log"))
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
	<key>EnvironmentVariables</key>
	<dict>
		<key>VCK_LOGIN_START</key>
		<string>1</string>
	</dict>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, LoginItemLabel, xmlEscape(executable), logFile, logFile)
}

// LoginItemEnabled reports whether the launch agent is installed.
func LoginItemEnabled() bool {
	path, err := launchAgentPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// SetLoginItem installs or removes the launch agent. launchd reads
// ~/Library/LaunchAgents at login, so nothing is loaded or unloaded now: that
// would start a second copy, or stop this one.
func SetLoginItem(enabled bool, executable, logDir string) error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
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
	return os.WriteFile(path, []byte(LaunchAgentPlist(executable, logDir)), 0o644)
}

//go:build darwin

package platform

import (
	"strings"
	"testing"
)

func TestLaunchAgentPlist(t *testing.T) {
	plist := LaunchAgentPlist("/Applications/Vibe Coder Deck.app/Contents/MacOS/vibe-coder-deck", "/Users/me/Library/Logs")
	for _, want := range []string{
		"<key>Label</key>",
		"<string>" + LoginItemLabel + "</string>",
		"<string>/Applications/Vibe Coder Deck.app/Contents/MacOS/vibe-coder-deck</string>",
		"<key>RunAtLoad</key>",
		"<key>VCK_LOGIN_START</key>",
		"<string>/Users/me/Library/Logs/login.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
	if !strings.HasPrefix(plist, "<?xml") {
		t.Error("plist must start with the xml declaration")
	}
}

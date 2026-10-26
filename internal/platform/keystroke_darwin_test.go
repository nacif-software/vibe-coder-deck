//go:build darwin

package platform

import "testing"

func TestAppleScriptForCombo(t *testing.T) {
	cases := map[string]string{
		"cmd+shift+4": `keystroke "4" using {command down, shift down}`,
		"enter":       `key code 36`,
		"ctrl+left":   `key code 123 using {control down}`,
		"opt+p":       `keystroke "p" using {option down}`,
		"comma":       `keystroke ","`,
	}
	for combo, want := range cases {
		got, err := AppleScriptForCombo(combo)
		if err != nil || got != want {
			t.Errorf("AppleScriptForCombo(%q) = %q, %v; want %q", combo, got, err, want)
		}
	}
	for _, bad := range []string{"cmd+shift", "cmd+foo", ""} {
		if _, err := AppleScriptForCombo(bad); err == nil {
			t.Errorf("AppleScriptForCombo(%q) should fail", bad)
		}
	}
}

func TestAppleScriptForText(t *testing.T) {
	if got := AppleScriptForText(`say "hi"`); got != `keystroke "say \"hi\""` {
		t.Fatalf("got %q", got)
	}
}

func TestParseFrontmost(t *testing.T) {
	if got := ParseFrontmost(`"LSDisplayName"="Orca"` + "\n"); got != "Orca" {
		t.Fatalf("got %q", got)
	}
	if got := ParseFrontmost("garbage"); got != "" {
		t.Fatalf("got %q", got)
	}
}

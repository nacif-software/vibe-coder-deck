//go:build linux

package platform

import "testing"

func TestKeyCodesPinLinuxValues(t *testing.T) {
	for name, want := range map[string]uint16{
		"a": 30, "z": 44, "1": 2, "0": 11, "enter": 28, "esc": 1, "space": 57,
		"ctrl": 29, "shift": 42, "opt": 56, "cmd": 125, "rcmd": 126, "left": 105, "f12": 88, "f13": 183, "kp-enter": 96,
		"[": 26, "lbracket": 26, "delete": 14, "forward-delete": 111, "volume-up": 115, "play": 164, "alt": 56, "super": 125,
	} {
		if got, err := KeyCode(name); err != nil || got != want {
			t.Errorf("%s: %d %v, want %d", name, got, err, want)
		}
	}
	if _, err := KeyCode("no-such"); err == nil {
		t.Error("unknown name must fail")
	}
	if c, _ := KeyCode("code:200"); c != 200 {
		t.Error("code:N")
	}
	for code, want := range map[int]string{30: "a", 26: "[", 14: "backspace", 125: "cmd", 115: "volume-up", 999: "code:999"} {
		if got := KeyName(code); got != want {
			t.Errorf("KeyName(%d)=%q want %q", code, got, want)
		}
	}
	if !IsModifier("shift") || IsModifier("a") {
		t.Error("IsModifier")
	}
	if code, ok := MediaKey("play-pause"); !ok || code != 164 {
		t.Errorf("MediaKey(play-pause) = %d %v", code, ok)
	}
	if MediaKeyName(115) != "volume-up" {
		t.Error("MediaKeyName")
	}
	if ch := usLayout['A']; ch.Code != 30 || !ch.Shift {
		t.Error("A = shift+a")
	}
	if ch := usLayout['!']; ch.Code != 2 || !ch.Shift {
		t.Error("! = shift+1")
	}
	if ch := usLayout['\n']; ch.Code != 28 {
		t.Error("newline = enter")
	}
	if b, _ := ButtonCode("right"); b != 0x111 {
		t.Error("right button")
	}
}

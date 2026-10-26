//go:build darwin

package platform

import "testing"

func TestVirtualKeyCode(t *testing.T) {
	cases := map[string]uint16{
		"a": 0, "4": 21, "cmd": 55, "shift": 56, "opt": 58, "ctrl": 59, "enter": 36, "esc": 53,
		"space": 49, "left": 123, "f5": 96, "comma": 43, "backspace": 51, "tab": 48,
	}
	for name, want := range cases {
		got, err := VirtualKeyCode(name)
		if err != nil || got != want {
			t.Errorf("VirtualKeyCode(%q) = %d, %v; want %d", name, got, err, want)
		}
	}
	if _, err := VirtualKeyCode("teleport"); err == nil {
		t.Error("unknown key should fail")
	}
}

func TestModifierFlags(t *testing.T) {
	held := map[string]bool{"cmd": true, "shift": true}
	flags := ModifierFlags(held)
	if flags&flagCommand == 0 || flags&flagShift == 0 || flags&flagOption != 0 || flags&flagControl != 0 {
		t.Fatalf("flags = %#x", flags)
	}
	if ModifierFlags(nil) != 0 {
		t.Fatal("no modifiers should give no flags")
	}
	if !IsModifier("cmd") || IsModifier("a") {
		t.Fatal("IsModifier")
	}
}

// Media keys arrive as NX_SYSDEFINED events with an NX_KEYTYPE_* code
// (IOKit's ev_keymap.h): sound up 0, down 1, brightness up 2, down 3, mute 7,
// play 16, next 17, previous 18.
func TestMediaKeys(t *testing.T) {
	cases := map[string]int{"play": 16, "next": 17, "prev": 18, "mute": 7, "volume-up": 0, "volume-down": 1, "brightness-up": 2, "brightness-down": 3}
	for name, want := range cases {
		if got, ok := MediaKey(name); !ok || got != want {
			t.Errorf("MediaKey(%q) = %d, %v; want %d", name, got, ok, want)
		}
		if got := MediaKeyName(want); got != name {
			t.Errorf("MediaKeyName(%d) = %q, want %q", want, got, name)
		}
	}
	if got, ok := MediaKey("play-pause"); !ok || got != 16 {
		t.Errorf("alias play-pause = %d, %v", got, ok)
	}
	if got := MediaKeyName(99); got != "media:99" {
		t.Errorf("unknown media key = %q", got)
	}
	if got, ok := MediaKey("media:99"); !ok || got != 99 {
		t.Errorf("MediaKey(media:99) = %d, %v", got, ok)
	}
	if _, ok := MediaKey("a"); ok {
		t.Error("a is not a media key")
	}
}

// Keys without a name (Mission Control, Spotlight, Dictation on newer Macs)
// are recorded and played back by their key code.
func TestUnnamedKeysByCode(t *testing.T) {
	if got := KeyName(160); got != "code:160" {
		t.Errorf("KeyName(160) = %q", got)
	}
	if got := KeyName(0); got != "a" {
		t.Errorf("KeyName(0) = %q", got)
	}
	if got := KeyName(55); got != "cmd" {
		t.Errorf("KeyName(55) = %q", got)
	}
	if got := KeyName(122); got != "f1" {
		t.Errorf("KeyName(122) = %q", got)
	}
	if got, err := VirtualKeyCode("code:160"); err != nil || got != 160 {
		t.Errorf("VirtualKeyCode(code:160) = %d, %v", got, err)
	}
}

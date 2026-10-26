//go:build darwin

package platform

import (
	"fmt"
	"strings"
)

// CGEventFlags bits for the modifiers, as Quartz defines them.
const (
	flagShift   uint64 = 0x20000
	flagControl uint64 = 0x40000
	flagOption  uint64 = 0x80000
	flagCommand uint64 = 0x100000
)

// virtualKeys maps key names (what the recorder and the pad syntax use) to
// macOS virtual key codes of the ANSI layout.
var virtualKeys = map[string]uint16{
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9, "b": 11,
	"q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17, "1": 18, "2": 19, "3": 20, "4": 21,
	"6": 22, "5": 23, "equal": 24, "=": 24, "9": 25, "7": 26, "minus": 27, "-": 27, "8": 28,
	"0": 29, "]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "enter": 36, "return": 36,
	"l": 37, "j": 38, "'": 39, "k": 40, "semicolon": 41, ";": 41, "\\": 42, "comma": 43, ",": 43,
	"slash": 44, "/": 44, "n": 45, "m": 46, "period": 47, ".": 47, "tab": 48, "space": 49,
	"`": 50, "backspace": 51, "delete": 51, "esc": 53, "escape": 53, "cmd": 55, "command": 55,
	"shift": 56, "capslock": 57, "opt": 58, "alt": 58, "option": 58, "ctrl": 59, "control": 59,
	"rshift": 60, "ropt": 61, "rctrl": 62, "fn": 63, "f17": 64, "f18": 79, "f19": 80, "f20": 90,
	"f5": 96, "f6": 97, "f7": 98, "f3": 99, "f8": 100, "f9": 101, "f11": 103, "f13": 105,
	"f16": 106, "f14": 107, "f10": 109, "f12": 111, "f15": 113, "home": 115, "pageup": 116,
	"forward-delete": 117, "f4": 118, "end": 119, "f2": 120, "pagedown": 121, "f1": 122,
	"left": 123, "right": 124, "down": 125, "up": 126, "rcmd": 54,
}

var modifierFlag = map[string]uint64{
	"cmd": flagCommand, "command": flagCommand, "rcmd": flagCommand,
	"shift": flagShift, "rshift": flagShift,
	"opt": flagOption, "alt": flagOption, "option": flagOption, "ropt": flagOption,
	"ctrl": flagControl, "control": flagControl, "rctrl": flagControl,
}

// VirtualKeyCode is the macOS key code for a key name.
func VirtualKeyCode(name string) (uint16, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if code, ok := virtualKeys[name]; ok {
		return code, nil
	}
	var code uint16
	if _, err := fmt.Sscanf(name, "code:%d", &code); err == nil && strings.HasPrefix(name, "code:") {
		return code, nil
	}
	return 0, fmt.Errorf("unknown key %q", name)
}

// IsModifier reports whether the key name is a modifier key.
func IsModifier(name string) bool {
	_, ok := modifierFlag[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// ModifierFlags turns the set of held modifier names into CGEventFlags.
func ModifierFlags(held map[string]bool) uint64 {
	var flags uint64
	for name, on := range held {
		if on {
			flags |= modifierFlag[strings.ToLower(name)]
		}
	}
	return flags
}

// codeNames is the name the recorder writes for each key code; codes that
// several names share (cmd/command, enter/return…) get the canonical one.
var codeNames = map[int]string{
	54: "cmd", 55: "cmd", 56: "shift", 60: "shift", 58: "opt", 61: "opt", 59: "ctrl", 62: "ctrl", 57: "capslock", 63: "fn",
	36: "enter", 48: "tab", 49: "space", 51: "backspace", 53: "esc", 117: "forward-delete", 123: "left", 124: "right",
	125: "down", 126: "up", 115: "home", 119: "end", 116: "pageup", 121: "pagedown", 24: "equal", 27: "minus",
	41: "semicolon", 43: "comma", 44: "slash", 47: "period",
}

func init() {
	for name, code := range virtualKeys {
		if _, taken := codeNames[int(code)]; !taken {
			codeNames[int(code)] = name
		}
	}
}

// KeyName is the recorder's name for a key code. Keys without one (Mission
// Control, Spotlight, Dictation on newer Macs) become "code:N", which
// VirtualKeyCode reads back, so nothing pressed is dropped.
func KeyName(code int) string {
	if name, ok := codeNames[code]; ok {
		return name
	}
	return fmt.Sprintf("code:%d", code)
}

// mediaKeys are the NX_KEYTYPE_* codes (IOKit ev_keymap.h) of the keys macOS
// sends as system-defined events rather than key presses: the F-row's
// default functions (brightness, keyboard light, media, volume).
var mediaKeys = map[string]int{
	"volume-up": 0, "volume-down": 1, "brightness-up": 2, "brightness-down": 3, "mute": 7,
	"eject": 14, "play": 16, "next": 17, "prev": 18, "fast-forward": 19, "rewind": 20,
	"keyboard-light-up": 21, "keyboard-light-down": 22, "keyboard-light-toggle": 23,
}

var mediaAliases = map[string]string{"play-pause": "play", "previous": "prev", "vol-up": "volume-up", "vol-down": "volume-down"}

var mediaNames = map[int]string{}

func init() {
	for name, code := range mediaKeys {
		mediaNames[code] = name
	}
}

// MediaKey reads a media key name (or "media:N") as its NX_KEYTYPE code.
func MediaKey(name string) (int, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if a, ok := mediaAliases[name]; ok {
		name = a
	}
	if code, ok := mediaKeys[name]; ok {
		return code, true
	}
	var code int
	if _, err := fmt.Sscanf(name, "media:%d", &code); err == nil && strings.HasPrefix(name, "media:") {
		return code, true
	}
	return 0, false
}

// MediaKeyName is the name for an NX_KEYTYPE code, "media:N" when it has none.
func MediaKeyName(code int) string {
	if name, ok := mediaNames[code]; ok {
		return name
	}
	return fmt.Sprintf("media:%d", code)
}

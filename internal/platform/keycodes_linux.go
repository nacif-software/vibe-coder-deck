//go:build linux

package platform

import (
	"fmt"
	"strings"
)

// Linux input event codes (linux/input-event-codes.h) by key name. The names
// are the ones macros and the recorder use on macOS ("[", "delete" is
// backspace, "forward-delete"); the pad's spellings ("lbracket", "return")
// resolve through keyAliases.
var linuxKeys = map[string]uint16{
	"esc": 1, "1": 2, "2": 3, "3": 4, "4": 5, "5": 6, "6": 7, "7": 8, "8": 9, "9": 10, "0": 11,
	"minus": 12, "equal": 13, "backspace": 14, "tab": 15,
	"q": 16, "w": 17, "e": 18, "r": 19, "t": 20, "y": 21, "u": 22, "i": 23, "o": 24, "p": 25,
	"[": 26, "]": 27, "enter": 28,
	"a": 30, "s": 31, "d": 32, "f": 33, "g": 34, "h": 35, "j": 36, "k": 37, "l": 38,
	"semicolon": 39, "'": 40, "`": 41, "\\": 43,
	"z": 44, "x": 45, "c": 46, "v": 47, "b": 48, "n": 49, "m": 50,
	"comma": 51, "period": 52, "slash": 53, "space": 57, "capslock": 58,
	"f1": 59, "f2": 60, "f3": 61, "f4": 62, "f5": 63, "f6": 64, "f7": 65, "f8": 66, "f9": 67, "f10": 68,
	"numlock": 69, "scrolllock": 70,
	"kp-7": 71, "kp-8": 72, "kp-9": 73, "kp-minus": 74, "kp-4": 75, "kp-5": 76, "kp-6": 77, "kp-plus": 78,
	"kp-1": 79, "kp-2": 80, "kp-3": 81, "kp-0": 82, "kp-period": 83, "kp-asterisk": 55,
	"f11": 87, "f12": 88, "kp-enter": 96, "kp-slash": 98, "printscreen": 99,
	"home": 102, "up": 103, "pageup": 104, "left": 105, "right": 106, "end": 107, "down": 108,
	"pagedown": 109, "insert": 110, "forward-delete": 111, "kp-equal": 117, "pause": 119, "menu": 127,
	"f13": 183, "f14": 184, "f15": 185, "f16": 186, "f17": 187, "f18": 188, "f19": 189, "f20": 190,
	"f21": 191, "f22": 192, "f23": 193, "f24": 194, "fn": 464,
}

var modifierCodes = map[string]uint16{
	"ctrl": 29, "shift": 42, "opt": 56, "cmd": 125,
	"rctrl": 97, "rshift": 54, "ropt": 100, "rcmd": 126,
}

var mediaCodes = map[string]uint16{
	"mute": 113, "volume-down": 114, "volume-up": 115, "eject": 161, "next": 163, "play": 164,
	"prev": 165, "stop": 166, "rewind": 168, "fast-forward": 208, "brightness-down": 224,
	"brightness-up": 225, "keyboard-light-toggle": 228, "keyboard-light-down": 229, "keyboard-light-up": 230,
}

var buttonCodes = map[string]uint16{"left": 0x110, "right": 0x111, "middle": 0x112}

var keyAliases = map[string]string{
	"control": "ctrl", "alt": "opt", "option": "opt", "command": "cmd", "win": "cmd", "super": "cmd",
	"meta": "cmd", "gui": "cmd", "rcontrol": "rctrl", "ralt": "ropt", "roption": "ropt", "rcommand": "rcmd",
	"rwin": "rcmd", "return": "enter", "escape": "esc", "spacebar": "space", "delete": "backspace",
	"lbracket": "[", "rbracket": "]", "grave": "`", "backtick": "`", "quote": "'", "backslash": "\\",
	"-": "minus", "=": "equal", ";": "semicolon", ",": "comma", ".": "period", "dot": "period", "/": "slash",
	"pgup": "pageup", "pgdn": "pagedown", "ins": "insert", "prtsc": "printscreen",
	"arrow-left": "left", "arrow-right": "right", "arrow-up": "up", "arrow-down": "down",
	"play-pause": "play", "previous": "prev", "vol-up": "volume-up", "vol-down": "volume-down",
}

// keyCodeNames is every code the virtual keyboard can send, by code.
var keyCodeNames = map[uint16]string{}

func init() {
	for _, table := range []map[string]uint16{linuxKeys, modifierCodes, mediaCodes} {
		for name, code := range table {
			keyCodeNames[code] = name
		}
	}
}

func canonicalKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if a, ok := keyAliases[name]; ok {
		return a
	}
	return name
}

// KeyCode is the Linux key code for a key, modifier or media name, or code:N.
func KeyCode(name string) (uint16, error) {
	name = canonicalKey(name)
	for _, table := range []map[string]uint16{linuxKeys, modifierCodes, mediaCodes} {
		if code, ok := table[name]; ok {
			return code, nil
		}
	}
	var code uint16
	if _, err := fmt.Sscanf(name, "code:%d", &code); err == nil && strings.HasPrefix(name, "code:") {
		return code, nil
	}
	return 0, fmt.Errorf("unknown key %q", name)
}

// KeyName is the name for a Linux key code, or code:N.
func KeyName(code int) string {
	if name, ok := keyCodeNames[uint16(code)]; ok && int(uint16(code)) == code {
		return name
	}
	return fmt.Sprintf("code:%d", code)
}

// IsModifier reports whether name is a modifier key.
func IsModifier(name string) bool {
	_, ok := modifierCodes[canonicalKey(name)]
	return ok
}

// MediaKey is the key code of a media name (play, volume-up…) or media:N.
func MediaKey(name string) (int, bool) {
	name = canonicalKey(name)
	if code, ok := mediaCodes[name]; ok {
		return int(code), true
	}
	var code int
	if _, err := fmt.Sscanf(name, "media:%d", &code); err == nil && strings.HasPrefix(name, "media:") {
		return code, true
	}
	return 0, false
}

// MediaKeyName is the media name for a key code, or media:N.
func MediaKeyName(code int) string {
	for name, c := range mediaCodes {
		if int(c) == code {
			return name
		}
	}
	return fmt.Sprintf("media:%d", code)
}

// ButtonCode is the BTN_ code of a mouse button: left, right or middle.
func ButtonCode(button string) (uint16, error) {
	if code, ok := buttonCodes[strings.ToLower(strings.TrimSpace(button))]; ok {
		return code, nil
	}
	return 0, fmt.Errorf("unknown mouse button %q", button)
}

// keyChord is how a character is typed on a US keyboard.
type keyChord struct {
	Code  uint16
	Shift bool
}

// usLayout types printable ASCII (plus newline and tab) on a US keyboard.
var usLayout = map[rune]keyChord{'\n': {28, false}, '\t': {15, false}, ' ': {57, false}}

func init() {
	rows := []struct {
		plain, shifted string
		codes          []uint16
	}{
		{"`1234567890-=", "~!@#$%^&*()_+", []uint16{41, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}},
		{"qwertyuiop[]\\", "QWERTYUIOP{}|", []uint16{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 43}},
		{"asdfghjkl;'", "ASDFGHJKL:\"", []uint16{30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}},
		{"zxcvbnm,./", "ZXCVBNM<>?", []uint16{44, 45, 46, 47, 48, 49, 50, 51, 52, 53}},
	}
	for _, row := range rows {
		for i, r := range row.plain {
			usLayout[r] = keyChord{row.codes[i], false}
		}
		for i, r := range row.shifted {
			usLayout[r] = keyChord{row.codes[i], true}
		}
	}
}

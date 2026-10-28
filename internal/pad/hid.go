package pad

import (
	"fmt"
	"strings"
)

// The pad's USB identity (see docs/protocol.md in vibe-coder-keyboard).
const (
	VendorID  = 0x1189
	ProductID = 0x8890
)

// HIDInput is one value the pad reports on its keyboard, consumer or mouse
// interface: a usage on a HID usage page going down or up.
type HIDInput struct {
	Page, Usage uint32
	Down        bool
}

const (
	pageKeyboard = 0x07
	pageButton   = 0x09
	pageConsumer = 0x0C
)

// Modifiers in the order combos are written: ctrl, opt, shift, cmd, then the
// right-hand ones.
var modifierOrder = []string{"ctrl", "opt", "shift", "cmd", "rctrl", "ropt", "rshift", "rcmd"}

// Keyboard usages 0xE0..0xE7.
var modifierUsages = map[uint32]string{
	0xE0: "ctrl", 0xE1: "shift", 0xE2: "opt", 0xE3: "cmd",
	0xE4: "rctrl", 0xE5: "rshift", 0xE6: "ropt", 0xE7: "rcmd",
}

var keyUsages = map[uint32]string{
	0x27: "0", 0x28: "enter", 0x29: "esc", 0x2A: "backspace", 0x2B: "tab", 0x2C: "space",
	0x2D: "minus", 0x2E: "equal", 0x2F: "lbracket", 0x30: "rbracket", 0x31: "backslash",
	0x33: "semicolon", 0x34: "quote", 0x35: "grave", 0x36: "comma", 0x37: "period", 0x38: "slash",
	0x39: "capslock", 0x46: "printscreen", 0x47: "scrolllock", 0x48: "pause", 0x49: "insert",
	0x4A: "home", 0x4B: "pageup", 0x4C: "forward-delete", 0x4D: "end", 0x4E: "pagedown",
	0x4F: "right", 0x50: "left", 0x51: "down", 0x52: "up", 0x53: "numlock",
	0x54: "kp-slash", 0x55: "kp-asterisk", 0x56: "kp-minus", 0x57: "kp-plus", 0x58: "kp-enter",
	0x62: "kp-0", 0x63: "kp-period", 0x65: "menu", 0x67: "kp-equal",
}

func init() {
	for i := uint32(0); i < 26; i++ {
		keyUsages[0x04+i] = string(rune('a' + i))
	}
	for n := uint32(1); n <= 9; n++ {
		keyUsages[0x1E+n-1] = fmt.Sprint(n)
		keyUsages[0x59+n-1] = fmt.Sprintf("kp-%d", n)
	}
	for n := uint32(1); n <= 12; n++ {
		keyUsages[0x3A+n-1] = fmt.Sprintf("f%d", n)
	}
	for n := uint32(13); n <= 24; n++ {
		keyUsages[0x68+n-13] = fmt.Sprintf("f%d", n)
	}
}

var consumerUsages = map[uint32]string{
	0xCD: "play", 0xB5: "next", 0xB6: "prev", 0xB7: "stop", 0xE2: "mute",
	0xE9: "volume-up", 0xEA: "volume-down", 0x6F: "brightness-up", 0x70: "brightness-down",
}

var buttonUsages = map[uint32]string{1: "click", 2: "right-click", 3: "middle-click"}

// Tracker turns one HID device's inputs into the combos the pad sent, in the
// pad's action syntax, so a press can be matched to the slot that sends it.
type Tracker struct {
	held map[string]bool
}

// Feed takes one input and returns the combo when a key (not a modifier),
// media control or mouse button goes down.
func (t *Tracker) Feed(in HIDInput) (string, bool) {
	var name string
	var ok bool
	switch in.Page {
	case pageKeyboard:
		if name, ok = modifierUsages[in.Usage]; !ok {
			name, ok = keyUsages[in.Usage]
		}
	case pageConsumer:
		name, ok = consumerUsages[in.Usage]
	case pageButton:
		name, ok = buttonUsages[in.Usage]
	}
	if !ok {
		return "", false
	}
	return t.FeedKey(name, in.Down)
}

// FeedKey is Feed for a key given by its pad name ("ctrl", "a", "kp-1",
// "volume-up", "right-click"); macOS spellings ("[", "return") resolve through
// aliases. Unknown names are ignored.
func (t *Tracker) FeedKey(name string, down bool) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if a, ok := aliases[name]; ok {
		name = a
	}
	if hasName(modifierUsages, name) {
		if t.held == nil {
			t.held = map[string]bool{}
		}
		t.held[name] = down
		return "", false
	}
	if !down {
		return "", false
	}
	switch {
	case hasName(keyUsages, name):
		var parts []string
		for _, mod := range modifierOrder {
			if t.held[mod] {
				parts = append(parts, mod)
			}
		}
		return strings.Join(append(parts, name), "+"), true
	case hasName(consumerUsages, name), hasName(buttonUsages, name):
		return name, true
	}
	return "", false
}

// hasName reports whether a usage table has name as a value.
func hasName(table map[uint32]string, name string) bool {
	for _, n := range table {
		if n == name {
			return true
		}
	}
	return false
}

var aliases = map[string]string{
	"control": "ctrl", "option": "opt", "alt": "opt", "command": "cmd", "win": "cmd", "super": "cmd",
	"meta": "cmd", "gui": "cmd", "rcontrol": "rctrl", "roption": "ropt", "ralt": "ropt",
	"rcommand": "rcmd", "rwin": "rcmd",
	"return": "enter", "escape": "esc", "spacebar": "space", "-": "minus", "=": "equal",
	"[": "lbracket", "]": "rbracket", "\\": "backslash", ";": "semicolon", "'": "quote",
	"`": "grave", "backtick": "grave", ",": "comma", ".": "period", "dot": "period", "/": "slash",
	"pgup": "pageup", "pgdn": "pagedown", "ins": "insert", "prtsc": "printscreen",
	"arrow-left": "left", "arrow-right": "right", "arrow-up": "up", "arrow-down": "down",
	"play-pause": "play", "previous": "prev", "vol-up": "volume-up", "vol-down": "volume-down",
}

// normalize rewrites the first combo of a pad action the way Tracker writes
// combos: lower case, aliases resolved, modifiers in modifierOrder.
func normalize(action string) string {
	first, _, _ := strings.Cut(action, ",")
	parts := strings.Split(strings.ToLower(strings.TrimSpace(first)), "+")
	held := map[string]bool{}
	key := ""
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if a, ok := aliases[p]; ok {
			p = a
		}
		if i == len(parts)-1 {
			key = p
		} else {
			held[p] = true
		}
	}
	var out []string
	for _, mod := range modifierOrder {
		if held[mod] {
			out = append(out, mod)
		}
	}
	return strings.Join(append(out, key), "+")
}

// Matches reports whether a combo from Tracker is what a slot holding this pad
// action sends first.
func Matches(action, combo string) bool {
	return strings.TrimSpace(action) != "" && normalize(action) == combo
}

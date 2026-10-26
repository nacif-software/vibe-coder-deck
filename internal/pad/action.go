package pad

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Kind is what a slot sends: keystrokes, a media key or a mouse action.
type Kind int

const (
	KindKeys  Kind = iota // one combo or a sequence of up to MaxSteps combos
	KindMedia             // a consumer usage such as volume-up
	KindMouse             // a click or a scroll, with modifiers
)

// MaxSteps is how many combos the pad plays in a sequence.
const MaxSteps = 5

// Keystroke is one combo: HID modifier bits and one keyboard usage (0: none).
type Keystroke struct{ Mods, Code byte }

// Mouse is a click (buttons: 1 left, 2 right, 4 middle) or a wheel step
// (1 up, -1 down), with modifier bits.
type Mouse struct {
	Buttons byte
	Wheel   int8
	Mods    byte
}

// Action is a slot's action in the pad's own terms, parsed from the action
// syntax ("cmd+shift+4", "cmd+a, cmd+c", "volume-up", "ctrl+scroll-up").
type Action struct {
	Kind  Kind
	Steps []Keystroke // KindKeys
	Media uint16      // KindMedia
	Mouse Mouse       // KindMouse
}

// HID keyboard modifier bits.
var modifierBits = map[string]byte{
	"ctrl": 0x01, "shift": 0x02, "opt": 0x04, "cmd": 0x08,
	"rctrl": 0x10, "rshift": 0x20, "ropt": 0x40, "rcmd": 0x80,
}

var mouseActions = map[string]Mouse{
	"click": {Buttons: 1}, "right-click": {Buttons: 2}, "middle-click": {Buttons: 4},
	"scroll-up": {Wheel: 1}, "scroll-down": {Wheel: -1},
}

var mouseAliases = map[string]string{"left-click": "click", "wheel-up": "scroll-up", "wheel-down": "scroll-down"}

// Names that mean different keys on macOS and PC keyboards.
var ambiguous = map[string][]string{
	"delete": {"backspace", "forward-delete"},
	"del":    {"backspace", "forward-delete"},
}

// name → code, inverted from the usage → name tables of hid.go once they are
// complete (their init may run after this file's).
var (
	codesOnce  sync.Once
	keyCodes   map[string]byte
	mediaCodes map[string]uint16
)

func codes() (map[string]byte, map[string]uint16) {
	codesOnce.Do(func() {
		keyCodes = make(map[string]byte, len(keyUsages))
		for usage, name := range keyUsages {
			keyCodes[name] = byte(usage)
		}
		mediaCodes = make(map[string]uint16, len(consumerUsages))
		for usage, name := range consumerUsages {
			mediaCodes[name] = uint16(usage)
		}
	})
	return keyCodes, mediaCodes
}

// ParseAction reads an action in the pad syntax: combos of modifiers and at
// most one key, up to MaxSteps of them separated by commas, or a media key
// alone, or a mouse action with modifiers.
func ParseAction(text string) (Action, error) {
	raw := strings.Split(text, ",")
	steps := make([]string, len(raw))
	any := false
	for i, s := range raw {
		steps[i] = strings.TrimSpace(s)
		any = any || steps[i] != ""
	}
	if !any {
		return Action{}, errors.New("action is empty")
	}
	for _, s := range steps {
		if s == "" {
			return Action{}, fmt.Errorf("empty step in sequence %q", strings.TrimSpace(text))
		}
	}
	if len(steps) > MaxSteps {
		return Action{}, fmt.Errorf("sequence has %d steps; the device plays at most %d", len(steps), MaxSteps)
	}
	parsed := make([]Action, len(steps))
	for i, s := range steps {
		a, err := parseStep(s)
		if err != nil {
			return Action{}, err
		}
		parsed[i] = a
	}
	if len(parsed) == 1 {
		return parsed[0], nil
	}
	seq := Action{Kind: KindKeys}
	for _, a := range parsed {
		if a.Kind != KindKeys {
			return Action{}, errors.New("media and mouse actions cannot be part of a sequence")
		}
		seq.Steps = append(seq.Steps, a.Steps[0])
	}
	return seq, nil
}

func parseStep(step string) (Action, error) {
	var mods byte
	var others []string
	for _, t := range strings.Split(strings.ToLower(step), "+") {
		t = strings.TrimSpace(t)
		if t == "" {
			return Action{}, fmt.Errorf("empty key name in %q ('+' is a separator, use shift+equal)", step)
		}
		name := t
		if a, ok := aliases[t]; ok {
			name = a
		}
		if bit, ok := modifierBits[name]; ok {
			mods |= bit
			continue
		}
		others = append(others, t)
	}
	if len(others) > 1 {
		return Action{}, fmt.Errorf("only one regular key per step, got %v in %q", others, step)
	}
	if len(others) == 0 {
		return Action{Kind: KindKeys, Steps: []Keystroke{{Mods: mods}}}, nil
	}
	token := others[0]
	name := token
	if a, ok := aliases[token]; ok {
		name = a
	}
	keys, media := codes()
	if code, ok := media[name]; ok {
		if mods != 0 {
			return Action{}, fmt.Errorf("media key %q cannot take modifiers", name)
		}
		return Action{Kind: KindMedia, Media: code}, nil
	}
	mouseName := token
	if a, ok := mouseAliases[token]; ok {
		mouseName = a
	}
	if m, ok := mouseActions[mouseName]; ok {
		m.Mods = mods
		return Action{Kind: KindMouse, Mouse: m}, nil
	}
	if options, ok := ambiguous[token]; ok {
		return Action{}, fmt.Errorf("%q is ambiguous on macOS; use one of %s", token, strings.Join(options, " or "))
	}
	if code, ok := keys[name]; ok {
		return Action{Kind: KindKeys, Steps: []Keystroke{{Mods: mods, Code: code}}}, nil
	}
	return Action{}, fmt.Errorf("unknown key %q", token)
}

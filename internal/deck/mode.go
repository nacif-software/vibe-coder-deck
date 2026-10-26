package deck

import "fmt"

// Mode says who does the work when a slot is pressed.
type Mode string

const (
	// ModeOff clears the slot on the pad.
	ModeOff Mode = "off"
	// ModeKeys makes the pad itself send a macropad action (combo, sequence, media, mouse).
	ModeKeys Mode = "keys"
	// ModeScript runs a script file on this machine.
	ModeScript Mode = "script"
	// ModeMacro plays keystroke, text and delay steps on this machine.
	ModeMacro Mode = "macro"
	// ModeShell runs a shell command line on this machine.
	ModeShell Mode = "shell"
)

var modes = []Mode{ModeOff, ModeKeys, ModeScript, ModeMacro, ModeShell}

func ParseMode(s string) (Mode, error) {
	for _, m := range modes {
		if string(m) == s {
			return m, nil
		}
	}
	return "", fmt.Errorf("unknown mode %q (use off, keys, script, macro, shell)", s)
}

// Dispatched is true when the pad sends the slot's hotkey and this program
// does the work.
func (m Mode) Dispatched() bool {
	return m == ModeScript || m == ModeMacro || m == ModeShell
}

func (m Mode) NeedsAction() bool {
	return m == ModeKeys || m == ModeScript || m == ModeShell
}

func (m Mode) Label() string {
	switch m {
	case ModeOff:
		return "Off"
	case ModeKeys:
		return "Keys (the pad sends them)"
	case ModeScript:
		return "Script file"
	case ModeMacro:
		return "Macro (keystrokes)"
	case ModeShell:
		return "Shell command"
	}
	return string(m)
}

// ModeInfo is what the UI needs to offer a mode.
type ModeInfo struct {
	Value       Mode   `json:"value"`
	Label       string `json:"label"`
	NeedsAction bool   `json:"needs_action"`
	Dispatched  bool   `json:"dispatched"`
}

func Modes() []ModeInfo {
	out := make([]ModeInfo, 0, len(modes))
	for _, m := range modes {
		out = append(out, ModeInfo{Value: m, Label: m.Label(), NeedsAction: m.NeedsAction(), Dispatched: m.Dispatched()})
	}
	return out
}

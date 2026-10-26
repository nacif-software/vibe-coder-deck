package deck

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

// Step is one action of a macro: keys (pad syntax, comma-separated combos),
// text (typed as is), delay (milliseconds), or what the recorder captures:
// keydown / keyup (one key name) and click (left, right or middle, at the
// pointer).
type Step struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

var stepTypes = map[string]bool{"keys": true, "text": true, "delay": true, "keydown": true, "keyup": true, "click": true, "launch": true}

var mouseButtons = map[string]bool{"left": true, "right": true, "middle": true}

// UnmarshalJSON accepts a number as value, which is how nehacloud stored delays.
func (s *Step) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Type = raw.Type
	var str string
	if err := json.Unmarshal(raw.Value, &str); err == nil {
		s.Value = str
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(raw.Value, &num); err == nil {
		s.Value = num.String()
		return nil
	}
	return fmt.Errorf("step value must be a string or a number")
}

// Key is what one slot does.
type Key struct {
	ProfileID       int64     `json:"profile_id"` // 0 when saving: the persistent profile
	Slot            Slot      `json:"slot"`
	Label           string    `json:"label"`
	Icon            string    `json:"icon"`
	Mode            Mode      `json:"mode"`
	Action          string    `json:"action"`
	Steps           []Step    `json:"steps"`
	MacroID         int64     `json:"macro_id"` // macro mode: the library macro it plays
	AppScope        string    `json:"app_scope"`
	CooldownSeconds int       `json:"cooldown_seconds"`
	SingleInstance  bool      `json:"single_instance"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Hotkey is the keystroke the pad sends when the slot is dispatched.
func (k Key) Hotkey(modifiers string) string {
	return modifiers + "+" + strconv.Itoa(k.Slot.Digit())
}

// PadAction is what to write into the slot; ok=false clears it.
func (k Key) PadAction(modifiers string) (string, bool) {
	switch k.Mode {
	case ModeOff:
		return "", false
	case ModeKeys:
		return k.Action, true
	default:
		return k.Hotkey(modifiers), true
	}
}

// SlotsSending lists the slots whose pad action starts with combo, the
// keystroke the pad just sent; it is how a press on the pad is traced back
// to its key.
func SlotsSending(keys []Key, modifiers, combo string) []Slot {
	var out []Slot
	for _, k := range keys {
		if action, ok := k.PadAction(modifiers); ok && pad.Matches(action, combo) {
			out = append(out, k.Slot)
		}
	}
	return out
}

func (k Key) Validate() error {
	if _, err := ParseSlot(string(k.Slot)); err != nil {
		return err
	}
	if _, err := ParseMode(string(k.Mode)); err != nil {
		return err
	}
	if k.Mode.NeedsAction() && strings.TrimSpace(k.Action) == "" {
		return fmt.Errorf("action: %s needs one", k.Mode.Label())
	}
	if k.Mode == ModeMacro && k.MacroID == 0 && len(k.Steps) == 0 {
		return fmt.Errorf("macro: pick a macro from the library")
	}
	if err := validateSteps("steps", k.Steps); err != nil {
		return err
	}
	if k.CooldownSeconds < 0 || k.CooldownSeconds > 86400 {
		return fmt.Errorf("cooldown_seconds: must be between 0 and 86400")
	}
	if len(k.Label) > 60 {
		return fmt.Errorf("label: at most 60 characters")
	}
	if err := validateIcon(k.Icon); err != nil {
		return err
	}
	return nil
}

// Run is one press of a dispatched slot.
type Run struct {
	ID         int64
	Slot       Slot
	Status     string
	ExitCode   *int
	Output     string
	StartedAt  time.Time
	FinishedAt time.Time
}

// RunView is the run as the API shows it.
type RunView struct {
	Status     string  `json:"status"`
	ExitCode   *int    `json:"exit_code"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Output     string  `json:"output"`
}

func (r Run) View() RunView {
	v := RunView{Status: r.Status, ExitCode: r.ExitCode, StartedAt: r.StartedAt.Format(time.RFC3339), Output: r.Output}
	if !r.FinishedAt.IsZero() {
		f := r.FinishedAt.Format(time.RFC3339)
		v.FinishedAt = &f
	}
	return v
}

// View is the key as the API and the page show it: the shape nehacloud's
// `macropad:keys --json` has.
type View struct {
	ProfileID       int64         `json:"profile_id"`
	Slot            Slot          `json:"slot"`
	Digit           int           `json:"digit"`
	Name            string        `json:"name"`
	Label           string        `json:"label"`
	Icon            string        `json:"icon"`
	Mode            Mode          `json:"mode"`
	Action          string        `json:"action"`
	Steps           []Step        `json:"steps"`
	MacroID         int64         `json:"macro_id"`
	Macro           *MacroSummary `json:"macro"`
	AppScope        string        `json:"app_scope"`
	CooldownSeconds int           `json:"cooldown_seconds"`
	SingleInstance  bool          `json:"single_instance"`
	Hotkey          string        `json:"hotkey"`
	PadAction       *string       `json:"pad_action"`
	Dispatched      bool          `json:"dispatched"`
	LastRun         *RunView      `json:"last_run"`
}

func (k Key) View(modifiers string, last *Run) View {
	v := View{
		ProfileID: k.ProfileID, Slot: k.Slot, Digit: k.Slot.Digit(), Name: k.Slot.Name(), Label: k.Label, Icon: k.Icon, Mode: k.Mode,
		Action: k.Action, Steps: k.Steps, MacroID: k.MacroID, AppScope: k.AppScope, CooldownSeconds: k.CooldownSeconds,
		SingleInstance: k.SingleInstance, Hotkey: k.Hotkey(modifiers), Dispatched: k.Mode.Dispatched(),
	}
	if v.Steps == nil {
		v.Steps = []Step{}
	}
	if action, ok := k.PadAction(modifiers); ok {
		v.PadAction = &action
	}
	if last != nil {
		rv := last.View()
		v.LastRun = &rv
	}
	return v
}

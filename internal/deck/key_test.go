package deck

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestSlotsInPadOrder(t *testing.T) {
	want := []Slot{Key1, Key2, Key3, Key4, Key5, Key6, KnobLeft, KnobPress, KnobRight}
	got := Slots()
	if len(got) != len(want) {
		t.Fatalf("Slots() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Slots()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for slot, digit := range map[Slot]int{Key1: 1, Key6: 6, KnobLeft: 7, KnobPress: 8, KnobRight: 9} {
		if slot.Digit() != digit {
			t.Errorf("%s.Digit() = %d, want %d", slot, slot.Digit(), digit)
		}
	}
	if KnobLeft.Name() != "Knob left" || Key3.Name() != "Key 3" {
		t.Errorf("names: %q %q", KnobLeft.Name(), Key3.Name())
	}
}

func TestParseSlot(t *testing.T) {
	if s, err := ParseSlot("knob-press"); err != nil || s != KnobPress {
		t.Fatalf("ParseSlot(knob-press) = %q, %v", s, err)
	}
	if _, err := ParseSlot("key9"); err == nil {
		t.Fatal("ParseSlot(key9) should fail")
	}
}

func TestModes(t *testing.T) {
	cases := []struct {
		mode                   Mode
		dispatched, needAction bool
	}{
		{ModeOff, false, false},
		{ModeKeys, false, true},
		{ModeScript, true, true},
		{ModeMacro, true, false},
		{ModeShell, true, true},
	}
	for _, c := range cases {
		if c.mode.Dispatched() != c.dispatched || c.mode.NeedsAction() != c.needAction {
			t.Errorf("%s: dispatched=%v needsAction=%v", c.mode, c.mode.Dispatched(), c.mode.NeedsAction())
		}
		if m, err := ParseMode(string(c.mode)); err != nil || m != c.mode {
			t.Errorf("ParseMode(%s) = %q, %v", c.mode, m, err)
		}
	}
	if _, err := ParseMode("teleport"); err == nil {
		t.Fatal("ParseMode(teleport) should fail")
	}
	if len(Modes()) != 5 || Modes()[0].Value != ModeOff || Modes()[2].Label == "" {
		t.Fatalf("Modes() = %+v", Modes())
	}
}

func TestKeyHotkeyAndPadAction(t *testing.T) {
	script := Key{Slot: Key2, Mode: ModeScript, Action: "/x.sh"}
	if script.Hotkey("ctrl+opt+cmd") != "ctrl+opt+cmd+2" {
		t.Errorf("Hotkey = %q", script.Hotkey("ctrl+opt+cmd"))
	}
	if a, ok := script.PadAction("ctrl+opt+cmd"); !ok || a != "ctrl+opt+cmd+2" {
		t.Errorf("script PadAction = %q, %v", a, ok)
	}
	keys := Key{Slot: Key1, Mode: ModeKeys, Action: "esc"}
	if a, ok := keys.PadAction("ctrl+opt+cmd"); !ok || a != "esc" {
		t.Errorf("keys PadAction = %q, %v", a, ok)
	}
	off := Key{Slot: Key3, Mode: ModeOff}
	if _, ok := off.PadAction("ctrl+opt+cmd"); ok {
		t.Error("off PadAction should clear the slot")
	}
}

func TestKeyValidate(t *testing.T) {
	if err := (Key{Slot: Key1, Mode: ModeScript}).Validate(); err == nil {
		t.Error("script without action should fail")
	}
	if err := (Key{Slot: Key1, Mode: ModeMacro}).Validate(); err == nil {
		t.Error("macro without steps should fail")
	}
	if err := (Key{Slot: Key1, Mode: ModeMacro, Steps: []Step{{Type: "teleport", Value: "x"}}}).Validate(); err == nil {
		t.Error("unknown step type should fail")
	}
	if err := (Key{Slot: Key1, Mode: ModeShell, Action: "echo", CooldownSeconds: -1}).Validate(); err == nil {
		t.Error("negative cooldown should fail")
	}
	good := Key{Slot: Key1, Mode: ModeMacro, Steps: []Step{{Type: "keys", Value: "cmd+shift+4"}, {Type: "delay", Value: "300"}}}
	if err := good.Validate(); err != nil {
		t.Errorf("valid macro rejected: %v", err)
	}
	if err := (Key{Slot: Key1, Mode: ModeOff}).Validate(); err != nil {
		t.Errorf("off rejected: %v", err)
	}
}

func TestStepValueAcceptsNumbers(t *testing.T) {
	var steps []Step
	if err := json.Unmarshal([]byte(`[{"type":"delay","value":200},{"type":"text","value":"hi"}]`), &steps); err != nil {
		t.Fatal(err)
	}
	if steps[0].Value != "200" || steps[1].Value != "hi" {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestKeyValidateRecordedSteps(t *testing.T) {
	recorded := Key{Slot: Key1, Mode: ModeMacro, Steps: []Step{
		{Type: "keydown", Value: "cmd"}, {Type: "delay", Value: "120"}, {Type: "keydown", Value: "4"},
		{Type: "keyup", Value: "4"}, {Type: "keyup", Value: "cmd"}, {Type: "click", Value: "left"},
	}}
	if err := recorded.Validate(); err != nil {
		t.Fatalf("recorded steps rejected: %v", err)
	}
	if err := (Key{Slot: Key1, Mode: ModeMacro, Steps: []Step{{Type: "keydown", Value: ""}}}).Validate(); err == nil {
		t.Error("keydown without a key should fail")
	}
	if err := (Key{Slot: Key1, Mode: ModeMacro, Steps: []Step{{Type: "click", Value: "sideways"}}}).Validate(); err == nil {
		t.Error("unknown mouse button should fail")
	}
}

func TestSlotsSending(t *testing.T) {
	keys := []Key{
		{Slot: Key1, Mode: ModeScript, Action: "/x.sh"},
		{Slot: Key2, Mode: ModeKeys, Action: "cmd+shift+4"},
		{Slot: Key3, Mode: ModeKeys, Action: "Esc"},
		{Slot: Key4, Mode: ModeOff},
		{Slot: KnobRight, Mode: ModeKeys, Action: "volume-up"},
	}
	cases := map[string][]Slot{
		"ctrl+opt+cmd+1": {Key1},
		"shift+cmd+4":    {Key2},
		"esc":            {Key3},
		"volume-up":      {KnobRight},
		"ctrl+opt+cmd+4": nil, // key4 is off: the pad sends nothing for it
	}
	for combo, want := range cases {
		got := SlotsSending(keys, "ctrl+opt+cmd", combo)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("SlotsSending(%q) = %v, want %v", combo, got, want)
		}
	}
}

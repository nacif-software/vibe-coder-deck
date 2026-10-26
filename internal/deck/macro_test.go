package deck

import (
	"fmt"
	"testing"
)

func TestMacroValidate(t *testing.T) {
	keys := []Step{{Type: "keys", Value: "cmd+c"}}
	ok := []Macro{
		{Name: "Copy", Kind: MacroOnce, Steps: keys},
		{Name: "Spam", Kind: MacroRepeat, Steps: keys},
		{Name: "Auto", Kind: MacroToggle, Steps: keys},
		{Name: "Hold", Kind: MacroSequence, HoldSteps: keys},
		{Name: "App", Kind: MacroOnce, Steps: []Step{{Type: "launch", Value: "Safari"}}},
		{Name: "Colored", Kind: MacroOnce, Steps: keys, Color: "#ff8800", StandardDelayMs: 50},
	}
	for _, m := range ok {
		if err := m.Validate(); err != nil {
			t.Errorf("%s: %v", m.Name, err)
		}
	}
	bad := []Macro{
		{Name: "", Kind: MacroOnce, Steps: keys},
		{Name: "No steps", Kind: MacroOnce},
		{Name: "Kind", Kind: "forever", Steps: keys},
		{Name: "Sequence empty", Kind: MacroSequence},
		{Name: "Color", Kind: MacroOnce, Steps: keys, Color: "red; x"},
		{Name: "Delay", Kind: MacroOnce, Steps: keys, StandardDelayMs: -1},
		{Name: "Step", Kind: MacroOnce, Steps: []Step{{Type: "teleport", Value: "x"}}},
	}
	for _, m := range bad {
		if err := m.Validate(); err == nil {
			t.Errorf("%q should fail", m.Name)
		}
	}
}

func TestMacroStoreAndAssignment(t *testing.T) {
	s := newStore(t)
	id, err := s.SaveMacro(Macro{Name: "Copy", Kind: MacroOnce, Steps: []Step{{Type: "keys", Value: "cmd+c"}}})
	if err != nil || id == 0 {
		t.Fatalf("save: %d %v", id, err)
	}
	m, err := s.Macro(id)
	if err != nil || m.Name != "Copy" || len(m.Steps) != 1 {
		t.Fatalf("read back = %+v, %v", m, err)
	}
	m.Name = "Copy all"
	if _, err := s.SaveMacro(m); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveKey(Key{Slot: Key3, Mode: ModeMacro, MacroID: id, SingleInstance: true}); err != nil {
		t.Fatal(err)
	}
	views, _ := s.Views("ctrl+opt+cmd")
	v := views[2]
	if v.Macro == nil || v.Macro.Name != "Copy all" || len(v.Steps) != 1 {
		t.Fatalf("key3 view = %+v", v)
	}
	macros, _ := s.Macros()
	if len(macros) != 1 || len(macros[0].AssignedTo) != 1 || macros[0].AssignedTo[0] != Key3 {
		t.Fatalf("macros = %+v", macros)
	}
	if err := s.SaveKey(Key{Slot: Key4, Mode: ModeMacro, MacroID: 999, SingleInstance: true}); err == nil {
		t.Fatal("assigning a missing macro should fail")
	}
	// Deleting a macro frees the keys that used it.
	if err := s.DeleteMacro(id); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.Key(Key3); k.Mode != ModeOff || k.MacroID != 0 {
		t.Fatalf("key3 after delete = %+v", k)
	}
}

func TestSavingKeyStepsMakesAMacro(t *testing.T) {
	s := newStore(t)
	// The pre-library way (MCP set_key with steps): the key gets its own macro.
	err := s.SaveKey(Key{Slot: Key2, Label: "Copy", Mode: ModeMacro, Steps: []Step{{Type: "keys", Value: "cmd+c"}}, SingleInstance: true})
	if err != nil {
		t.Fatal(err)
	}
	k, _ := s.Key(Key2)
	m, err := s.Macro(k.MacroID)
	if err != nil || m.Name != "Copy" || m.Kind != MacroOnce || len(m.Steps) != 1 {
		t.Fatalf("macro = %+v, %v (key %+v)", m, err, k)
	}
}

func TestLegacyMacroKeysMigrate(t *testing.T) {
	s := newStore(t)
	// The pre-library keys table: key5 held its own steps.
	if _, err := s.db.Exec(`INSERT INTO keys (slot, label, mode, steps, updated_at) VALUES ('key5', 'Old', 'macro', '[{"type":"keys","value":"cmd+v"}]', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateMacros(s.db); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.db.QueryRow(`SELECT macro_id FROM keys WHERE slot = 'key5'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	m, err := s.Macro(id)
	if err != nil || m.Name != "Old" || fmt.Sprint(m.Steps) != fmt.Sprint([]Step{{Type: "keys", Value: "cmd+v"}}) {
		t.Fatalf("migrated = %+v, %v", m, err)
	}
	if err := migrateMacros(s.db); err != nil {
		t.Fatal(err)
	}
	if macros, _ := s.Macros(); len(macros) != 1 {
		t.Fatalf("migration ran twice: %d macros", len(macros))
	}
}

func TestStandardDelay(t *testing.T) {
	steps := []Step{{Type: "keydown", Value: "cmd"}, {Type: "delay", Value: "149"}, {Type: "keydown", Value: "c"}, {Type: "delay", Value: "7"}, {Type: "keyup", Value: "c"}}
	got := WithStandardDelay(steps, 50)
	want := []Step{{Type: "keydown", Value: "cmd"}, {Type: "delay", Value: "50"}, {Type: "keydown", Value: "c"}, {Type: "delay", Value: "50"}, {Type: "keyup", Value: "c"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v", got)
	}
	if fmt.Sprint(WithStandardDelay(steps, 0)) != fmt.Sprint(steps) {
		t.Fatal("0 should keep the recorded delays")
	}
}

// Package deck holds what each control of the vibe-coder-keyboard does and
// decides whether a press may run.
package deck

import "fmt"

// Slot is one of the nine controls of the pad.
type Slot string

const (
	Key1      Slot = "key1"
	Key2      Slot = "key2"
	Key3      Slot = "key3"
	Key4      Slot = "key4"
	Key5      Slot = "key5"
	Key6      Slot = "key6"
	KnobLeft  Slot = "knob-left"
	KnobPress Slot = "knob-press"
	KnobRight Slot = "knob-right"
)

var slots = []Slot{Key1, Key2, Key3, Key4, Key5, Key6, KnobLeft, KnobPress, KnobRight}

// Slots lists every control in pad order.
func Slots() []Slot {
	out := make([]Slot, len(slots))
	copy(out, slots)
	return out
}

func ParseSlot(s string) (Slot, error) {
	for _, slot := range slots {
		if string(slot) == s {
			return slot, nil
		}
	}
	return "", fmt.Errorf("unknown slot %q (use key1..key6, knob-left, knob-press, knob-right)", s)
}

// Digit is the slot's position on the pad and the digit of the hotkey a
// dispatched slot sends: key1..key6 = 1..6, knob left/press/right = 7/8/9.
func (s Slot) Digit() int {
	for i, slot := range slots {
		if slot == s {
			return i + 1
		}
	}
	return 0
}

func (s Slot) Name() string {
	switch s {
	case KnobLeft:
		return "Knob left"
	case KnobPress:
		return "Knob press"
	case KnobRight:
		return "Knob right"
	default:
		return fmt.Sprintf("Key %d", s.Digit())
	}
}

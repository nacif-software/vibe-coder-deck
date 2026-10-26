package deck

import (
	"fmt"

	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

// Op is one write to the pad; an empty Action clears the slot.
type Op struct {
	Slot   Slot   `json:"slot"`
	Action string `json:"action"`
}

func (o Op) String() string {
	if o.Action == "" {
		return "clear " + string(o.Slot)
	}
	return "set " + string(o.Slot) + " " + o.Action
}

// Plan is what a sync writes, in pad order.
type Plan []Op

// Sync writes every slot into the pad from the configuration: keys slots get
// their action, dispatched slots their hotkey, off slots are cleared. It stops
// at the first failure and only records the sync when everything was written.
func Sync(s *Store, w pad.Writer, modifiers string, dryRun bool) (Plan, error) {
	plan, err := PadPlan(s, modifiers)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return plan, nil
	}
	for _, op := range plan {
		var err error
		if op.Action == "" {
			err = w.Clear(string(op.Slot))
		} else {
			err = w.Set(string(op.Slot), op.Action)
		}
		if err != nil {
			return plan, fmt.Errorf("%s: %w", op, err)
		}
	}
	return plan, s.MarkSynced(s.Now())
}

// PadPlan is what the pad should hold. With profile switching on, every slot
// sends its hotkey and this program does what the active profile says, keys
// slots included; otherwise the persistent profile is written as it is.
func PadPlan(s *Store, modifiers string) (Plan, error) {
	keys, err := s.Keys()
	if err != nil {
		return nil, err
	}
	switching, err := s.ProfileSwitching()
	if err != nil {
		return nil, err
	}
	plan := make(Plan, 0, len(keys))
	for _, k := range keys {
		action, _ := k.PadAction(modifiers)
		if switching {
			action = k.Hotkey(modifiers)
		}
		plan = append(plan, Op{Slot: k.Slot, Action: action})
	}
	return plan, nil
}

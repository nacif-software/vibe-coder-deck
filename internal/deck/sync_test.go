package deck

import (
	"errors"
	"strconv"
	"testing"

	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

type fakeWriter struct {
	ops    []string
	failOn string
	err    error
}

func (w *fakeWriter) Set(slot, action string) error {
	if slot == w.failOn {
		return w.err
	}
	w.ops = append(w.ops, "set "+slot+" "+action)
	return nil
}

func (w *fakeWriter) Clear(slot string) error {
	if slot == w.failOn {
		return w.err
	}
	w.ops = append(w.ops, "clear "+slot)
	return nil
}

func TestSyncWritesEverySlot(t *testing.T) {
	s := newStore(t)
	s.SaveKey(Key{Slot: Key1, Mode: ModeScript, Action: "/h.sh"})
	s.SaveKey(Key{Slot: Key2, Mode: ModeKeys, Action: "cmd+shift+4"})
	w := &fakeWriter{}
	plan, err := Sync(s, w, "ctrl+opt+cmd", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 9 || plan[0].String() != "set key1 ctrl+opt+cmd+1" || plan[1].String() != "set key2 cmd+shift+4" || plan[2].String() != "clear key3" {
		t.Fatalf("plan = %v", plan)
	}
	if len(w.ops) != 9 {
		t.Fatalf("ops = %v", w.ops)
	}
	if _, ok, _ := s.SyncedAt(); !ok {
		t.Fatal("sync must record synced_at")
	}
}

func TestSyncDryRunTouchesNothing(t *testing.T) {
	s := newStore(t)
	w := &fakeWriter{}
	plan, err := Sync(s, w, "ctrl+opt+cmd", true)
	if err != nil || len(plan) != 9 || len(w.ops) != 0 {
		t.Fatalf("plan=%v ops=%v err=%v", plan, w.ops, err)
	}
	if _, ok, _ := s.SyncedAt(); ok {
		t.Fatal("dry run must not record synced_at")
	}
}

func TestSyncStopsWhenPadIsNotConnected(t *testing.T) {
	s := newStore(t)
	w := &fakeWriter{failOn: "key2", err: pad.ErrNotConnected}
	_, err := Sync(s, w, "ctrl+opt+cmd", false)
	if !errors.Is(err, pad.ErrNotConnected) {
		t.Fatalf("err = %v", err)
	}
	if len(w.ops) != 1 {
		t.Fatalf("ops = %v (must stop at the first failure)", w.ops)
	}
	if _, ok, _ := s.SyncedAt(); ok {
		t.Fatal("failed sync must not record synced_at")
	}
}

func TestSyncWithSwitchingWritesHotkeysOnly(t *testing.T) {
	s := newStore(t)
	s.SaveKey(Key{Slot: Key2, Mode: ModeKeys, Action: "cmd+shift+4", SingleInstance: true})
	s.SetProfileSwitching(true)
	plan, err := Sync(s, nil, "ctrl+opt+cmd", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range plan {
		if op.Action != "ctrl+opt+cmd+"+strconv.Itoa(op.Slot.Digit()) {
			t.Fatalf("%s = %q, want its hotkey", op.Slot, op.Action)
		}
	}
}

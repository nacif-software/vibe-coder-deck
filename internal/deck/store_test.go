package deck

import (
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestKeysEnsuresNineInPadOrder(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 2; i++ {
		keys, err := s.Keys()
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) != 9 || keys[0].Slot != Key1 || keys[8].Slot != KnobRight {
			t.Fatalf("keys = %+v", keys)
		}
		if keys[0].Mode != ModeOff || !keys[0].SingleInstance {
			t.Fatalf("defaults = %+v", keys[0])
		}
	}
}

func TestSaveKeyRoundTrip(t *testing.T) {
	s := newStore(t)
	in := Key{Slot: Key1, Label: "Handoff", Mode: ModeScript, Action: "/h.sh", AppScope: "Orca", CooldownSeconds: 60, SingleInstance: true}
	if err := s.SaveKey(in); err != nil {
		t.Fatal(err)
	}
	out, err := s.Key(Key1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Label != "Handoff" || out.Mode != ModeScript || out.Action != "/h.sh" || out.AppScope != "Orca" || out.CooldownSeconds != 60 || !out.SingleInstance {
		t.Fatalf("round trip = %+v", out)
	}
	if out.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not set")
	}
	changed, err := s.ChangedAt()
	if err != nil || changed.Before(out.UpdatedAt) {
		t.Fatalf("ChangedAt = %v, %v (updated %v)", changed, err, out.UpdatedAt)
	}

	macro := Key{Slot: KnobPress, Mode: ModeMacro, Steps: []Step{{Type: "keys", Value: "cmd+a"}, {Type: "delay", Value: "200"}}}
	if err := s.SaveKey(macro); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Key(KnobPress)
	if len(got.Steps) != 2 || got.Steps[1].Value != "200" {
		t.Fatalf("steps = %+v", got.Steps)
	}
}

func TestSaveKeyValidates(t *testing.T) {
	s := newStore(t)
	if err := s.SaveKey(Key{Slot: Key1, Mode: ModeScript}); err == nil {
		t.Fatal("expected validation error")
	}
	k, _ := s.Key(Key1)
	if k.Mode != ModeOff {
		t.Fatalf("invalid save changed the row: %+v", k)
	}
}

func TestRuns(t *testing.T) {
	s := newStore(t)
	run, err := s.CreateRun(Key1)
	if err != nil || run.ID == 0 || run.Status != "running" || run.StartedAt.IsZero() {
		t.Fatalf("CreateRun = %+v, %v", run, err)
	}
	zero := 0
	if err := s.FinishRun(run.ID, "ok", &zero, "done"); err != nil {
		t.Fatal(err)
	}
	last, err := s.LastRun(Key1)
	if err != nil || last == nil || last.Status != "ok" || last.ExitCode == nil || *last.ExitCode != 0 || last.Output != "done" || last.FinishedAt.IsZero() {
		t.Fatalf("LastRun = %+v, %v", last, err)
	}
	if none, _ := s.LastRun(Key2); none != nil {
		t.Fatal("key2 has no runs")
	}
	runs, _ := s.Runs(Key1, 10)
	if len(runs) != 1 {
		t.Fatalf("Runs = %+v", runs)
	}
}

func TestCooldownAndSynced(t *testing.T) {
	s := newStore(t)
	if until, _ := s.CooldownUntil(Key1); !until.IsZero() {
		t.Fatal("fresh cooldown should be zero")
	}
	at := time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC)
	if err := s.SetCooldownUntil(Key1, at); err != nil {
		t.Fatal(err)
	}
	if until, _ := s.CooldownUntil(Key1); !until.Equal(at) {
		t.Fatalf("CooldownUntil = %v", until)
	}
	if _, ok, _ := s.SyncedAt(); ok {
		t.Fatal("not synced yet")
	}
	if err := s.MarkSynced(at); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := s.SyncedAt(); !ok || !got.Equal(at) {
		t.Fatalf("SyncedAt = %v, %v", got, ok)
	}
}

func TestDebugSettingPersists(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if on, err := s.Debug(); err != nil || on {
		t.Fatalf("default debug = %v, %v", on, err)
	}
	if err := s.SetDebug(true); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.Debug(); !on {
		t.Fatal("debug not saved")
	}
}

package deck

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func newGate(t *testing.T) (*Gate, *Store, *clock) {
	t.Helper()
	s := newStore(t)
	c := &clock{t: time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC)}
	g := NewGate(s)
	g.Now = c.Now
	return g, s, c
}

func scriptKey(t *testing.T, s *Store, single bool, cooldown int) Key {
	t.Helper()
	k := Key{Slot: Key1, Mode: ModeScript, Action: "/x.sh", SingleInstance: single, CooldownSeconds: cooldown}
	if err := s.SaveKey(k); err != nil {
		t.Fatal(err)
	}
	k, _ = s.Key(Key1)
	return k
}

func TestGateGrantsAndRecords(t *testing.T) {
	g, s, _ := newGate(t)
	d, err := g.Acquire(scriptKey(t, s, true, 0))
	if err != nil || !d.Granted || d.Run.ID == 0 || d.Run.Status != "running" {
		t.Fatalf("Acquire = %+v, %v", d, err)
	}
}

func TestGateRefusesWhileRunning(t *testing.T) {
	g, s, _ := newGate(t)
	k := scriptKey(t, s, true, 0)
	g.Acquire(k)
	d, _ := g.Acquire(k)
	if d.Granted || d.Reason != "running" {
		t.Fatalf("second = %+v", d)
	}
	if runs, _ := s.Runs(Key1, 10); len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
}

func TestGateGrantsAgainAfterRelease(t *testing.T) {
	g, s, _ := newGate(t)
	k := scriptKey(t, s, true, 0)
	first, _ := g.Acquire(k)
	if err := g.Release(first.Run, 0, "done"); err != nil {
		t.Fatal(err)
	}
	second, _ := g.Acquire(k)
	if !second.Granted {
		t.Fatalf("second = %+v", second)
	}
	run, _ := s.LastRun(Key1)
	runs, _ := s.Runs(Key1, 10)
	if len(runs) != 2 || runs[1].Status != "ok" || *runs[1].ExitCode != 0 || runs[1].Output != "done" || runs[1].FinishedAt.IsZero() || run.Status != "running" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestGateMarksNonZeroExitFailed(t *testing.T) {
	g, s, _ := newGate(t)
	d, _ := g.Acquire(scriptKey(t, s, true, 0))
	g.Release(d.Run, 2, "boom")
	run, _ := s.LastRun(Key1)
	if run.Status != "failed" || *run.ExitCode != 2 {
		t.Fatalf("run = %+v", run)
	}
}

func TestGateCooldownRefusesAfterFinish(t *testing.T) {
	g, s, c := newGate(t)
	k := scriptKey(t, s, false, 60)
	first, _ := g.Acquire(k)
	g.Release(first.Run, 0, "")
	if d, _ := g.Acquire(k); d.Granted || d.Reason != "cooldown" {
		t.Fatalf("inside cooldown = %+v", d)
	}
	c.t = c.t.Add(61 * time.Second)
	if d, _ := g.Acquire(k); !d.Granted {
		t.Fatalf("after cooldown = %+v", d)
	}
}

func TestGateCooldownRefusalDoesNotHoldLock(t *testing.T) {
	g, s, c := newGate(t)
	k := scriptKey(t, s, true, 60)
	first, _ := g.Acquire(k)
	g.Release(first.Run, 0, "")
	if d, _ := g.Acquire(k); d.Reason != "cooldown" {
		t.Fatalf("refused = %+v", d)
	}
	c.t = c.t.Add(61 * time.Second)
	if d, _ := g.Acquire(k); !d.Granted {
		t.Fatalf("lock leaked: %+v", d)
	}
}

func TestGateCooldownSurvivesRestart(t *testing.T) {
	g, s, c := newGate(t)
	k := scriptKey(t, s, false, 60)
	first, _ := g.Acquire(k)
	g.Release(first.Run, 0, "")
	fresh := NewGate(s)
	fresh.Now = c.Now
	if d, _ := fresh.Acquire(k); d.Granted || d.Reason != "cooldown" {
		t.Fatalf("after restart = %+v", d)
	}
}

func TestGateAllowsOverlapWithoutSingleInstance(t *testing.T) {
	g, s, _ := newGate(t)
	k := scriptKey(t, s, false, 0)
	a, _ := g.Acquire(k)
	b, _ := g.Acquire(k)
	if !a.Granted || !b.Granted {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}

func TestGateRefusesOffAndPadNativeKeys(t *testing.T) {
	g, s, _ := newGate(t)
	off, _ := s.Key(Key1)
	if d, _ := g.Acquire(off); d.Granted || d.Reason != "off" {
		t.Fatalf("off = %+v", d)
	}
	s.SaveKey(Key{Slot: Key2, Mode: ModeKeys, Action: "esc"})
	keys, _ := s.Key(Key2)
	if d, _ := g.Acquire(keys); d.Granted || d.Reason != "not-dispatched" {
		t.Fatalf("keys = %+v", d)
	}
	if runs, _ := s.Runs(Key1, 10); len(runs) != 0 {
		t.Fatal("refusals must not record runs")
	}
}

func TestGateSkipFreesTheKey(t *testing.T) {
	g, s, _ := newGate(t)
	k := scriptKey(t, s, true, 0)
	d, _ := g.Acquire(k)
	if err := g.Skip(d.Run, "frontmost app: Finder"); err != nil {
		t.Fatal(err)
	}
	run, _ := s.LastRun(Key1)
	if run.Status != "skipped" || run.Output != "frontmost app: Finder" || run.FinishedAt.IsZero() {
		t.Fatalf("run = %+v", run)
	}
	if again, _ := g.Acquire(k); !again.Granted {
		t.Fatalf("after skip = %+v", again)
	}
}

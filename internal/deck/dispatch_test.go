package deck

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu     sync.Mutex
	calls  []string
	exit   int
	output string
}

func (f *fakeRunner) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

// Calls is safe while macros loop on other goroutines.
func (f *fakeRunner) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeRunner) RunScript(path string) (int, string, error) {
	f.record("script:" + path)
	return f.exit, f.output, nil
}

func (f *fakeRunner) SendKeys(action string) error {
	f.record("keys:" + action)
	return nil
}

func (f *fakeRunner) HoldKeys(action string, down bool) error {
	f.record("hold:" + action + ":" + map[bool]string{true: "down", false: "up"}[down])
	return nil
}

func (f *fakeRunner) RunShell(command string) (int, string, error) {
	f.record("shell:" + command)
	return f.exit, f.output, nil
}

func (f *fakeRunner) PlayMacro(ctx context.Context, steps []Step) (int, string, error) {
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		parts = append(parts, s.Type+"="+s.Value)
	}
	f.record("macro:" + strings.Join(parts, ","))
	time.Sleep(2 * time.Millisecond) // like a short macro
	return f.exit, f.output, nil
}

func newDispatcher(t *testing.T, front string) (*Dispatcher, *Store, *fakeRunner) {
	t.Helper()
	s := newStore(t)
	r := &fakeRunner{output: "out"}
	d := &Dispatcher{Store: s, Gate: NewGate(s), Runner: r, Frontmost: func() (string, error) { return front, nil }}
	return d, s, r
}

func TestPressRunsScriptAndFinishes(t *testing.T) {
	d, s, r := newDispatcher(t, "Orca")
	s.SaveKey(Key{Slot: Key1, Mode: ModeScript, Action: "/h.sh", AppScope: "Orca", SingleInstance: true})
	out, err := d.Press(Key1)
	if err != nil || !out.Ran || out.ExitCode != 0 {
		t.Fatalf("Press = %+v, %v", out, err)
	}
	if len(r.Calls()) != 1 || r.Calls()[0] != "script:/h.sh" {
		t.Fatalf("calls = %v", r.Calls())
	}
	run, _ := s.LastRun(Key1)
	if run.Status != "ok" || run.Output != "out" {
		t.Fatalf("run = %+v", run)
	}
}

func TestPressSkipsWhenAnotherAppIsInFront(t *testing.T) {
	d, s, r := newDispatcher(t, "Finder")
	s.SaveKey(Key{Slot: Key1, Mode: ModeScript, Action: "/h.sh", AppScope: "Orca", SingleInstance: true})
	out, _ := d.Press(Key1)
	if out.Ran || out.Reason != "app" || len(r.Calls()) != 0 {
		t.Fatalf("Press = %+v, calls %v", out, r.Calls())
	}
	run, _ := s.LastRun(Key1)
	if run.Status != "skipped" || !strings.Contains(run.Output, "Finder") {
		t.Fatalf("run = %+v", run)
	}
}

func TestPressIgnoresAppWhenScopeEmpty(t *testing.T) {
	d, s, r := newDispatcher(t, "Finder")
	s.SaveKey(Key{Slot: Key2, Mode: ModeShell, Action: "echo hi"})
	out, _ := d.Press(Key2)
	if !out.Ran || r.Calls()[0] != "shell:echo hi" {
		t.Fatalf("Press = %+v, calls %v", out, r.Calls())
	}
}

func TestPressPlaysMacroAndReportsFailure(t *testing.T) {
	d, s, r := newDispatcher(t, "Orca")
	r.exit = 3
	s.SaveKey(Key{Slot: KnobPress, Mode: ModeMacro, Steps: []Step{{Type: "keys", Value: "cmd+a"}, {Type: "delay", Value: "100"}}})
	out, _ := d.Press(KnobPress)
	if !out.Ran || out.ExitCode != 3 || r.Calls()[0] != "macro:keys=cmd+a,delay=100" {
		t.Fatalf("Press = %+v, calls %v", out, r.Calls())
	}
	run, _ := s.LastRun(KnobPress)
	if run.Status != "failed" || *run.ExitCode != 3 {
		t.Fatalf("run = %+v", run)
	}
}

func TestPressReportsGateRefusals(t *testing.T) {
	d, s, _ := newDispatcher(t, "Orca")
	if out, _ := d.Press(Key3); out.Ran || out.Reason != "off" {
		t.Fatalf("off = %+v", out)
	}
	s.SaveKey(Key{Slot: Key1, Mode: ModeScript, Action: "/h.sh", SingleInstance: true, CooldownSeconds: 60})
	d.Press(Key1)
	if out, _ := d.Press(Key1); out.Ran || out.Reason != "cooldown" {
		t.Fatalf("cooldown = %+v", out)
	}
}

func macroKey(t *testing.T, s *Store, slot Slot, m Macro) {
	t.Helper()
	id, err := s.SaveMacro(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveKey(Key{Slot: slot, Mode: ModeMacro, MacroID: id, SingleInstance: true}); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls until cond holds, failing the test after a second.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func wait(t *testing.T, done <-chan Outcome) Outcome {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(time.Second):
		t.Fatal("the run did not end")
		return Outcome{}
	}
}

var copyStep = []Step{{Type: "keys", Value: "cmd+c"}}

func TestOnceMacroPlaysOnceOnDown(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	macroKey(t, s, Key1, Macro{Name: "Copy", Kind: MacroOnce, Steps: copyStep})
	out := wait(t, d.Down(Key1))
	d.Up(Key1)
	if !out.Ran || len(r.Calls()) != 1 || r.Calls()[0] != "macro:keys=cmd+c" {
		t.Fatalf("out %+v calls %v", out, r.Calls())
	}
}

func TestRepeatMacroLoopsWhileHeld(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	macroKey(t, s, Key1, Macro{Name: "Spam", Kind: MacroRepeat, Steps: copyStep})
	done := d.Down(Key1)
	waitFor(t, "three plays", func() bool { return len(r.Calls()) >= 3 })
	d.Up(Key1)
	out := wait(t, done)
	n := len(r.Calls())
	time.Sleep(20 * time.Millisecond)
	if !out.Ran || len(r.Calls()) != n {
		t.Fatalf("kept playing after release: %d → %d", n, len(r.Calls()))
	}
	runs, _ := s.Runs(Key1, 10)
	if len(runs) != 1 || runs[0].Status != "ok" {
		t.Fatalf("one run per hold expected: %+v", runs)
	}
}

func TestToggleMacroStartsAndStopsOnPresses(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	macroKey(t, s, Key1, Macro{Name: "Auto", Kind: MacroToggle, Steps: copyStep})
	done := d.Down(Key1)
	d.Up(Key1) // releasing does not stop a toggle
	waitFor(t, "plays after release", func() bool { return len(r.Calls()) >= 3 })
	if stop := d.Down(Key1); stop != nil {
		t.Fatal("the stopping press should not start a run")
	}
	d.Up(Key1)
	wait(t, done)
	n := len(r.Calls())
	time.Sleep(20 * time.Millisecond)
	if len(r.Calls()) != n {
		t.Fatal("toggle kept playing after the second press")
	}
}

func TestSequenceMacroPressHoldRelease(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	macroKey(t, s, Key1, Macro{Name: "Seq", Kind: MacroSequence,
		Steps: []Step{{Type: "keys", Value: "p"}}, HoldSteps: []Step{{Type: "keys", Value: "h"}}, ReleaseSteps: []Step{{Type: "keys", Value: "r"}}})
	done := d.Down(Key1)
	waitFor(t, "hold plays", func() bool { return len(r.Calls()) >= 3 })
	d.Up(Key1)
	wait(t, done)
	calls := r.Calls()
	if calls[0] != "macro:keys=p" || calls[1] != "macro:keys=h" || calls[len(calls)-1] != "macro:keys=r" {
		t.Fatalf("calls = %v", calls)
	}
	for _, c := range calls[1 : len(calls)-1] {
		if c != "macro:keys=h" {
			t.Fatalf("calls = %v", calls)
		}
	}
}

func TestPressPlaysAnyMacroOnce(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	macroKey(t, s, Key1, Macro{Name: "Spam", Kind: MacroRepeat, Steps: copyStep, StandardDelayMs: 0})
	out, err := d.Press(Key1)
	if err != nil || !out.Ran || len(r.Calls()) != 1 {
		t.Fatalf("out %+v err %v calls %v", out, err, r.Calls())
	}
}

func TestStandardDelayIsApplied(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	macroKey(t, s, Key1, Macro{Name: "Two", Kind: MacroOnce, StandardDelayMs: 50,
		Steps: []Step{{Type: "keys", Value: "a"}, {Type: "delay", Value: "900"}, {Type: "keys", Value: "b"}}})
	d.Press(Key1)
	if got := r.Calls()[0]; got != "macro:keys=a,delay=50,keys=b" {
		t.Fatalf("played %s", got)
	}
}

func TestActiveProfileDrivesTheKeys(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	app, _ := s.AddApp("com.apple.Safari", "Safari", "")
	s.SaveKey(Key{ProfileID: app.ActiveProfileID, Slot: Key1, Mode: ModeShell, Action: "echo safari", SingleInstance: true})
	s.SaveKey(Key{Slot: Key1, Mode: ModeShell, Action: "echo desktop", SingleInstance: true})
	active := app.ActiveProfileID
	d.Profile = func() int64 { return active }
	d.Press(Key1)
	active = 0 // the persistent profile
	d.Press(Key1)
	if calls := r.Calls(); len(calls) != 2 || calls[0] != "shell:echo safari" || calls[1] != "shell:echo desktop" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestKeysModeIsTypedWhenSwitching(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	s.SaveKey(Key{Slot: Key2, Mode: ModeKeys, Action: "cmd+shift+4", SingleInstance: true})
	// Without switching the pad types it itself: nothing to do here.
	if out, _ := d.Press(Key2); out.Ran || len(r.Calls()) != 0 {
		t.Fatalf("out %+v calls %v", out, r.Calls())
	}
	s.SetProfileSwitching(true)
	if out := wait(t, d.Down(Key2)); !out.Ran || len(r.Calls()) != 1 || r.Calls()[0] != "keys:cmd+shift+4" {
		t.Fatalf("out %+v calls %v", out, r.Calls())
	}
}

// A keys slot that is only modifiers ("ropt", "ctrl+shift") is meant to be held:
// with switching on the pad sends the hotkey, so the program holds the
// modifiers from the key going down until it comes up. Press (run now) taps it.
func TestModifierOnlyKeyIsHeldWhileDown(t *testing.T) {
	d, s, r := newDispatcher(t, "")
	s.SaveKey(Key{Slot: Key3, Mode: ModeKeys, Action: "ropt", SingleInstance: true})
	s.SetProfileSwitching(true)
	if out := wait(t, d.Down(Key3)); !out.Ran || len(r.Calls()) != 1 || r.Calls()[0] != "hold:ropt:down" {
		t.Fatalf("down: out %+v calls %v", out, r.Calls())
	}
	d.Up(Key3)
	if calls := r.Calls(); len(calls) != 2 || calls[1] != "hold:ropt:up" {
		t.Fatalf("up: calls %v", calls)
	}
	if out, _ := d.Press(Key3); !out.Ran || len(r.Calls()) != 3 || r.Calls()[2] != "keys:ropt" {
		t.Fatalf("press: out %+v calls %v", out, r.Calls())
	}
	// Up without a Down holds nothing and releases nothing.
	d.Up(Key3)
	if calls := r.Calls(); len(calls) != 3 {
		t.Fatalf("stray up: calls %v", calls)
	}
}

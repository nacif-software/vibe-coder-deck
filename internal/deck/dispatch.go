package deck

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Runner does the work of a dispatched key on this machine.
type Runner interface {
	RunScript(path string) (exitCode int, output string, err error)
	RunShell(command string) (exitCode int, output string, err error)
	// SendKeys types a pad action (a combo, comma-separated combos, a media
	// or mouse action) like a real keyboard: what a keys-mode slot does when
	// profile switching has the pad send hotkeys only.
	SendKeys(action string) error
	// PlayMacro plays steps until they end or ctx is cancelled.
	PlayMacro(ctx context.Context, steps []Step) (exitCode int, output string, err error)
}

// Outcome is what happened to a press.
type Outcome struct {
	Slot     Slot
	Ran      bool
	Reason   string // off, not-dispatched, running, cooldown, app when Ran is false
	Run      *Run
	ExitCode int
	Output   string
}

func (o Outcome) String() string {
	switch {
	case o.Ran:
		return fmt.Sprintf("%s: ran (exit %d)", o.Slot, o.ExitCode)
	case o.Reason == "app":
		return fmt.Sprintf("%s: skipped (%s)", o.Slot, o.Output)
	default:
		return fmt.Sprintf("%s: ignored (%s)", o.Slot, o.Reason)
	}
}

// Dispatcher turns a press into a run: asks the gate, checks the app in
// front, runs, and records the result.
type Dispatcher struct {
	Store     *Store
	Gate      *Gate
	Runner    Runner
	Frontmost func() (string, error)
	Log       func(format string, args ...any)
	// Profile is the active profile (the app in front's); nil or 0 = persistent.
	Profile func() int64

	mu     sync.Mutex
	active map[Slot]*activation // macros looping on a held key or a toggle
}

// activation is a repeat, toggle or sequence macro running on a key.
type activation struct {
	kind   MacroKind
	cancel context.CancelFunc
}

const outputTail = 2000

func (d *Dispatcher) profile() int64 {
	if d.Profile == nil {
		return 0
	}
	return d.Profile()
}

func (d *Dispatcher) key(slot Slot) (Key, error) { return d.Store.KeyIn(d.profile(), slot) }

// typeKeys handles a keys-mode slot: with profile switching the pad sends a
// hotkey and this program types the action; without it the pad types it
// itself and there is nothing to do.
func (d *Dispatcher) typeKeys(k Key) (Outcome, error) {
	if on, _ := d.Store.ProfileSwitching(); !on {
		return Outcome{Slot: k.Slot, Reason: "not-dispatched"}, nil
	}
	d.logf("%s: types %s", k.Slot, k.Action)
	if err := d.Runner.SendKeys(k.Action); err != nil {
		return Outcome{Slot: k.Slot, Ran: true, ExitCode: 1, Output: err.Error()}, nil
	}
	return Outcome{Slot: k.Slot, Ran: true}, nil
}

// Press runs a key once, whatever its macro type: "Run now", press_key, and
// the pad's press of a script, shell or one-shot macro key.
func (d *Dispatcher) Press(slot Slot) (Outcome, error) {
	if k, err := d.key(slot); err == nil && k.Mode == ModeKeys {
		return d.typeKeys(k)
	}
	k, run, out, err := d.begin(slot)
	if run == nil {
		return out, err
	}
	var code int
	var output string
	switch k.Mode {
	case ModeScript:
		code, output, err = d.Runner.RunScript(k.Action)
	case ModeShell:
		code, output, err = d.Runner.RunShell(k.Action)
	case ModeMacro:
		var m Macro
		if m, err = d.Store.Macro(k.MacroID); err == nil {
			steps := append(append(append([]Step{}, m.Steps...), m.HoldSteps...), m.ReleaseSteps...)
			code, output, err = d.Runner.PlayMacro(context.Background(), WithStandardDelay(steps, m.StandardDelayMs))
		}
	}
	return d.end(slot, *run, code, output, err)
}

// Down is the pad's key going down. Script, shell and one-shot macro keys
// run once (like Press); repeat, toggle and sequence macros start playing
// until Up (or, for a toggle, the next Down). The channel gets the outcome
// when the run ends; it is nil when the press only stopped a toggle.
func (d *Dispatcher) Down(slot Slot) <-chan Outcome {
	done := make(chan Outcome, 1)
	k, err := d.key(slot)
	var m Macro
	if err == nil && k.Mode == ModeMacro {
		m, err = d.Store.Macro(k.MacroID)
	}
	if err != nil || k.Mode != ModeMacro || m.Kind == MacroOnce || m.Kind == "" {
		go func() {
			out, err := d.Press(slot)
			if err != nil {
				d.logf("%s: %v", slot, err)
			}
			done <- out
		}()
		return done
	}

	d.mu.Lock()
	if a := d.active[slot]; a != nil {
		if a.kind == MacroToggle {
			a.cancel() // the second press of a toggle stops it
		}
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	k, run, out, err := d.begin(slot)
	if run == nil {
		if err != nil {
			d.logf("%s: %v", slot, err)
		}
		done <- out
		return done
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Lock()
	if d.active == nil {
		d.active = map[Slot]*activation{}
	}
	d.active[slot] = &activation{kind: m.Kind, cancel: cancel}
	d.mu.Unlock()

	go func() {
		code, output, err := d.play(ctx, m)
		cancel()
		d.mu.Lock()
		delete(d.active, slot)
		d.mu.Unlock()
		out, endErr := d.end(slot, *run, code, output, err)
		if endErr != nil {
			d.logf("%s: %v", slot, endErr)
		}
		done <- out
	}()
	return done
}

// Up is the pad's key released: it ends a repeat or the hold of a sequence.
func (d *Dispatcher) Up(slot Slot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if a := d.active[slot]; a != nil && a.kind != MacroToggle {
		a.cancel()
	}
}

// pauseBetweenLoops keeps a macro of instant steps from spinning the CPU.
const pauseBetweenLoops = 10 * time.Millisecond

// play runs a repeat, toggle or sequence macro until ctx is cancelled.
func (d *Dispatcher) play(ctx context.Context, m Macro) (int, string, error) {
	loop := func(steps []Step) (int, string, error) {
		steps = WithStandardDelay(steps, m.StandardDelayMs)
		var code int
		var out string
		var err error
		for ctx.Err() == nil && len(steps) > 0 {
			if code, out, err = d.Runner.PlayMacro(ctx, steps); err != nil || code != 0 {
				return code, out, err
			}
			select {
			case <-ctx.Done():
			case <-time.After(pauseBetweenLoops):
			}
		}
		if len(steps) == 0 {
			<-ctx.Done() // a sequence without hold steps waits for the release
		}
		return code, out, err
	}
	if m.Kind != MacroSequence {
		return loop(m.Steps)
	}
	// Press and release steps always play in full; only the hold loop stops.
	if code, out, err := d.Runner.PlayMacro(context.Background(), WithStandardDelay(m.Steps, m.StandardDelayMs)); err != nil || code != 0 {
		return code, out, err
	}
	if code, out, err := loop(m.HoldSteps); err != nil || code != 0 {
		return code, out, err
	}
	return d.Runner.PlayMacro(context.Background(), WithStandardDelay(m.ReleaseSteps, m.StandardDelayMs))
}

// begin asks the gate and checks the app in front. run is nil when the press
// is ignored or skipped; out then says why.
func (d *Dispatcher) begin(slot Slot) (Key, *Run, Outcome, error) {
	k, err := d.key(slot)
	if err != nil {
		return k, nil, Outcome{Slot: slot}, err
	}
	dec, err := d.Gate.Acquire(k)
	if err != nil {
		return k, nil, Outcome{Slot: slot}, err
	}
	if !dec.Granted {
		d.logf("%s: ignored: %s", slot, dec.Reason)
		return k, nil, Outcome{Slot: slot, Reason: dec.Reason}, nil
	}
	run := dec.Run
	if k.AppScope != "" && d.Frontmost != nil {
		front, err := d.Frontmost()
		if err != nil || front != k.AppScope {
			why := "frontmost app: " + front
			if err != nil {
				why = "frontmost app unknown: " + err.Error()
			}
			d.logf("%s: skipped: %s, not %s", slot, why, k.AppScope)
			if err := d.Gate.Skip(run, why); err != nil {
				return k, nil, Outcome{Slot: slot}, err
			}
			return k, nil, Outcome{Slot: slot, Reason: "app", Run: &run, Output: why}, nil
		}
	}
	d.logf("%s: run %d: %s %s", slot, run.ID, k.Mode, describe(k))
	return k, &run, Outcome{}, nil
}

// end records how a run went and frees the key.
func (d *Dispatcher) end(slot Slot, run Run, code int, out string, err error) (Outcome, error) {
	if err != nil {
		out = strings.TrimSpace(out + "\n" + err.Error())
		if code == 0 {
			code = 1
		}
	}
	if len(out) > outputTail {
		out = out[len(out)-outputTail:]
	}
	if err := d.Gate.Release(run, code, out); err != nil {
		return Outcome{Slot: slot}, err
	}
	d.logf("%s: run %d finished: exit %d", slot, run.ID, code)
	return Outcome{Slot: slot, Ran: true, Run: &run, ExitCode: code, Output: out}, nil
}

func describe(k Key) string {
	if k.Mode == ModeMacro {
		return fmt.Sprintf("macro %d", k.MacroID)
	}
	return k.Action
}

func (d *Dispatcher) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(format, args...)
	}
}

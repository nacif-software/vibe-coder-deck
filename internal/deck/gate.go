package deck

import (
	"sync"
	"time"
)

// Gate decides whether a press of a dispatched key may run: one run at a time
// per key (single instance) and a cooldown after a run starts. The lock lives
// in this process, which is the only one that runs keys; the cooldown is in
// the store so it survives a restart. A refusal is a normal answer, not an
// error: the press is simply ignored.
type Gate struct {
	store   *Store
	Now     func() time.Time
	mu      sync.Mutex
	running map[Slot]int64
}

func NewGate(store *Store) *Gate {
	return &Gate{store: store, Now: time.Now, running: map[Slot]int64{}}
}

// Decision is the gate's answer. Reason is one of off, not-dispatched,
// running, cooldown when Granted is false.
type Decision struct {
	Granted bool
	Reason  string
	Run     Run
}

func (g *Gate) Acquire(k Key) (Decision, error) {
	if k.Mode == ModeOff {
		return Decision{Reason: "off"}, nil
	}
	if !k.Mode.Dispatched() {
		return Decision{Reason: "not-dispatched"}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if k.SingleInstance {
		if _, busy := g.running[k.Slot]; busy {
			return Decision{Reason: "running"}, nil
		}
	}
	now := g.Now()
	if k.CooldownSeconds > 0 {
		until, err := g.store.CooldownUntil(k.Slot)
		if err != nil {
			return Decision{}, err
		}
		if now.Before(until) {
			return Decision{Reason: "cooldown"}, nil
		}
		if err := g.store.SetCooldownUntil(k.Slot, now.Add(time.Duration(k.CooldownSeconds)*time.Second)); err != nil {
			return Decision{}, err
		}
	}
	run, err := g.store.CreateRun(k.Slot)
	if err != nil {
		return Decision{}, err
	}
	if k.SingleInstance {
		g.running[k.Slot] = run.ID
	}
	return Decision{Granted: true, Run: run}, nil
}

// Release ends the run with how it went and frees the key.
func (g *Gate) Release(run Run, exitCode int, output string) error {
	status := "ok"
	if exitCode != 0 {
		status = "failed"
	}
	return g.finish(run, status, &exitCode, output)
}

// Skip ends a run this program decided not to execute after all.
func (g *Gate) Skip(run Run, why string) error {
	return g.finish(run, "skipped", nil, why)
}

func (g *Gate) finish(run Run, status string, exitCode *int, output string) error {
	err := g.store.FinishRun(run.ID, status, exitCode, output)
	g.mu.Lock()
	if g.running[run.Slot] == run.ID {
		delete(g.running, run.Slot)
	}
	g.mu.Unlock()
	return err
}

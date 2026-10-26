//go:build linux

package platform

import (
	"fmt"
	"reflect"
	"testing"
)

type fwdLog struct {
	emits   []keyEvent
	hotkeys []string
	combos  []string
}

func newTestForwarder() (*forwarder, *fwdLog) {
	l := &fwdLog{}
	f := newForwarder("ctrl+opt+cmd",
		func(digit int, down bool) { l.hotkeys = append(l.hotkeys, fmt.Sprintf("%d %v", digit, down)) },
		func(code uint16, down bool) {
			v := int32(0)
			if down {
				v = 1
			}
			l.emits = append(l.emits, keyEvent{code, v})
		},
		func(combo string) { l.combos = append(l.combos, combo) })
	return f, l
}

func feed(f *forwarder, events ...keyEvent) {
	for _, e := range events {
		f.Event(e.Code, e.Value)
	}
}

func TestForwarderDispatchesHotkeysWithoutLeakingModifiers(t *testing.T) {
	f, l := newTestForwarder()
	feed(f, keyEvent{29, 1}, keyEvent{56, 1}, keyEvent{125, 1}, keyEvent{2, 1}, keyEvent{2, 0}, keyEvent{125, 0}, keyEvent{56, 0}, keyEvent{29, 0})
	if !reflect.DeepEqual(l.hotkeys, []string{"1 true", "1 false"}) {
		t.Errorf("hotkeys %v", l.hotkeys)
	}
	if len(l.emits) != 0 {
		t.Errorf("nothing may reach the desktop, got %v", l.emits)
	}
	if !reflect.DeepEqual(l.combos, []string{"ctrl+opt+cmd+1"}) {
		t.Errorf("combos %v", l.combos)
	}
}

func TestForwarderPassesTypedCombos(t *testing.T) {
	f, l := newTestForwarder()
	in := []keyEvent{{125, 1}, {42, 1}, {5, 1}, {5, 0}, {42, 0}, {125, 0}}
	feed(f, in...)
	if !reflect.DeepEqual(l.emits, in) {
		t.Errorf("emits %v want %v", l.emits, in)
	}
	if len(l.hotkeys) != 0 || !reflect.DeepEqual(l.combos, []string{"shift+cmd+4"}) {
		t.Errorf("hotkeys %v combos %v", l.hotkeys, l.combos)
	}
}

func TestForwarderFlushesABareModifierTap(t *testing.T) {
	f, l := newTestForwarder()
	feed(f, keyEvent{125, 1}, keyEvent{125, 0})
	if want := []keyEvent{{125, 1}, {125, 0}}; !reflect.DeepEqual(l.emits, want) {
		t.Errorf("emits %v want %v", l.emits, want)
	}
}

func TestForwarderIgnoresRepeats(t *testing.T) {
	f, l := newTestForwarder()
	feed(f, keyEvent{30, 1}, keyEvent{30, 2}, keyEvent{30, 2}, keyEvent{30, 0})
	if want := []keyEvent{{30, 1}, {30, 0}}; !reflect.DeepEqual(l.emits, want) {
		t.Errorf("emits %v want %v", l.emits, want)
	}
}

func TestForwarderHotkeyNeedsAllThreeModifiers(t *testing.T) {
	f, l := newTestForwarder()
	in := []keyEvent{{29, 1}, {125, 1}, {2, 1}, {2, 0}, {125, 0}, {29, 0}}
	feed(f, in...)
	if !reflect.DeepEqual(l.emits, in) || len(l.hotkeys) != 0 {
		t.Errorf("emits %v hotkeys %v", l.emits, l.hotkeys)
	}
}

func TestForwarderHandlesTwoHotkeysUnderHeldModifiers(t *testing.T) {
	f, l := newTestForwarder()
	feed(f, keyEvent{29, 1}, keyEvent{56, 1}, keyEvent{125, 1}, keyEvent{2, 1}, keyEvent{2, 0}, keyEvent{3, 1}, keyEvent{3, 0}, keyEvent{125, 0}, keyEvent{56, 0}, keyEvent{29, 0})
	if !reflect.DeepEqual(l.hotkeys, []string{"1 true", "1 false", "2 true", "2 false"}) || len(l.emits) != 0 {
		t.Errorf("hotkeys %v emits %v", l.hotkeys, l.emits)
	}
}

func TestForwarderReportsMediaAndButtons(t *testing.T) {
	f, l := newTestForwarder()
	feed(f, keyEvent{115, 1}, keyEvent{115, 0}, keyEvent{0x111, 1}, keyEvent{0x111, 0})
	if !reflect.DeepEqual(l.combos, []string{"volume-up", "right-click"}) {
		t.Errorf("combos %v", l.combos)
	}
	if want := []keyEvent{{115, 1}, {115, 0}, {0x111, 1}, {0x111, 0}}; !reflect.DeepEqual(l.emits, want) {
		t.Errorf("emits %v", l.emits)
	}
}

//go:build linux

package platform

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func fakePad(t *testing.T) *uinputDevice {
	t.Helper()
	keys := []uint16{29, 56, 125, 42}
	for c := uint16(2); c <= 11; c++ {
		keys = append(keys, c)
	}
	for _, n := range "abcdefghijklmnopqrstuvwxyz" {
		c, _ := KeyCode(string(n))
		keys = append(keys, c)
	}
	keys = append(keys, 115) // volume-up
	d, err := newUinputDevice("Macro Keyboard", unix.BUS_USB, 0x1189, 0x8890, keys)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func waitPad(t *testing.T, connected bool) {
	t.Helper()
	for i := 0; i < 50; i++ {
		devices, _ := PadStatus()
		if (len(devices) > 0) == connected {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pad connected=%v never happened", connected)
}

func emitAll(t *testing.T, d *uinputDevice, events ...keyEvent) {
	t.Helper()
	for _, e := range events {
		if err := d.Emit(e.Code, e.Value == 1); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWatcher(t *testing.T) {
	requireUinput(t)
	padPoll = 200 * time.Millisecond
	kbd := observeVirtualKeyboard(t)
	var mu sync.Mutex
	var hot []string
	stop, err := Listen(func(digit int, down bool) {
		mu.Lock()
		hot = append(hot, fmt.Sprintf("%d %v", digit, down))
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	hotkeys := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), hot...) }
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for i := 0; i < 30; i++ {
			if cond() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("%s never happened (hotkeys %v)", what, hotkeys())
	}
	hotkey1 := []keyEvent{{29, 1}, {56, 1}, {125, 1}, {2, 1}, {2, 0}, {125, 0}, {56, 0}, {29, 0}}

	pad := fakePad(t)
	waitPad(t, true)
	devices, inputAllowed := PadStatus()
	if devices[0].VendorID != 0x1189 || devices[0].ProductID != 0x8890 || !inputAllowed || !InputMonitoringAllowed() {
		t.Fatalf("PadStatus: %+v %v", devices, inputAllowed)
	}

	t.Run("dispatches a hotkey and keeps it off the desktop", func(t *testing.T) {
		emitAll(t, pad, hotkey1...)
		waitFor("hotkey 1", func() bool { h := hotkeys(); return len(h) == 2 && h[0] == "1 true" && h[1] == "1 false" })
		presses, _ := PadPresses(0)
		if len(presses) != 1 || presses[0].Combo != "ctrl+opt+cmd+1" {
			t.Fatalf("presses %+v", presses)
		}
		select {
		case ev := <-kbd:
			t.Fatalf("leaked to the desktop: %+v", ev)
		case <-time.After(300 * time.Millisecond):
		}
	})

	t.Run("forwards a typed combo", func(t *testing.T) {
		combo := []keyEvent{{125, 1}, {42, 1}, {5, 1}, {5, 0}, {42, 0}, {125, 0}}
		emitAll(t, pad, combo...)
		expectEvents(t, kbd, combo)
		presses, _ := PadPresses(0)
		if len(presses) != 2 || presses[1].Combo != "shift+cmd+4" {
			t.Fatalf("presses %+v", presses)
		}
	})

	t.Run("follows a replug", func(t *testing.T) {
		pad.Close()
		waitPad(t, false)
		if _, inputAllowed := PadStatus(); !inputAllowed {
			t.Fatal("no pad means nothing to allow")
		}
		pad = fakePad(t)
		waitPad(t, true)
		emitAll(t, pad, hotkey1...)
		waitFor("hotkey after replug", func() bool { return len(hotkeys()) == 4 })
	})

	t.Run("stop uninstalls the callback", func(t *testing.T) {
		stop()
		emitAll(t, pad, hotkey1...)
		time.Sleep(300 * time.Millisecond)
		if len(hotkeys()) != 4 {
			t.Fatalf("callback still called: %v", hotkeys())
		}
		presses, _ := PadPresses(0)
		if len(presses) != 4 {
			t.Fatalf("presses keep being recorded without a listener: %d", len(presses))
		}
	})
	pad.Close()
}

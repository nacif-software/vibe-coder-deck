//go:build linux

package platform

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func fakePad(t *testing.T) *uinputDevice { return fakePadAt(t, uinputPath) }

// fakePadAt makes the fake pad through a given uinput node, so a test can
// hide uinputPath from the code under test while still having a pad.
func fakePadAt(t *testing.T, uinput string) *uinputDevice {
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
	d, err := newUinputDeviceAt(uinput, "Macro Keyboard", unix.BUS_USB, 0x1189, 0x8890, keys)
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

// TestWatcherGrabsOnlyWhenItCanForward covers the onboarding path: the app
// starts before the udev rule exists (no /dev/uinput), the rule is installed
// while it runs. Until then the pad must not be grabbed (its keys would
// vanish); afterwards Listen succeeds and the pad is grabbed.
func TestWatcherGrabsOnlyWhenItCanForward(t *testing.T) {
	requireUinput(t)
	padPoll = 200 * time.Millisecond
	vk.mu.Lock()
	saved := vk.dev
	vk.dev = nil
	vk.mu.Unlock()
	oldPath := uinputPath
	uinputPath = filepath.Join(t.TempDir(), "missing-uinput")
	t.Cleanup(func() {
		uinputPath = oldPath
		vk.mu.Lock()
		if vk.dev != nil && vk.dev != saved {
			vk.dev.Close()
		}
		vk.dev = saved
		vk.mu.Unlock()
	})
	pad := fakePadAt(t, "/dev/uinput")
	defer pad.Close()

	if _, err := Listen(func(int, bool) {}); err == nil || !strings.Contains(err.Error(), "udev --install") {
		t.Fatalf("Listen without uinput: %v", err)
	}
	waitPad(t, true)
	time.Sleep(3 * padPoll)
	path, ok := findInputByName("Macro Keyboard")
	if !ok {
		t.Fatal("fake pad not in sysfs")
	}
	second, err := openInput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	seen := make(chan keyEvent, 16)
	go second.Read(func(ev inputEvent) {
		if ev.Type == unix.EV_KEY {
			seen <- keyEvent{ev.Code, ev.Value}
		}
	})
	time.Sleep(100 * time.Millisecond)
	emitAll(t, pad, keyEvent{30, 1}, keyEvent{30, 0})
	expectEvents(t, seen, []keyEvent{{30, 1}, {30, 0}}) // not grabbed: a second reader still sees the pad

	uinputPath = oldPath // the rule is installed
	stop, err := Listen(func(int, bool) {})
	if err != nil {
		t.Fatalf("Listen with uinput back: %v", err)
	}
	defer stop()
	time.Sleep(3 * padPoll)
	emitAll(t, pad, keyEvent{30, 1}, keyEvent{30, 0})
	select {
	case ev := <-seen:
		t.Fatalf("still not grabbed after uinput came back: %+v", ev)
	case <-time.After(500 * time.Millisecond):
	}
}

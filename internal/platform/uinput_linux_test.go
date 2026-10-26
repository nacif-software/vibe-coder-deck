//go:build linux

package platform

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
)

// requireUinput skips a device test where /dev/uinput cannot be opened,
// unless VCD_REQUIRE_DEVICES=1 (the Docker runner and CI set it) makes a
// missing device a failure rather than a silent skip.
func requireUinput(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		skipOrFail(t, "no /dev/uinput: "+err.Error())
	}
	f.Close()
}

func skipOrFail(t *testing.T, why string) {
	t.Helper()
	if os.Getenv("VCD_REQUIRE_DEVICES") == "1" {
		t.Fatal(why + " (VCD_REQUIRE_DEVICES=1)")
	}
	t.Skip(why)
}

type keyEvent struct {
	Code  uint16
	Value int32
}

// observeVirtualKeyboard reads the virtual keyboard's own event node and
// returns the EV_KEY events it sees.
func observeVirtualKeyboard(t *testing.T) <-chan keyEvent {
	t.Helper()
	if _, err := virtualKeyboard(); err != nil {
		t.Fatal(err)
	}
	var path string
	for i := 0; i < 50; i++ {
		var ok bool
		if path, ok = findInputByName("Vibe Coder Deck"); ok {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if path == "" {
		t.Fatal("virtual keyboard not in /sys/class/input")
	}
	r, err := openInput(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	got := make(chan keyEvent, 64)
	go r.Read(func(ev inputEvent) {
		if ev.Type == unix.EV_KEY {
			got <- keyEvent{ev.Code, ev.Value}
		}
	})
	time.Sleep(100 * time.Millisecond) // let the reader start before typing
	return got
}

func expectEvents(t *testing.T, got <-chan keyEvent, want []keyEvent) {
	t.Helper()
	for i, w := range want {
		select {
		case ev := <-got:
			if ev != w {
				t.Fatalf("event %d: %+v want %+v", i, ev, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("event %d (%+v) never arrived", i, w)
		}
	}
}

func TestVirtualKeyboardTypesAndIsObservable(t *testing.T) {
	requireUinput(t)
	got := observeVirtualKeyboard(t)
	if err := (Runner{}).SendKeys("ctrl+c"); err != nil {
		t.Fatal(err)
	}
	expectEvents(t, got, []keyEvent{{29, 1}, {46, 1}, {46, 0}, {29, 0}})
	if err := typeText("Hi!"); err != nil {
		t.Fatal(err)
	}
	expectEvents(t, got, []keyEvent{{42, 1}, {35, 1}, {35, 0}, {42, 0}, {23, 1}, {23, 0}, {42, 1}, {2, 1}, {2, 0}, {42, 0}})
	if err := typeText("é"); err == nil {
		t.Fatal("non-US rune must be refused")
	}
	if err := PostClick("right"); err != nil {
		t.Fatal(err)
	}
	expectEvents(t, got, []keyEvent{{0x111, 1}, {0x111, 0}})
	if err := (Runner{}).SendKeys("volume-up, click"); err != nil {
		t.Fatal(err)
	}
	expectEvents(t, got, []keyEvent{{115, 1}, {115, 0}, {0x110, 1}, {0x110, 0}})
}

func TestPlayMacroReleasesHeldModifiersWhenCancelled(t *testing.T) {
	requireUinput(t)
	got := observeVirtualKeyboard(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	code, out, err := (Runner{}).PlayMacro(ctx, []deck.Step{{Type: "keydown", Value: "shift"}, {Type: "delay", Value: "5000"}})
	if err != nil || code != 0 {
		t.Fatalf("PlayMacro: %d %q %v", code, out, err)
	}
	expectEvents(t, got, []keyEvent{{42, 1}, {42, 0}})
}

func TestPlayMacroStepsTypeThroughTheVirtualKeyboard(t *testing.T) {
	requireUinput(t)
	got := observeVirtualKeyboard(t)
	steps := []deck.Step{{Type: "keys", Value: "cmd+a"}, {Type: "text", Value: "x"}, {Type: "click", Value: "middle"}, {Type: "keys", Value: "play"}}
	if code, out, err := (Runner{}).PlayMacro(context.Background(), steps); err != nil || code != 0 {
		t.Fatalf("PlayMacro: %d %q %v", code, out, err)
	}
	expectEvents(t, got, []keyEvent{{125, 1}, {30, 1}, {30, 0}, {125, 0}, {45, 1}, {45, 0}, {0x112, 1}, {0x112, 0}, {164, 1}, {164, 0}})
	if code, out, _ := (Runner{}).PlayMacro(context.Background(), []deck.Step{{Type: "keys", Value: "no-such-key"}}); code != 65 || out == "" {
		t.Fatalf("unknown key: %d %q", code, out)
	}
}

func TestRunnerRunsShellAndScripts(t *testing.T) {
	code, out, err := (Runner{}).RunShell("echo hi; exit 3")
	if err != nil || code != 3 || out != "hi\n" {
		t.Fatalf("RunShell: %d %q %v", code, out, err)
	}
	script := t.TempDir() + "/s.sh"
	os.WriteFile(script, []byte("#!/bin/sh\necho from-script\n"), 0o755)
	if code, out, err := (Runner{}).RunScript(script); err != nil || code != 0 || out != "from-script\n" {
		t.Fatalf("RunScript: %d %q %v", code, out, err)
	}
}

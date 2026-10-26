//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recorded(t *testing.T, n int) []RecordedEvent {
	t.Helper()
	for i := 0; i < 30; i++ {
		if on, events := Recording(0); on && len(events) >= n {
			return events
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, events := Recording(0)
	t.Fatalf("only %d events recorded: %+v", len(events), events)
	return nil
}

func describe(events []RecordedEvent) string {
	var parts []string
	for _, e := range events {
		parts = append(parts, e.Type+" "+e.Value)
	}
	return strings.Join(parts, ", ")
}

func TestRecorderCapturesKeysAndClicks(t *testing.T) {
	requireUinput(t)
	if _, err := virtualKeyboard(); err != nil {
		t.Fatal(err)
	}
	if on, _ := Recording(0); on {
		t.Fatal("recording before start")
	}
	if err := StartRecording(true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // the readers start
	if err := (Runner{}).SendKeys("ctrl+c"); err != nil {
		t.Fatal(err)
	}
	if err := PostClick("left"); err != nil {
		t.Fatal(err)
	}
	events := recorded(t, 5)
	if got := describe(events[:5]); got != "keydown ctrl, keydown c, keyup c, keyup ctrl, click left" {
		t.Fatalf("got %s", got)
	}
	if events[0].At == 0 || events[4].At < events[0].At {
		t.Fatalf("timestamps: %+v", events)
	}
	StopRecording()
	if on, _ := Recording(0); on {
		t.Fatal("still recording after stop")
	}

	// Without the mouse, clicks are not recorded; a second start clears the log.
	if err := StartRecording(false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := PostClick("right"); err != nil {
		t.Fatal(err)
	}
	if err := PostKey("a", true); err != nil {
		t.Fatal(err)
	}
	PostKey("a", false)
	events = recorded(t, 2)
	if got := describe(events); got != "keydown a, keyup a" {
		t.Fatalf("got %s", got)
	}
	StopRecording()
}

// TestRecordableNodesSkipThePad: the pad's own node is readable (udev rule)
// but grabbed by the watcher, so recording from it yields nothing; it must
// not count as a keyboard for the recorder or the recording permission.
func TestRecordableNodesSkipThePad(t *testing.T) {
	requireUinput(t)
	if _, err := virtualKeyboard(); err != nil {
		t.Fatal(err)
	}
	kbd, ok := findInputByName("Vibe Coder Deck")
	if !ok {
		t.Fatal("no virtual keyboard")
	}
	pad := fakePad(t)
	defer pad.Close()
	var padPath string
	for i := 0; i < 50 && padPath == ""; i++ {
		padPath, _ = findInputByName("Macro Keyboard")
		time.Sleep(100 * time.Millisecond)
	}
	if padPath == "" {
		t.Fatal("fake pad not in sysfs")
	}
	dir := t.TempDir()
	os.Symlink(kbd, filepath.Join(dir, filepath.Base(kbd)))
	os.Symlink(padPath, filepath.Join(dir, filepath.Base(padPath)))
	got := recordableNodes(dir, []padNode{{Event: padPath}}, true)
	if len(got) != 1 || filepath.Base(got[0]) != filepath.Base(kbd) {
		t.Fatalf("recordable nodes with the pad excluded: %v", got)
	}
	if got := recordableNodes(dir, nil, true); len(got) != 2 {
		t.Fatalf("without the exclusion both are keyboards: %v", got)
	}
}

//go:build linux

package platform

import (
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

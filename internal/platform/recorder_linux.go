//go:build linux

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

// RecordedEvent is one key or click the recorder saw, for the macro editor.
type RecordedEvent struct {
	Type  string `json:"type"` // keydown, keyup, click
	Value string `json:"value"`
	At    int64  `json:"at"`
}

var rec struct {
	mu      sync.Mutex
	on      bool
	events  []RecordedEvent
	readers []*inputReader
}

var clickButtons = map[uint16]string{0x110: "left", 0x111: "right", 0x112: "middle"}

// recordableNodes are the readable keyboards (and, with mouse, devices with
// mouse buttons) under dir, except the pad's own nodes: the watcher grabs
// those, so a second reader would see nothing.
func recordableNodes(dir string, pads []padNode, mouse bool) []string {
	skip := map[string]bool{}
	for _, p := range pads {
		skip[filepath.Base(p.Event)] = true
	}
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") || skip[e.Name()] {
			continue
		}
		path := filepath.Join(dir, e.Name())
		r, err := openInput(path)
		if err != nil {
			continue
		}
		ok := r.HasKey(30) || (mouse && r.HasKey(0x110)) // KEY_A, BTN_LEFT
		r.Close()
		if ok {
			out = append(out, path)
		}
	}
	return out
}

// StartRecording reads every keyboard (and, with mouse, every device with
// mouse buttons) without grabbing them. It needs read access to the event
// nodes: the recording permission.
func StartRecording(mouse bool) error {
	StopRecording()
	var readers []*inputReader
	for _, path := range recordableNodes(devInputDir, padNodes(pad.VendorID, pad.ProductID), mouse) {
		if r, err := openInput(path); err == nil {
			readers = append(readers, r)
		}
	}
	if len(readers) == 0 {
		return errors.New("no keyboard can be read: see the recording permission")
	}
	rec.mu.Lock()
	rec.on, rec.events, rec.readers = true, nil, readers
	rec.mu.Unlock()
	for _, r := range readers {
		go recordFrom(r, mouse)
	}
	return nil
}

func recordFrom(r *inputReader, mouse bool) {
	r.Read(func(ev inputEvent) {
		if ev.Type != unix.EV_KEY || ev.Value == 2 {
			return
		}
		var event RecordedEvent
		if button, ok := clickButtons[ev.Code]; ok {
			if !mouse || ev.Value != 1 {
				return
			}
			event = RecordedEvent{Type: "click", Value: button}
		} else {
			event = RecordedEvent{Type: map[bool]string{true: "keydown", false: "keyup"}[ev.Value == 1], Value: KeyName(int(ev.Code))}
		}
		event.At = time.Now().UnixMilli()
		rec.mu.Lock()
		if rec.on {
			rec.events = append(rec.events, event)
		}
		rec.mu.Unlock()
	})
}

// StopRecording closes the readers; the events stay until the next start.
func StopRecording() {
	rec.mu.Lock()
	readers := rec.readers
	rec.on, rec.readers = false, nil
	rec.mu.Unlock()
	for _, r := range readers {
		r.Close()
	}
}

// Recording reports whether a recording is on and the events from index since.
func Recording(since int) (bool, []RecordedEvent) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if since < 0 {
		since = 0
	}
	if since > len(rec.events) {
		since = len(rec.events)
	}
	return rec.on, append([]RecordedEvent(nil), rec.events[since:]...)
}

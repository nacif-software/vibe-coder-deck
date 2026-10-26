//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation -framework AppKit
#include <ApplicationServices/ApplicationServices.h>
#import <AppKit/AppKit.h>

extern void deckRecordEvent(int kind, int code, int down, int button);

static CFMachPortRef recTap = NULL;
static CFRunLoopSourceRef recSrc = NULL;

static CGEventFlags flagForCode(int code) {
	switch (code) {
	case 54: case 55: return kCGEventFlagMaskCommand;
	case 56: case 60: return kCGEventFlagMaskShift;
	case 58: case 61: return kCGEventFlagMaskAlternate;
	case 59: case 62: return kCGEventFlagMaskControl;
	case 57: return kCGEventFlagMaskAlphaShift;
	case 63: return kCGEventFlagMaskSecondaryFn;
	}
	return 0;
}

static CGEventRef recCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef ev, void *ctx) {
	int code = (int)CGEventGetIntegerValueField(ev, kCGKeyboardEventKeycode);
	switch (type) {
	case kCGEventKeyDown: deckRecordEvent(0, code, 1, 0); break;
	case kCGEventKeyUp: deckRecordEvent(0, code, 0, 0); break;
	case kCGEventFlagsChanged: deckRecordEvent(1, code, (CGEventGetFlags(ev) & flagForCode(code)) ? 1 : 0, 0); break;
	case kCGEventLeftMouseDown: deckRecordEvent(2, 0, 1, 0); break;
	case kCGEventRightMouseDown: deckRecordEvent(2, 0, 1, 1); break;
	case kCGEventOtherMouseDown: deckRecordEvent(2, 0, 1, 2); break;
	case NX_SYSDEFINED: {
		// Media keys (the F-row's default functions): subtype 8, with the
		// NX_KEYTYPE in the high half of data1 and down (0xA) / up (0xB) in
		// its second byte. Auto-repeats (bit 0) are skipped.
		NSEvent *e = [NSEvent eventWithCGEvent:ev];
		if (e.subtype != 8) break;
		long data1 = e.data1;
		int key = (int)((data1 & 0xFFFF0000) >> 16);
		int state = (int)((data1 & 0xFF00) >> 8);
		if (data1 & 0x1) break;
		if (state == 0xA || state == 0xB) deckRecordEvent(3, key, state == 0xA, 0);
		break;
	}
	case kCGEventTapDisabledByTimeout: case kCGEventTapDisabledByUserInput: if (recTap) CGEventTapEnable(recTap, true); break;
	default: break;
	}
	return ev;
}

static int recStart(int mouse) {
	if (recTap) return 1;
	// Without Input Monitoring the tap is still created but macOS drops every
	// key event (clicks still arrive), so refuse instead of recording nothing.
	if (!CGPreflightListenEventAccess()) return 0;
	CGEventMask mask = CGEventMaskBit(kCGEventKeyDown) | CGEventMaskBit(kCGEventKeyUp) | CGEventMaskBit(kCGEventFlagsChanged) | CGEventMaskBit(NX_SYSDEFINED);
	if (mouse) mask |= CGEventMaskBit(kCGEventLeftMouseDown) | CGEventMaskBit(kCGEventRightMouseDown) | CGEventMaskBit(kCGEventOtherMouseDown);
	recTap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionListenOnly, mask, recCallback, NULL);
	if (!recTap) return 0;
	recSrc = CFMachPortCreateRunLoopSource(NULL, recTap, 0);
	CFRunLoopAddSource(CFRunLoopGetMain(), recSrc, kCFRunLoopCommonModes);
	CGEventTapEnable(recTap, true);
	return 1;
}

static void recStop(void) {
	if (!recTap) return;
	CGEventTapEnable(recTap, false);
	CFRunLoopRemoveSource(CFRunLoopGetMain(), recSrc, kCFRunLoopCommonModes);
	CFRelease(recSrc);
	CFRelease(recTap);
	recSrc = NULL;
	recTap = NULL;
}
*/
import "C"

import (
	"errors"
	"sync"
	"time"

	"github.com/nacif-software/vibe-coder-deck/internal/debuglog"
)

// RecordedEvent is one thing the user did while recording: a key going
// down or up, or a mouse click, At milliseconds after recording started.
type RecordedEvent struct {
	Type  string `json:"type"` // keydown, keyup, click
	Value string `json:"value"`
	At    int64  `json:"at"`
}

var rec struct {
	mu      sync.Mutex
	on      bool
	started time.Time
	events  []RecordedEvent
}

//export deckRecordEvent
func deckRecordEvent(kind, code, down, button C.int) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !rec.on {
		return
	}
	at := time.Since(rec.started).Milliseconds()
	switch kind {
	case 2:
		rec.events = append(rec.events, RecordedEvent{Type: "click", Value: []string{"left", "right", "middle"}[button], At: at})
		debuglog.Printf("record: click %d", int(button))
	default:
		if kind != 3 && code == 53 && down == 1 { // Esc stops and is not recorded
			rec.on = false
			C.recStop()
			return
		}
		name := KeyName(int(code))
		if kind == 3 {
			name = MediaKeyName(int(code))
		}
		t := "keyup"
		if down == 1 {
			t = "keydown"
		}
		rec.events = append(rec.events, RecordedEvent{Type: t, Value: name, At: at})
		debuglog.Printf("record: %s %s (key code %d)", t, name, int(code))
	}
}

// StartRecording begins capturing keys (and clicks when mouse is true) from
// the whole system until StopRecording or Esc.
func StartRecording(mouse bool) error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.on {
		return nil
	}
	m := C.int(0)
	if mouse {
		m = 1
	}
	if C.recStart(m) == 0 {
		RequestPermission(PermInputMonitoring)
		return errors.New("could not listen to the keyboard: allow Vibe Coder Deck in System Settings › Privacy & Security › Input Monitoring, then relaunch it")
	}
	rec.on, rec.started, rec.events = true, time.Now(), nil
	return nil
}

func StopRecording() {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.on = false
	C.recStop()
}

// Recording reports whether a recording is running and the events captured so far after index since.
func Recording(since int) (bool, []RecordedEvent) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if since < 0 || since > len(rec.events) {
		since = 0
	}
	out := make([]RecordedEvent, len(rec.events)-since)
	copy(out, rec.events[since:])
	return rec.on, out
}

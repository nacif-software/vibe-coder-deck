//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ApplicationServices -framework AppKit
#include <ApplicationServices/ApplicationServices.h>
#import <AppKit/AppKit.h>

// postMedia presses or releases a media key the way the keyboard does: a
// system-defined event, subtype 8, NX_KEYTYPE in data1.
static int postMedia(int key, int down) {
	int state = down ? 0xA : 0xB;
	NSEvent *e = [NSEvent otherEventWithType:NSEventTypeSystemDefined location:NSZeroPoint
		modifierFlags:(state << 8) timestamp:0 windowNumber:0 context:nil subtype:8
		data1:((key << 16) | (state << 8)) data2:-1];
	if (e == nil) return 0;
	CGEventPost(kCGHIDEventTap, [e CGEvent]);
	return 1;
}

static int postKey(uint16_t code, int down, uint64_t flags) {
	CGEventRef ev = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)code, down ? true : false);
	if (ev == NULL) return 0;
	CGEventSetFlags(ev, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, ev);
	CFRelease(ev);
	return 1;
}

static int postClick(int button) {
	CGEventRef probe = CGEventCreate(NULL);
	if (probe == NULL) return 0;
	CGPoint at = CGEventGetLocation(probe);
	CFRelease(probe);
	CGEventType downType = kCGEventLeftMouseDown, upType = kCGEventLeftMouseUp;
	CGMouseButton mb = kCGMouseButtonLeft;
	if (button == 1) { downType = kCGEventRightMouseDown; upType = kCGEventRightMouseUp; mb = kCGMouseButtonRight; }
	if (button == 2) { downType = kCGEventOtherMouseDown; upType = kCGEventOtherMouseUp; mb = kCGMouseButtonCenter; }
	CGEventRef down = CGEventCreateMouseEvent(NULL, downType, at, mb);
	CGEventRef up = CGEventCreateMouseEvent(NULL, upType, at, mb);
	if (down == NULL || up == NULL) return 0;
	CGEventPost(kCGHIDEventTap, down);
	CGEventPost(kCGHIDEventTap, up);
	CFRelease(down);
	CFRelease(up);
	return 1;
}
*/
import "C"

import (
	"errors"
	"strings"
)

// PostKey presses or releases one key with the given modifier flags, as a
// real keyboard would (Quartz event at the HID tap). Media key names (play,
// next, volume-up…) are posted as the keyboard's system-defined events.
func PostKey(name string, down bool, flags uint64) error {
	if key, ok := MediaKey(name); ok {
		if C.postMedia(C.int(key), boolInt(down)) == 0 {
			return errors.New("could not create the media key event")
		}
		return nil
	}
	code, err := VirtualKeyCode(name)
	if err != nil {
		return err
	}
	if C.postKey(C.uint16_t(code), boolInt(down), C.uint64_t(flags)) == 0 {
		return errors.New("could not create the key event")
	}
	return nil
}

// PostClick clicks a mouse button where the pointer is.
func PostClick(button string) error {
	var b C.int
	switch strings.ToLower(button) {
	case "left":
		b = 0
	case "right":
		b = 1
	case "middle":
		b = 2
	default:
		return errors.New("click must be left, right or middle")
	}
	if C.postClick(b) == 0 {
		return errors.New("could not create the mouse event")
	}
	return nil
}

func boolInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

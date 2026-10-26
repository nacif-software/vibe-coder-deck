//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void setDockVisible(int visible) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp setActivationPolicy:visible ? NSApplicationActivationPolicyRegular : NSApplicationActivationPolicyAccessory];
		if (visible) [NSApp activateIgnoringOtherApps:YES];
	});
}
*/
import "C"

// SetDockVisible shows the app in the Dock (with its menu bar, cmd+Q and a
// place in Force Quit) while its window is open, and hides it again when the
// window closes, so a hidden menu bar icon never leaves it unreachable.
func SetDockVisible(visible bool) {
	v := C.int(0)
	if visible {
		v = 1
	}
	C.setDockVisible(v)
}

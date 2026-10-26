//go:build darwin

package platform

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/hid/IOHIDKeys.h>

extern void deckPadDevice(void *dev, int added, char *product, char *manufacturer, int vid, int pid);
extern void deckPadInput(void *dev, int page, int usage, int value);

static IOHIDManagerRef padMgr = NULL;

static void padString(CFTypeRef v, char *buf, int n) {
	buf[0] = 0;
	if (v && CFGetTypeID(v) == CFStringGetTypeID()) CFStringGetCString((CFStringRef)v, buf, n, kCFStringEncodingUTF8);
}

static int padInt(CFTypeRef v) {
	int x = 0;
	if (v && CFGetTypeID(v) == CFNumberGetTypeID()) CFNumberGetValue((CFNumberRef)v, kCFNumberIntType, &x);
	return x;
}

static void padMatched(void *ctx, IOReturn res, void *sender, IOHIDDeviceRef dev) {
	char product[256], manufacturer[256];
	padString(IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDProductKey)), product, sizeof product);
	padString(IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDManufacturerKey)), manufacturer, sizeof manufacturer);
	deckPadDevice(dev, 1, product, manufacturer,
		padInt(IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDVendorIDKey))),
		padInt(IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDProductIDKey))));
}

static void padRemoved(void *ctx, IOReturn res, void *sender, IOHIDDeviceRef dev) {
	deckPadDevice(dev, 0, "", "", 0, 0);
}

static void padValue(void *ctx, IOReturn res, void *sender, IOHIDValueRef val) {
	IOHIDElementRef el = IOHIDValueGetElement(val);
	deckPadInput(IOHIDElementGetDevice(el), (int)IOHIDElementGetUsagePage(el), (int)IOHIDElementGetUsage(el), (int)IOHIDValueGetIntegerValue(val));
}

static void padSetInt(CFMutableDictionaryRef d, CFStringRef key, int v) {
	CFNumberRef n = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &v);
	CFDictionarySetValue(d, key, n);
	CFRelease(n);
}

// padStart watches every HID interface of the pad on the main run loop.
// Without kIOHIDOptionsTypeSeizeDevice the pad keeps typing into apps.
static void padStart(int vid, int pid) {
	if (padMgr) return;
	padMgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
	CFMutableDictionaryRef match = CFDictionaryCreateMutable(kCFAllocatorDefault, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	padSetInt(match, CFSTR(kIOHIDVendorIDKey), vid);
	padSetInt(match, CFSTR(kIOHIDProductIDKey), pid);
	IOHIDManagerSetDeviceMatching(padMgr, match);
	CFRelease(match);
	IOHIDManagerRegisterDeviceMatchingCallback(padMgr, padMatched, NULL);
	IOHIDManagerRegisterDeviceRemovalCallback(padMgr, padRemoved, NULL);
	IOHIDManagerRegisterInputValueCallback(padMgr, padValue, NULL);
	IOHIDManagerScheduleWithRunLoop(padMgr, CFRunLoopGetMain(), kCFRunLoopCommonModes);
}

// padOpen starts the input values; macOS refuses keyboards without Input
// Monitoring, so it is retried once that is granted.
static int padOpen(void) {
	if (!padMgr) return 0;
	return IOHIDManagerOpen(padMgr, kIOHIDOptionsTypeNone) == kIOReturnSuccess;
}
*/
import "C"

import (
	"log"
	"sync"
	"time"
	"unsafe"

	"github.com/nacif-software/vibe-coder-deck/internal/debuglog"
	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

// PadInfo is one HID interface of the pad that macOS reports as connected.
type PadInfo struct {
	Product, Manufacturer string
	VendorID, ProductID   int
}

// PadPress is one keystroke the pad sent, as a combo in the pad's syntax.
type PadPress struct {
	Combo string
	At    int64 // unix milliseconds
}

const keptPresses = 200

var padw struct {
	mu       sync.Mutex
	started  bool
	open     bool
	devices  map[uintptr]PadInfo
	trackers map[uintptr]*pad.Tracker
	presses  []PadPress
	dropped  int // presses discarded from the front of presses
	last     PadPress
}

//export deckPadDevice
func deckPadDevice(dev unsafe.Pointer, added C.int, product, manufacturer *C.char, vid, pid C.int) {
	padw.mu.Lock()
	defer padw.mu.Unlock()
	id := uintptr(dev)
	log.Printf("pad: interface %#x %s (vid %#x pid %#x)", id, map[C.int]string{0: "removed", 1: "added"}[added], int(vid), int(pid))
	if added == 0 {
		delete(padw.devices, id)
		delete(padw.trackers, id)
		return
	}
	padw.devices[id] = PadInfo{Product: C.GoString(product), Manufacturer: C.GoString(manufacturer), VendorID: int(vid), ProductID: int(pid)}
	padw.trackers[id] = &pad.Tracker{}
}

//export deckPadInput
func deckPadInput(dev unsafe.Pointer, page, usage, value C.int) {
	padw.mu.Lock()
	defer padw.mu.Unlock()
	tr := padw.trackers[uintptr(dev)]
	if tr == nil {
		return
	}
	combo, ok := tr.Feed(pad.HIDInput{Page: uint32(page), Usage: uint32(usage), Down: value != 0})
	debuglog.Printf("pad: interface %#x page %#x usage %#x value %d", uintptr(dev), int(page), int(usage), int(value))
	if !ok {
		return
	}
	debuglog.Printf("pad: sent %s", combo)
	now := time.Now().UnixMilli()
	// The pad has two keyboard interfaces; one keystroke may arrive on both.
	if padw.last.Combo == combo && now-padw.last.At < 30 {
		return
	}
	padw.last = PadPress{Combo: combo, At: now}
	padw.presses = append(padw.presses, padw.last)
	if extra := len(padw.presses) - keptPresses; extra > 0 {
		padw.presses = padw.presses[extra:]
		padw.dropped += extra
	}
}

// WatchPad starts following the pad's connection and keystrokes. Keystrokes
// need Input Monitoring; they start on their own once it is granted.
func WatchPad() {
	padw.mu.Lock()
	if padw.started {
		padw.mu.Unlock()
		return
	}
	padw.started = true
	padw.devices = map[uintptr]PadInfo{}
	padw.trackers = map[uintptr]*pad.Tracker{}
	padw.mu.Unlock()

	C.padStart(C.int(pad.VendorID), C.int(pad.ProductID))
	go func() {
		for {
			padw.mu.Lock()
			open := padw.open
			padw.mu.Unlock()
			if !open && InputMonitoringAllowed() {
				ok := C.padOpen() != 0
				log.Printf("pad: open for keystrokes: %v", ok)
				padw.mu.Lock()
				padw.open = ok
				padw.mu.Unlock()
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

// PadStatus reports the pad's interfaces macOS sees now (empty when it is
// unplugged) and whether its keystrokes can be read.
func PadStatus() (devices []PadInfo, inputAllowed bool) {
	padw.mu.Lock()
	defer padw.mu.Unlock()
	for _, d := range padw.devices {
		devices = append(devices, d)
	}
	return devices, padw.open
}

// PadPresses returns the keystrokes numbered since and after, and the next number.
func PadPresses(since int) ([]PadPress, int) {
	padw.mu.Lock()
	defer padw.mu.Unlock()
	next := padw.dropped + len(padw.presses)
	i := since - padw.dropped
	if i < 0 {
		i = 0
	}
	if i >= len(padw.presses) {
		return nil, next
	}
	return append([]PadPress(nil), padw.presses[i:]...), next
}

//go:build darwin

package pad

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/IOCFPlugIn.h>
#include <IOKit/usb/IOUSBLib.h>
#include <IOKit/usb/USB.h>

// A number property of a registry entry, -1 when absent.
static int vcd_num(io_service_t s, const char *key) {
	CFStringRef k = CFStringCreateWithCString(kCFAllocatorDefault, key, kCFStringEncodingUTF8);
	CFTypeRef v = IORegistryEntryCreateCFProperty(s, k, kCFAllocatorDefault, 0);
	CFRelease(k);
	int out = -1;
	if (v && CFGetTypeID(v) == CFNumberGetTypeID()) CFNumberGetValue(v, kCFNumberIntType, &out);
	if (v) CFRelease(v);
	return out;
}

// Every USB interface service on the system (the caller releases the
// iterator). idVendor / idProduct keys in the matching dictionary match
// nothing for interfaces, so the caller filters by property.
static io_iterator_t vcd_interfaces(void) {
	const char *classes[] = {"IOUSBHostInterface", "IOUSBInterface"};
	for (int c = 0; c < 2; c++) {
		CFMutableDictionaryRef m = IOServiceMatching(classes[c]);
		if (!m) continue;
		io_iterator_t it = 0;
		if (IOServiceGetMatchingServices(0, m, &it) == KERN_SUCCESS && it) return it;
	}
	return 0;
}

// Writes `count` packets of `size` bytes to the first interface that has an
// interrupt OUT pipe and can be opened (the config interface; the keyboard and
// mouse interfaces are held by their drivers and have no OUT pipe anyway).
// 0 ok, 1 no device, 2 no config interface could be opened, 4 a write failed
// (*index says which, *detail the IOReturn).
static int vcd_pad_send(int vid, int pid, const unsigned char *data, int count, int size, int *index, int *detail) {
	io_iterator_t it = vcd_interfaces();
	if (!it) return 1;
	int rc = 1;
	io_service_t service;
	while (rc != 0 && rc != 4 && (service = IOIteratorNext(it))) {
		if (vcd_num(service, "idVendor") != vid || vcd_num(service, "idProduct") != pid) { IOObjectRelease(service); continue; }
		if (rc == 1) rc = 2; // the pad is there; now find its config interface
		IOCFPlugInInterface **plugin = NULL;
		SInt32 score = 0;
		kern_return_t kr = IOCreatePlugInInterfaceForService(service, kIOUSBInterfaceUserClientTypeID, kIOCFPlugInInterfaceID, &plugin, &score);
		IOObjectRelease(service);
		if (kr != KERN_SUCCESS || !plugin) { *detail = kr; continue; }
		IOUSBInterfaceInterface300 **intf = NULL;
		HRESULT hr = (*plugin)->QueryInterface(plugin, CFUUIDGetUUIDBytes(kIOUSBInterfaceInterfaceID300), (LPVOID *)&intf);
		(*plugin)->Release(plugin);
		if (hr || !intf) { *detail = (int)hr; continue; }
		kr = (*intf)->USBInterfaceOpen(intf);
		if (kr != kIOReturnSuccess) kr = (*intf)->USBInterfaceOpenSeize(intf); // held by another tool: take it, as libusb does
		if (kr != kIOReturnSuccess) { *detail = kr; (*intf)->Release(intf); continue; }
		UInt8 n = 0;
		(*intf)->GetNumEndpoints(intf, &n);
		int pipe = 0;
		for (UInt8 i = 1; i <= n && !pipe; i++) {
			UInt8 dir, num, type, interval;
			UInt16 maxPacket;
			if ((*intf)->GetPipeProperties(intf, i, &dir, &num, &type, &maxPacket, &interval) != kIOReturnSuccess) continue;
			if (dir == kUSBOut && type == kUSBInterrupt) pipe = i;
		}
		if (pipe) {
			rc = 0;
			for (int i = 0; i < count; i++) {
				// WritePipeTO is for bulk pipes only (kIOReturnBadArgument on an interrupt pipe).
				kr = (*intf)->WritePipe(intf, (UInt8)pipe, (void *)(data + i * size), (UInt32)size);
				if (kr != kIOReturnSuccess) { *index = i; *detail = kr; rc = 4; break; }
			}
		}
		(*intf)->USBInterfaceClose(intf);
		(*intf)->Release(intf);
	}
	IOObjectRelease(it);
	return rc;
}
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

// sendPackets writes the packets; a pad that stops taking data would block
// the (uncancellable) IOKit write, so the caller waits at most writeTimeout.
func sendPackets(packets [][]byte) error {
	if len(packets) == 0 {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- sendPacketsNow(packets) }()
	select {
	case err := <-done:
		return err
	case <-time.After(writeTimeout):
		return fmt.Errorf("the pad did not take the data within %s; unplug and replug it", writeTimeout)
	}
}

const writeTimeout = 5 * time.Second

func sendPacketsNow(packets [][]byte) error {
	flat := flatten(packets)
	var index, detail C.int
	rc := C.vcd_pad_send(C.int(VendorID), C.int(ProductID), (*C.uchar)(unsafe.Pointer(&flat[0])), C.int(len(packets)), C.int(PacketSize), &index, &detail)
	switch rc {
	case 0:
		return nil
	case 1:
		return ErrNotConnected
	case 2:
		return fmt.Errorf("the pad's config interface could not be opened (IOKit 0x%x); is another tool using it?", int(detail))
	default:
		return fmt.Errorf("writing packet %d of %d to the pad failed (IOKit 0x%x); try again", int(index)+1, len(packets), int(detail))
	}
}

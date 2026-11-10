//go:build linux

package pad

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

// fakeSysfs lays out the pad as /sys/bus/usb/devices shows it: the device
// (3-1.4), its keyboard interface (:1.0, interrupt IN) and its config
// interface (:1.1, interrupt OUT only).
func fakeSysfs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(path, value string) {
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("usb3/idVendor", "1d6b") // a hub, not the pad
	write("usb3/idProduct", "0002")
	write("3-1.4/idVendor", "1189")
	write("3-1.4/idProduct", "8890")
	write("3-1.4/busnum", "3")
	write("3-1.4/devnum", "7")
	write("3-1.4:1.0/bInterfaceNumber", "00")
	write("3-1.4:1.0/ep_81/bEndpointAddress", "81")
	write("3-1.4:1.0/ep_81/type", "Interrupt")
	write("3-1.4:1.0/ep_81/direction", "in")
	write("3-1.4:1.1/bInterfaceNumber", "01")
	write("3-1.4:1.1/ep_02/bEndpointAddress", "02")
	write("3-1.4:1.1/ep_02/type", "Interrupt")
	write("3-1.4:1.1/ep_02/direction", "out")
	return root
}

func TestFindUSBPortFindsTheConfigInterfacesOutEndpoint(t *testing.T) {
	port, err := findUSBPort(fakeSysfs(t), VendorID, ProductID, ConfigInterface)
	if err != nil {
		t.Fatal(err)
	}
	if port.node != "/dev/bus/usb/003/007" || port.iface != 1 || port.ep != 0x02 {
		t.Fatalf("port = %+v", port)
	}
}

func TestFindUSBPortWithoutThePad(t *testing.T) {
	_, err := findUSBPort(fakeSysfs(t), VendorID, 0x1234, ConfigInterface)
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestUSBDevfsRequestNumbers(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("the numbers below are for 64-bit kernels")
	}
	for name, got := range map[string]uintptr{"bulk": usbdevfsBulk, "claim": usbdevfsClaimInterface, "release": usbdevfsReleaseInterface, "ioctl": usbdevfsIoctl, "disconnect": usbdevfsDisconnect} {
		want := map[string]uintptr{"bulk": 0xC0185502, "claim": 0x8004550F, "release": 0x80045510, "ioctl": 0xC0105512, "disconnect": 0x5516}[name]
		if got != want {
			t.Errorf("%s = %#x, want %#x", name, got, want)
		}
	}
}

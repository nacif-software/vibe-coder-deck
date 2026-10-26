//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeSysInput builds the sysfs shape the kernel gives a USB HID keyboard:
// class/input/eventN → devices/…/usb/1-1/1-1:1.0/<hid>/input/inputM/eventN,
// eventN/device → inputM, inputM/device → <hid>, and idVendor on 1-1.
func fakeSysInput(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	usb := filepath.Join(root, "devices", "usb", "1-1")
	hid := filepath.Join(usb, "1-1:1.0", "0003:1189:8890.0001")
	input5 := filepath.Join(hid, "input", "input5")
	writeFiles(t, root, map[string]string{
		"devices/usb/1-1/idVendor":     "1189\n",
		"devices/usb/1-1/manufacturer": "Makerlab\n",
		"devices/usb/1-1/product":      "Macro Pad\n",
	})
	writeFiles(t, input5, map[string]string{"name": "Macro Keyboard\n", "id/bustype": "0003\n", "id/vendor": "1189\n", "id/product": "8890\n"})
	os.MkdirAll(filepath.Join(input5, "event3"), 0o755)
	os.Symlink("..", filepath.Join(input5, "event3", "device"))
	os.Symlink("../..", filepath.Join(input5, "device"))
	class := filepath.Join(root, "class", "input")
	os.MkdirAll(class, 0o755)
	os.Symlink(filepath.Join(input5, "event3"), filepath.Join(class, "event3"))
	// A virtual device with the pad's ids (our own uinput keyboard would never
	// carry them, but a test one might) and a USB mouse: both ignored.
	for name, id := range map[string][3]string{"event4": {"0006", "1189", "8890"}, "event5": {"0003", "046d", "c52b"}} {
		dir := filepath.Join(root, "devices", "virtual", name)
		writeFiles(t, dir, map[string]string{"name": name + "\n", "id/bustype": id[0] + "\n", "id/vendor": id[1] + "\n", "id/product": id[2] + "\n"})
		os.MkdirAll(filepath.Join(dir, name), 0o755)
		os.Symlink("..", filepath.Join(dir, name, "device"))
		os.Symlink(filepath.Join(dir, name), filepath.Join(class, name))
	}
	return class
}

func TestPadNodesFindsTheUsbPadInSysfs(t *testing.T) {
	old := sysInput
	sysInput = fakeSysInput(t)
	t.Cleanup(func() { sysInput = old })
	got := padNodes(0x1189, 0x8890)
	want := []padNode{{Event: "/dev/input/event3", Name: "Macro Keyboard", Manufacturer: "Makerlab", Product: "Macro Pad", Vendor: 0x1189, ProductID: 0x8890, Bus: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if n := padNodes(0x046d, 0xc52b); len(n) != 1 || n[0].Manufacturer != "" {
		t.Fatalf("a device without a USB ancestor keeps empty manufacturer/product: %+v", n)
	}
}

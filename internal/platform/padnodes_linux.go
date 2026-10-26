//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// padNode is one /dev/input/eventN of the pad, as sysfs describes it.
type padNode struct {
	Event, Name, Manufacturer, Product string
	Vendor, ProductID, Bus             int
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readHex(path string) int {
	n, err := strconv.ParseInt(readTrim(path), 16, 32)
	if err != nil {
		return -1
	}
	return int(n)
}

// padNodes lists the event devices of the USB device vendor:product. The
// manufacturer and product strings come from the USB device, the first
// ancestor of the HID device that has an idVendor.
func padNodes(vendor, product int) []padNode {
	entries, _ := os.ReadDir(sysInput)
	var out []padNode
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		dev := filepath.Join(sysInput, e.Name(), "device")
		bus, v, p := readHex(filepath.Join(dev, "id", "bustype")), readHex(filepath.Join(dev, "id", "vendor")), readHex(filepath.Join(dev, "id", "product"))
		if bus != unix.BUS_USB || v != vendor || p != product {
			continue
		}
		node := padNode{Event: "/dev/input/" + e.Name(), Name: readTrim(filepath.Join(dev, "name")), Vendor: v, ProductID: p, Bus: bus}
		node.Manufacturer, node.Product = usbIdentity(filepath.Join(dev, "device"))
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Event < out[j].Event })
	return out
}

// usbIdentity walks up from a HID device directory to the USB device that
// owns it and reads its manufacturer and product strings.
func usbIdentity(hid string) (manufacturer, product string) {
	dir, err := filepath.EvalSymlinks(hid)
	if err != nil {
		return "", ""
	}
	for ; len(dir) > 1; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "idVendor")); err == nil {
			return readTrim(filepath.Join(dir, "manufacturer")), readTrim(filepath.Join(dir, "product"))
		}
	}
	return "", ""
}

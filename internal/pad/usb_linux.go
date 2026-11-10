//go:build linux

package pad

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HidrawNode is the /dev/hidrawN of the pad's config interface, where the
// kernel's HID driver took it: writing an output report to the node goes out
// through the interface's interrupt OUT endpoint. "" when there is none,
// which is the usual case (see usbfs_linux.go).
func HidrawNode(vid, pid, iface int) string {
	entries, _ := os.ReadDir("/sys/class/hidraw")
	id := fmt.Sprintf("HID_ID=0003:%08X:%08X", vid, pid)
	phys := fmt.Sprintf("/input%d\n", iface)
	for _, e := range entries {
		uevent, err := os.ReadFile(filepath.Join("/sys/class/hidraw", e.Name(), "device", "uevent"))
		if err != nil {
			continue
		}
		s := string(uevent)
		if strings.Contains(s, id) && strings.Contains(s, phys) {
			return "/dev/" + e.Name()
		}
	}
	return ""
}

// ConfigNode is the node the pad is written through and how it is opened:
// the hidraw node when there is one, else the USB device node. "" when the
// pad is absent.
func ConfigNode(vid, pid, iface int) (path string, flag int) {
	if node := HidrawNode(vid, pid, iface); node != "" {
		return node, os.O_WRONLY
	}
	if port, err := findUSBPort(sysUSBDevices, vid, pid, iface); err == nil {
		return port.node, os.O_RDWR
	}
	return "", 0
}

func sendPackets(packets [][]byte) error {
	node := HidrawNode(VendorID, ProductID, ConfigInterface)
	if node == "" {
		port, err := findUSBPort(sysUSBDevices, VendorID, ProductID, ConfigInterface)
		if err != nil {
			return err
		}
		return sendUSB(port, packets)
	}
	f, err := os.OpenFile(node, os.O_WRONLY, 0)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%s is not writable: run `vibe-coder-deck udev --install` to add the udev rule, then replug the pad", node)
		}
		return err
	}
	defer f.Close()
	for i, p := range packets {
		n, err := f.Write(p)
		if err != nil {
			return fmt.Errorf("writing packet %d of %d to %s: %w", i+1, len(packets), node, err)
		}
		if n != len(p) {
			return fmt.Errorf("writing packet %d of %d to %s: %d of %d bytes", i+1, len(packets), node, n, len(p))
		}
	}
	return nil
}

//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

// Permission is one kind of device access the app needs on Linux.
type Permission struct {
	Name, Label, Why string
	Granted          bool
}

// The names main.go and the page use: Accessibility (typing) is the virtual
// keyboard, Input Monitoring (reading keys) is the pad's input device.
const (
	PermAccessibility   = "uinput"
	PermInputMonitoring = "pad-input"
	PermPadWrite        = "pad-hidraw"
	PermRecording       = "recording"
)

// devInputDir holds the event nodes; tests point it at an empty directory.
var devInputDir = "/dev/input"

func opens(path string, flag int) bool {
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// Permissions reports what can be opened now. The pad's entries are granted
// while it is unplugged, so nothing nags.
func Permissions() []Permission {
	nodes := padNodes(pad.VendorID, pad.ProductID)
	padInput := true
	for _, n := range nodes {
		if !opens(filepath.Join(devInputDir, filepath.Base(n.Event)), os.O_RDONLY) {
			padInput = false
		}
	}
	hidraw := len(nodes) == 0
	if !hidraw {
		if node := pad.HidrawNode(pad.VendorID, pad.ProductID, pad.ConfigInterface); node != "" {
			hidraw = opens(node, os.O_WRONLY)
		}
	}
	return []Permission{
		{Name: PermAccessibility, Label: "Virtual keyboard (/dev/uinput)", Why: "Types macros and forwards the pad's keys through a virtual keyboard.", Granted: AccessibilityAllowed()},
		{Name: PermInputMonitoring, Label: "Read the pad's keys", Why: "Reads the pad's own input device, so its hotkeys reach this app on X11 and Wayland alike.", Granted: padInput},
		{Name: PermPadWrite, Label: "Write the pad", Why: "Writes the key layout to the pad (hidraw) when you apply it.", Granted: hidraw},
		{Name: PermRecording, Label: "Record keystrokes", Why: "The macro recorder reads every keyboard (not the pad, which this app holds), which needs membership of the input group.", Granted: len(recordableNodes(devInputDir, nodes, false)) > 0},
	}
}

// RequestPermission installs the udev rule for the device permissions and
// explains the input group for recording. The macOS names the page and
// main.go use map to the closest thing here.
func RequestPermission(name string) error {
	switch name {
	case PermAccessibility, PermInputMonitoring, PermPadWrite, "accessibility":
		return InstallUdevRule()
	case PermRecording, "input-monitoring":
		return errors.New("add yourself to the input group: sudo usermod -aG input $USER, then log out and in")
	}
	return fmt.Errorf("unknown permission %q", name)
}

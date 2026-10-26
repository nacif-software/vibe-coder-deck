//go:build windows

package platform

// PadInfo is one HID interface of the pad that the OS reports as connected.
type PadInfo struct {
	Product, Manufacturer string
	VendorID, ProductID   int
}

// PadPress is one keystroke the pad sent, as a combo in the pad's syntax.
type PadPress struct {
	Combo string
	At    int64
}

// WatchPad is not implemented on Windows yet: the pad shows as not connected.
func WatchPad() {}

func PadStatus() ([]PadInfo, bool) { return nil, false }

func PadPresses(since int) ([]PadPress, int) { return nil, 0 }

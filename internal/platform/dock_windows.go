//go:build windows

package platform

// SetDockVisible does nothing on Windows: the window is in the taskbar.
func SetDockVisible(visible bool) {}

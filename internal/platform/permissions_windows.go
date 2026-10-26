//go:build windows

package platform

// Permission is something the OS must allow before a feature works. Windows
// asks for nothing.
type Permission struct {
	Name, Label, Why string
	Granted          bool
}

const (
	PermAccessibility   = "accessibility"
	PermInputMonitoring = "input-monitoring"
)

func Permissions() []Permission { return nil }

func AccessibilityAllowed() bool { return true }

func InputMonitoringAllowed() bool { return true }

func RequestPermission(name string) error { return nil }

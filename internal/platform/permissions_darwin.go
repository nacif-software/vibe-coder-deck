//go:build darwin

package platform

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>

static int axTrusted(int prompt) {
	if (!prompt) return AXIsProcessTrusted();
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *vals[] = { kCFBooleanTrue };
	CFDictionaryRef opts = CFDictionaryCreate(NULL, keys, vals, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	Boolean ok = AXIsProcessTrustedWithOptions(opts);
	CFRelease(opts);
	return ok;
}

static int listenAllowed(void) { return CGPreflightListenEventAccess(); }
static int listenRequest(void) { return CGRequestListenEventAccess(); }
*/
import "C"

import (
	"fmt"
	"os/exec"
)

// Permission is something macOS must allow before a feature works.
type Permission struct {
	Name, Label, Why string
	Granted          bool
}

const (
	PermAccessibility   = "accessibility"
	PermInputMonitoring = "input-monitoring"
)

// settingsPanes are the System Settings pages where each permission is turned on.
var settingsPanes = map[string]string{
	PermAccessibility:   "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
	PermInputMonitoring: "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent",
}

func Permissions() []Permission {
	return []Permission{
		{Name: PermAccessibility, Label: "Accessibility", Why: "the pad's hotkeys (script, shell and macro keys) and macro playback", Granted: C.axTrusted(0) != 0},
		{Name: PermInputMonitoring, Label: "Input Monitoring", Why: "the macro recorder and seeing which pad key was pressed", Granted: InputMonitoringAllowed()},
	}
}

func AccessibilityAllowed() bool { return C.axTrusted(0) != 0 }

func InputMonitoringAllowed() bool { return C.listenAllowed() != 0 }

// RequestPermission shows macOS's own prompt for the permission (it adds the
// app to the list in System Settings) and opens that System Settings page,
// since macOS only prompts once per app.
func RequestPermission(name string) error {
	pane, ok := settingsPanes[name]
	if !ok {
		return fmt.Errorf("unknown permission %q (use %s or %s)", name, PermAccessibility, PermInputMonitoring)
	}
	switch name {
	case PermAccessibility:
		if C.axTrusted(1) != 0 {
			return nil
		}
	case PermInputMonitoring:
		if C.listenRequest() != 0 {
			return nil
		}
	}
	return exec.Command("open", pane).Run()
}

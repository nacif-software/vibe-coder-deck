//go:build linux

package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUdevRuleMentionsThePadAndUinput(t *testing.T) {
	for _, want := range []string{`ATTRS{idVendor}=="1189"`, `ATTRS{idProduct}=="8890"`, `KERNEL=="uinput"`, `TAG+="uaccess"`, `SUBSYSTEM=="hidraw"`, `SUBSYSTEM=="input"`} {
		if !strings.Contains(UdevRule, want) {
			t.Errorf("rule lacks %s:\n%s", want, UdevRule)
		}
	}
	script := udevInstallScript()
	for _, want := range []string{UdevRulePath, "udevadm control --reload", "udevadm trigger"} {
		if !strings.Contains(script, want) {
			t.Errorf("install script lacks %q:\n%s", want, script)
		}
	}
}

func TestPermissionsWithoutAccess(t *testing.T) {
	oldSys, oldDev, oldUinput := sysInput, devInputDir, uinputPath
	sysInput = fakeSysInput(t)                                // the pad is plugged in…
	devInputDir = t.TempDir()                                 // …but its node cannot be opened
	uinputPath = filepath.Join(t.TempDir(), "missing-uinput") // root can open anything that exists
	t.Cleanup(func() { sysInput, devInputDir, uinputPath = oldSys, oldDev, oldUinput })

	perms := Permissions()
	var names []string
	for _, p := range perms {
		names = append(names, p.Name)
		if p.Label == "" || p.Why == "" {
			t.Errorf("%s: label and why are required", p.Name)
		}
	}
	if got := strings.Join(names, ","); got != "uinput,pad-input,pad-hidraw,recording" {
		t.Fatalf("names %s", got)
	}
	for _, p := range perms[:3] {
		if p.Granted {
			t.Errorf("%s must not be granted", p.Name)
		}
	}
	if PermAccessibility != "uinput" || PermInputMonitoring != "pad-input" {
		t.Error("main.go's permission names")
	}
}

func TestPermissionsAreGrantedWhileThePadIsAbsent(t *testing.T) {
	oldSys, oldDev := sysInput, devInputDir
	sysInput, devInputDir = t.TempDir(), t.TempDir()
	t.Cleanup(func() { sysInput, devInputDir = oldSys, oldDev })
	for _, p := range Permissions() {
		if (p.Name == PermInputMonitoring || p.Name == PermPadWrite) && !p.Granted {
			t.Errorf("%s: no pad, nothing to grant", p.Name)
		}
	}
}

func TestRequestRecordingExplainsTheInputGroup(t *testing.T) {
	err := RequestPermission(PermRecording)
	if err == nil || !strings.Contains(err.Error(), "usermod -aG input") {
		t.Fatalf("got %v", err)
	}
	if err := RequestPermission("nope"); err == nil {
		t.Fatal("unknown permission must fail")
	}
}

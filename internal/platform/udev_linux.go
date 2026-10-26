//go:build linux

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// udevInstallScript writes the rule and makes udev apply it to the devices
// already plugged in.
func udevInstallScript() string {
	return "cat > " + UdevRulePath + " <<'VCD_RULE'\n" + UdevRule + "VCD_RULE\nudevadm control --reload\nudevadm trigger\n"
}

// InstallUdevRule writes the rule as root: through pkexec's dialog in a
// graphical session, sudo otherwise.
func InstallUdevRule() error {
	tool := "sudo"
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("pkexec"); err == nil {
			tool = "pkexec"
		}
	}
	out, err := exec.Command(tool, "sh", "-c", udevInstallScript()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not install the udev rule with %s (%v: %s); as root, write it to %s and run `udevadm control --reload && udevadm trigger` (print it with `vibe-coder-deck udev`)",
			tool, err, strings.TrimSpace(string(out)), UdevRulePath)
	}
	return nil
}

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Relaunch starts this program again once this process has exited, for a
// caller that is about to quit (the app is single instance: opened while the
// old one is still shutting down, macOS would just activate the dying one).
// Inside a macOS bundle it reopens the bundle; otherwise it runs the same command.
func Relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Poll until this process is gone (at most 15 s), then a beat more.
	waitExit := fmt.Sprintf("i=0; while kill -0 %d 2>/dev/null && [ $i -lt 75 ]; do sleep 0.2; i=$((i+1)); done; sleep 0.3", os.Getpid())
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "windows":
		cmd = exec.Command("cmd", "/C", fmt.Sprintf(`timeout /t 1 >nul & start "" "%s" %s`, exe, strings.Join(os.Args[1:], " ")))
	case strings.Contains(exe, ".app/Contents/MacOS/"):
		bundle := exe[:strings.Index(exe, ".app/Contents/MacOS/")+len(".app")]
		cmd = exec.Command("/bin/sh", "-c", fmt.Sprintf("%s; open %q", waitExit, bundle))
	default:
		cmd = exec.Command("/bin/sh", append([]string{"-c", fmt.Sprintf("%s; exec %q \"$@\"", waitExit, exe), "sh"}, os.Args[1:]...)...)
	}
	return cmd.Start()
}

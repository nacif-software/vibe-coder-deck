//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.design/x/hotkey"
	"golang.org/x/sys/windows"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
)

// HotkeyModifiers is the macropad spelling of the modifiers dispatched slots
// send. The pad's cmd is the HID GUI key, which Windows sees as the Windows key.
const HotkeyModifiers = "ctrl+opt+cmd"

func hotkeyModifiers() []hotkey.Modifier {
	return []hotkey.Modifier{hotkey.ModCtrl, hotkey.ModAlt, hotkey.ModWin}
}

func permissionHint() string { return "" }

var sendKeysNames = map[string]string{
	"enter": "{ENTER}", "return": "{ENTER}", "tab": "{TAB}", "space": " ", "esc": "{ESC}", "escape": "{ESC}",
	"backspace": "{BACKSPACE}", "delete": "{BACKSPACE}", "forward-delete": "{DELETE}", "up": "{UP}", "down": "{DOWN}",
	"left": "{LEFT}", "right": "{RIGHT}", "home": "{HOME}", "end": "{END}", "pageup": "{PGUP}", "pagedown": "{PGDN}",
	"comma": ",", "period": ".", "minus": "-", "equal": "=", "slash": "/", "semicolon": ";",
}

var sendKeysModifiers = map[string]string{"ctrl": "^", "control": "^", "alt": "%", "opt": "%", "option": "%", "shift": "+"}

// SendKeysForCombo turns a pad-style combo into .NET SendKeys syntax. The
// Windows key has no SendKeys spelling, so cmd/win combos are refused.
func SendKeysForCombo(combo string) (string, error) {
	prefix := ""
	var keys []string
	for _, part := range strings.Split(combo, "+") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if m, ok := sendKeysModifiers[part]; ok {
			prefix += m
			continue
		}
		if part == "cmd" || part == "win" || part == "command" {
			return "", fmt.Errorf("combo %q: the Windows key cannot be sent as a macro step", combo)
		}
		keys = append(keys, part)
	}
	if len(keys) != 1 {
		return "", fmt.Errorf("combo %q needs exactly one key", combo)
	}
	key := keys[0]
	if named, ok := sendKeysNames[key]; ok {
		return prefix + named, nil
	}
	if strings.HasPrefix(key, "f") {
		if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 16 {
			return prefix + "{F" + strconv.Itoa(n) + "}", nil
		}
	}
	if len([]rune(key)) == 1 {
		return prefix + escapeSendKeys(key), nil
	}
	return "", fmt.Errorf("unknown key %q in %q", key, combo)
}

func escapeSendKeys(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '+', '^', '%', '~', '(', ')', '{', '}', '[', ']':
			b.WriteString("{" + string(r) + "}")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sendKeys(keys string) error {
	script := `Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait(` + psQuote(keys) + `)`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("SendKeys: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Runner runs scripts with the interpreter their extension asks for, shell
// commands with cmd.exe, and macros through .NET SendKeys.
type Runner struct{}

func (Runner) RunScript(path string) (int, string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ps1":
		return run(exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path))
	case ".bat", ".cmd":
		return run(exec.Command("cmd", "/C", path))
	default:
		return run(exec.Command(path))
	}
}

func (Runner) RunShell(command string) (int, string, error) {
	return run(exec.Command("cmd", "/C", command))
}

func (Runner) PlayMacro(ctx context.Context, steps []deck.Step) (int, string, error) {
	for i, step := range steps {
		if ctx.Err() != nil {
			return 0, "", nil // stopped: the key was released or the toggle pressed again
		}
		switch step.Type {
		case "keys":
			for _, part := range strings.Split(step.Value, ",") {
				if strings.TrimSpace(part) == "" {
					continue
				}
				keys, err := SendKeysForCombo(part)
				if err != nil {
					return 65, fmt.Sprintf("step %d: %v", i+1, err), nil
				}
				if err := sendKeys(keys); err != nil {
					return 1, fmt.Sprintf("step %d: %v", i+1, err), nil
				}
			}
		case "text":
			if err := sendKeys(escapeSendKeys(step.Value)); err != nil {
				return 1, fmt.Sprintf("step %d: %v", i+1, err), nil
			}
		case "launch":
			if err := exec.Command("cmd", "/C", "start", "", step.Value).Run(); err != nil {
				return 1, fmt.Sprintf("step %d: open %s: %v", i+1, step.Value, err), nil
			}
		case "delay":
			ms, _ := strconv.ParseFloat(step.Value, 64)
			select {
			case <-ctx.Done():
				return 0, "", nil
			case <-time.After(time.Duration(ms * float64(time.Millisecond))):
			}
		case "keydown", "keyup", "click":
			return 65, fmt.Sprintf("step %d: %s steps are not supported on Windows yet (use keys/text/delay)", i+1, step.Type), nil
		default:
			return 65, fmt.Sprintf("step %d: unknown type %q", i+1, step.Type), nil
		}
	}
	return 0, "", nil
}

func run(cmd *exec.Cmd) (int, string, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), string(out), nil
		}
		return 1, string(out), err
	}
	return 0, string(out), nil
}

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

// Frontmost is the executable name of the foreground window's process,
// without ".exe" (for example "Orca").
func Frontmost() (string, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", errors.New("no foreground window")
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return "", errors.New("foreground window has no process")
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	name := filepath.Base(windows.UTF16ToString(buf[:size]))
	return strings.TrimSuffix(name, filepath.Ext(name)), nil
}

// Notify shows a tray balloon; failures are ignored, it is only feedback.
func Notify(title, message string) {
	script := `Add-Type -AssemblyName System.Windows.Forms; $n = New-Object System.Windows.Forms.NotifyIcon; ` +
		`$n.Icon = [System.Drawing.SystemIcons]::Information; $n.Visible = $true; ` +
		`$n.ShowBalloonTip(4000, ` + psQuote("Macropad: "+title) + `, ` + psQuote(message) + `, 'Info'); Start-Sleep -Seconds 5; $n.Dispose()`
	_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script).Start()
}

// SendKeys types a pad action with SendKeys.
func (Runner) SendKeys(action string) error {
	for _, part := range strings.Split(action, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		keys, err := SendKeysForCombo(part)
		if err != nil {
			return err
		}
		if err := sendKeys(keys); err != nil {
			return err
		}
	}
	return nil
}

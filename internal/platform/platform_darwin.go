//go:build darwin

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.design/x/hotkey"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
)

// HotkeyModifiers is the macropad spelling of the modifiers dispatched slots
// send; the pad's cmd is the HID GUI key, so the same pad action works on
// Windows where it arrives as the Windows key.
const HotkeyModifiers = "ctrl+opt+cmd"

func hotkeyModifiers() []hotkey.Modifier {
	return []hotkey.Modifier{hotkey.ModCtrl, hotkey.ModOption, hotkey.ModCmd}
}

func permissionHint() string {
	return " (on macOS the hotkey listener needs Accessibility: System Settings > Privacy & Security > Accessibility, add this program, then start it again)"
}

var keyCodes = map[string]int{
	"enter": 36, "return": 36, "tab": 48, "space": 49, "esc": 53, "escape": 53,
	"backspace": 51, "delete": 51, "forward-delete": 117, "up": 126, "down": 125,
	"left": 123, "right": 124, "home": 115, "end": 119, "pageup": 116, "pagedown": 121,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98, "f8": 100,
	"f9": 101, "f10": 109, "f11": 103, "f12": 111,
}

var modifierNames = map[string]string{
	"cmd": "command", "command": "command", "opt": "option", "alt": "option", "option": "option",
	"ctrl": "control", "control": "control", "shift": "shift",
}

var namedKeys = map[string]string{
	"comma": ",", "period": ".", "minus": "-", "equal": "=", "slash": "/", "semicolon": ";",
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// AppleScriptForCombo turns a pad-style combo (cmd+shift+4, enter, ctrl+left)
// into the System Events statement that types it.
func AppleScriptForCombo(combo string) (string, error) {
	var mods []string
	var keys []string
	for _, part := range strings.Split(combo, "+") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if m, ok := modifierNames[part]; ok {
			mods = append(mods, m+" down")
		} else {
			keys = append(keys, part)
		}
	}
	if len(keys) != 1 {
		return "", fmt.Errorf("combo %q needs exactly one key", combo)
	}
	using := ""
	if len(mods) > 0 {
		using = " using {" + strings.Join(mods, ", ") + "}"
	}
	key := keys[0]
	if named, ok := namedKeys[key]; ok {
		key = named
	}
	if code, ok := keyCodes[key]; ok {
		return fmt.Sprintf("key code %d%s", code, using), nil
	}
	if len([]rune(key)) == 1 {
		return "keystroke " + quote(key) + using, nil
	}
	return "", fmt.Errorf("unknown key %q in %q", keys[0], combo)
}

func AppleScriptForText(text string) string { return "keystroke " + quote(text) }

func systemEvents(statement string) error {
	out, err := exec.Command("osascript", "-e", `tell application "System Events" to `+statement).CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Runner runs scripts and shell commands with bash and plays macros through
// System Events. System shortcuts (screenshots, Spotlight) ignore synthetic
// keystrokes: those belong in Keys mode, where the pad sends them itself.
type Runner struct{}

func (Runner) RunScript(path string) (int, string, error) {
	if info, err := os.Stat(path); err == nil && info.Mode()&0o111 != 0 {
		return run(exec.Command(path))
	}
	return run(exec.Command("/bin/bash", path))
}

func (Runner) RunShell(command string) (int, string, error) {
	return run(exec.Command("/bin/bash", "-c", command))
}

func (Runner) PlayMacro(ctx context.Context, steps []deck.Step) (int, string, error) {
	held := map[string]bool{}
	defer func() {
		// Never leave a modifier stuck down when a macro stops half way.
		for name, on := range held {
			if on {
				_ = PostKey(name, false, 0)
			}
		}
	}()
	for i, step := range steps {
		if ctx.Err() != nil {
			return 0, "", nil // stopped: the key was released or the toggle pressed again
		}
		switch step.Type {
		case "keydown", "keyup":
			down := step.Type == "keydown"
			name := strings.ToLower(strings.TrimSpace(step.Value))
			if IsModifier(name) {
				held[name] = down
			}
			if err := PostKey(name, down, ModifierFlags(held)); err != nil {
				return 65, fmt.Sprintf("step %d: %v", i+1, err), nil
			}
		case "click":
			if err := PostClick(step.Value); err != nil {
				return 65, fmt.Sprintf("step %d: %v", i+1, err), nil
			}
		case "keys":
			for _, part := range strings.Split(step.Value, ",") {
				if strings.TrimSpace(part) == "" {
					continue
				}
				if _, ok := MediaKey(part); ok { // play, next, volume-up…: not a keystroke
					err := PostKey(part, true, 0)
					if err == nil {
						err = PostKey(part, false, 0)
					}
					if err != nil {
						return 65, fmt.Sprintf("step %d: %v", i+1, err), nil
					}
					continue
				}
				statement, err := AppleScriptForCombo(part)
				if err != nil {
					return 65, fmt.Sprintf("step %d: %v", i+1, err), nil
				}
				if err := systemEvents(statement); err != nil {
					return 1, fmt.Sprintf("step %d: %v", i+1, err), nil
				}
			}
		case "text":
			if err := systemEvents(AppleScriptForText(step.Value)); err != nil {
				return 1, fmt.Sprintf("step %d: %v", i+1, err), nil
			}
		case "launch":
			if err := launchApp(step.Value); err != nil {
				return 1, fmt.Sprintf("step %d: %v", i+1, err), nil
			}
		case "delay":
			ms, _ := strconv.ParseFloat(step.Value, 64)
			select {
			case <-ctx.Done():
				return 0, "", nil
			case <-time.After(time.Duration(ms * float64(time.Millisecond))):
			}
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

var frontmostPattern = regexp.MustCompile(`"(?:LSDisplayName|name)"="([^"]*)"`)

// ParseFrontmost reads the app name out of `lsappinfo info -only name`.
func ParseFrontmost(output string) string {
	if m := frontmostPattern.FindStringSubmatch(output); m != nil {
		return m[1]
	}
	return ""
}

// Frontmost is the name of the app in front, as lsappinfo reports it.
func Frontmost() (string, error) {
	front, err := exec.Command("lsappinfo", "front").Output()
	if err != nil {
		return "", fmt.Errorf("lsappinfo front: %w", err)
	}
	info, err := exec.Command("lsappinfo", "info", "-only", "name", strings.TrimSpace(string(front))).Output()
	if err != nil {
		return "", fmt.Errorf("lsappinfo info: %w", err)
	}
	name := ParseFrontmost(string(info))
	if name == "" {
		return "", errors.New("lsappinfo gave no app name")
	}
	return name, nil
}

// Notify shows a macOS notification; failures are ignored, it is only feedback.
func Notify(title, message string) {
	script := fmt.Sprintf(`display notification %s with title "Macropad" subtitle %s`, quote(message), quote(title))
	_ = exec.Command("osascript", "-e", script).Run()
}

// launchApp opens an app by name ("Safari") or an app, file or URL by path.
func launchApp(target string) error {
	args := []string{target}
	if !strings.ContainsAny(target, "/:") {
		args = []string{"-a", target}
	}
	if out, err := exec.Command("open", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("open %s: %s", target, strings.TrimSpace(string(out)))
	}
	return nil
}

// padKeyNames maps the pad syntax's key names to the virtual key names here.
var padKeyNames = map[string]string{"lbracket": "[", "rbracket": "]", "grave": "`", "quote": "'", "backslash": "\\", "return": "enter", "escape": "esc"}

// SendKeys types a pad action as Quartz events at the HID level, like a real
// keyboard: comma-separated combos, media keys (play, volume-up…) and mouse
// buttons (click, right-click, middle-click).
func (Runner) SendKeys(action string) error {
	for _, part := range strings.Split(action, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		switch part {
		case "click", "right-click", "middle-click":
			if err := PostClick(map[string]string{"click": "left", "right-click": "right", "middle-click": "middle"}[part]); err != nil {
				return err
			}
			continue
		}
		tokens := strings.Split(part, "+")
		key := strings.TrimSpace(tokens[len(tokens)-1])
		if n, ok := padKeyNames[key]; ok {
			key = n
		}
		held := map[string]bool{}
		for _, m := range tokens[:len(tokens)-1] {
			m = strings.TrimSpace(m)
			if !IsModifier(m) {
				return fmt.Errorf("%q is not a modifier", m)
			}
			held[m] = true
			if err := PostKey(m, true, ModifierFlags(held)); err != nil {
				return err
			}
		}
		flags := ModifierFlags(held)
		err := PostKey(key, true, flags)
		if err == nil {
			err = PostKey(key, false, flags)
		}
		for m := range held {
			delete(held, m)
			PostKey(m, false, ModifierFlags(held))
		}
		if err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// HoldKeys presses or releases the modifiers of a modifier-only action, in
// combo order going down and in reverse coming up.
func (Runner) HoldKeys(action string, down bool) error {
	mods, err := holdModifiers(action)
	if err != nil {
		return err
	}
	held := map[string]bool{}
	if down {
		for _, m := range mods {
			held[m] = true
			if err := PostKey(m, true, ModifierFlags(held)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, m := range mods {
		held[m] = true
	}
	for i := len(mods) - 1; i >= 0; i-- {
		delete(held, mods[i])
		if err := PostKey(mods[i], false, ModifierFlags(held)); err != nil {
			return err
		}
	}
	return nil
}

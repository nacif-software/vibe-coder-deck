//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/nacif-software/vibe-coder-deck/internal/deck"
)

// HotkeyModifiers is the macropad spelling of the modifiers dispatched slots
// send. The pad's cmd is the HID GUI key, which Linux calls Meta (Super).
const HotkeyModifiers = "ctrl+opt+cmd"

// Runner runs scripts and shell commands with bash (sh without it) and
// types macros through the uinput virtual keyboard.
type Runner struct{}

func shell() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}

func (Runner) RunScript(path string) (int, string, error) {
	if info, err := os.Stat(path); err == nil && info.Mode()&0o111 != 0 {
		return run(exec.Command(path))
	}
	return run(exec.Command(shell(), path))
}

func (Runner) RunShell(command string) (int, string, error) {
	return run(exec.Command(shell(), "-c", command))
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

// clickNames are the pad's mouse actions by button.
var clickNames = map[string]string{"click": "left", "right-click": "right", "middle-click": "middle"}

// sendCombo types one combo ("ctrl+shift+t", "volume-up", "right-click"):
// modifiers down in order, the key down and up, modifiers up in reverse.
func sendCombo(combo string) error {
	combo = strings.ToLower(strings.TrimSpace(combo))
	if button, ok := clickNames[combo]; ok {
		return PostClick(button)
	}
	tokens := strings.Split(combo, "+")
	key := strings.TrimSpace(tokens[len(tokens)-1])
	mods := tokens[:len(tokens)-1]
	for _, m := range mods {
		if !IsModifier(m) {
			return fmt.Errorf("%q is not a modifier", strings.TrimSpace(m))
		}
	}
	if _, err := KeyCode(key); err != nil {
		return err
	}
	for _, m := range mods {
		if err := PostKey(m, true); err != nil {
			return err
		}
	}
	err := PostKey(key, true)
	if err == nil {
		err = PostKey(key, false)
	}
	for i := len(mods) - 1; i >= 0; i-- {
		if e := PostKey(mods[i], false); err == nil {
			err = e
		}
	}
	return err
}

// SendKeys types a pad action like a real keyboard: comma-separated combos,
// media keys (play, volume-up…) and mouse buttons (click, right-click, middle-click).
func (Runner) SendKeys(action string) error {
	for _, part := range strings.Split(action, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if err := sendCombo(part); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func (Runner) PlayMacro(ctx context.Context, steps []deck.Step) (int, string, error) {
	held := map[string]bool{}
	defer func() {
		// Never leave a modifier stuck down when a macro stops half way.
		for name, on := range held {
			if on {
				_ = PostKey(name, false)
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
				held[canonicalKey(name)] = down
			}
			if err := PostKey(name, down); err != nil {
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
				if err := sendCombo(part); err != nil {
					return 65, fmt.Sprintf("step %d: %v", i+1, err), nil
				}
			}
		case "text":
			if err := typeText(step.Value); err != nil {
				return 65, fmt.Sprintf("step %d: %v", i+1, err), nil
			}
		case "launch":
			if err := launch(step.Value); err != nil {
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

// launch starts an executable by path, or opens a file, URL or app name
// with xdg-open.
func launch(target string) error {
	if info, err := os.Stat(target); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return exec.Command(target).Start()
	}
	if out, err := exec.Command("xdg-open", target).CombinedOutput(); err != nil {
		return fmt.Errorf("xdg-open %s: %s", target, strings.TrimSpace(string(out)))
	}
	return nil
}

// Notify shows a desktop notification through notify-send; failures are
// ignored, it is only feedback.
func Notify(title, message string) {
	_ = exec.Command("notify-send", "-a", "Vibe Coder Deck", "Macropad: "+title, message).Start()
}

// Frontmost is the name of the app in front, when the session can tell.
func Frontmost() (string, error) {
	_, name, _ := FrontmostApp()
	if name == "" {
		return "", errors.New("the app in front is unknown (no X11 session)")
	}
	return name, nil
}

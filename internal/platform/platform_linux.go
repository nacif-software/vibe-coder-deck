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

// launch starts what a "launch" step names: an installed app (by name, id
// or .desktop file), an executable (by path or name), or a file or URL
// through xdg-open.
func launch(target string) error {
	args, err := launchArgs(target, installedEntries())
	if err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	if args[0] == "xdg-open" {
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("xdg-open %s: %s", target, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", args[0], err)
	}
	go cmd.Wait() // reap it; the app outlives the step
	return nil
}

// launchArgs is the command line for a launch target: a .desktop file's Exec
// (field codes dropped), an installed app matched by name or id, an
// executable by path or name, else xdg-open for files and URLs. A bare name
// that matches nothing is an error, not a document to open.
func launchArgs(target string, entries []desktopEntry) ([]string, error) {
	target = strings.TrimSpace(target)
	if strings.HasSuffix(target, ".desktop") {
		r, err := os.Open(target)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		e, ok := parseDesktopEntry(target, r)
		if !ok || e.Exec == "" {
			return nil, fmt.Errorf("%s is not a launchable application entry", target)
		}
		return execArgs(e.Exec)
	}
	if !strings.ContainsAny(target, "/:") {
		for _, e := range entries {
			if strings.EqualFold(e.Name, target) || strings.EqualFold(e.ID, target) {
				return execArgs(e.Exec)
			}
		}
		if path, err := exec.LookPath(target); err == nil {
			return []string{path}, nil
		}
		return nil, fmt.Errorf("no application or command named %q", target)
	}
	if info, err := os.Stat(target); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return []string{target}, nil
	}
	return []string{"xdg-open", target}, nil
}

// execArgs splits a .desktop Exec line (double quotes group, backslash
// escapes inside them) and drops its field codes (%f, %U, …).
func execArgs(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inQuote, inToken := false, false
	flush := func() {
		if inToken {
			args = append(args, cur.String())
			cur.Reset()
			inToken = false
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inQuote && c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case c == '"':
			inQuote = !inQuote
			inToken = true
		case !inQuote && (c == ' ' || c == '\t'):
			flush()
		case !inQuote && c == '%' && i+1 < len(line):
			i++
			if line[i] == '%' {
				cur.WriteByte('%')
				inToken = true
			} else if cur.Len() == 0 {
				inToken = false // a bare field code: drop the token
			}
		default:
			cur.WriteByte(c)
			inToken = true
		}
	}
	flush()
	if len(args) == 0 {
		return nil, fmt.Errorf("empty Exec line %q", line)
	}
	return args, nil
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

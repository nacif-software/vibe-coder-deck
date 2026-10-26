// Package pad writes slots into the vibe-coder-keyboard macropad.
package pad

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotConnected is returned when the pad is not plugged in. A sync stops at
// the first one: nothing else can be written either.
var ErrNotConnected = errors.New("pad not connected")

// Writer programs one slot of the pad.
type Writer interface {
	Set(slot, action string) error
	Clear(slot string) error
}

// CLI writes the pad through the `macropad` command of vibe-coder-keyboard,
// which already speaks the pad's USB protocol and keeps the layout file.
type CLI struct {
	Path string
	Exec func(name string, args ...string) (output []byte, exitCode int, err error)
}

func NewCLI(path string) *CLI {
	if path == "" {
		path = "macropad"
	}
	return &CLI{Path: path, Exec: run}
}

func run(name string, args ...string) ([]byte, int, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return out, exitErr.ExitCode(), nil
		}
		return out, -1, err
	}
	return out, 0, nil
}

func (c *CLI) Set(slot, action string) error { return c.call("set", slot, action) }

func (c *CLI) Clear(slot string) error { return c.call("clear", slot) }

func (c *CLI) call(args ...string) error {
	out, code, err := c.Exec(c.Path, append(args, "--json")...)
	if err != nil {
		return fmt.Errorf("macropad %s: %w", args[0], err)
	}
	switch code {
	case 0:
		return nil
	case 2:
		return ErrNotConnected
	default:
		return fmt.Errorf("macropad %s: %s", strings.Join(args, " "), message(out, code))
	}
}

func message(out []byte, code int) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(out, &payload) == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return fmt.Sprintf("exit %d", code)
}

//go:build linux

package app

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nacif-software/vibe-coder-deck/internal/update"
)

func hasCommand(name string) bool { _, err := exec.LookPath(name); return err == nil }

type stubDeck struct{}

func (stubDeck) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<title>stub</title>ok")) })
}
func (stubDeck) URL() string              { return "http://127.0.0.1:7071" }
func (stubDeck) Sync() (string, error)    { return "", errors.New("stub") }
func (stubDeck) HotkeyError() error       { return nil }
func (stubDeck) SetRestart(func())        {}
func (stubDeck) Close()                   {}
func (stubDeck) Updater() *update.Updater { return nil }

// TestMain doubles as the app under test: GTK wants the main thread, which
// only TestMain has, so the window test re-runs this binary with
// VCD_APP_SMOKE=1 and it becomes the desktop app with a stub deck.
func TestMain(m *testing.M) {
	if os.Getenv("VCD_APP_SMOKE") == "1" {
		err := Run(Options{
			Start:            func() (Deck, error) { return stubDeck{}, nil },
			Notify:           func(string, string) {},
			LoginItemEnabled: func() bool { return false },
			SetLoginItem:     func(bool) error { return nil },
			ShowOnStart:      true,
			DockVisible:      func(bool) {},
			Relaunch:         func() error { return nil },
			Version:          "test",
		})
		if err != nil {
			os.Stderr.WriteString("Run: " + err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestRunOpensAWindowOnX11 starts the desktop app (GTK4 + WebKitGTK) under
// an X server and looks for its window with xdotool.
func TestRunOpensAWindowOnX11(t *testing.T) {
	for what, missing := range map[string]bool{"no DISPLAY": os.Getenv("DISPLAY") == "", "no xdotool": !hasCommand("xdotool")} {
		if missing {
			if os.Getenv("VCD_REQUIRE_DEVICES") == "1" {
				t.Fatal(what + " (VCD_REQUIRE_DEVICES=1)")
			}
			t.Skip(what)
		}
	}
	child := exec.Command(os.Args[0], "-test.run=^$")
	child.Env = append(os.Environ(), "VCD_APP_SMOKE=1")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	for i := 0; i < 150; i++ {
		select {
		case err := <-exited:
			t.Fatalf("the app exited before showing a window: %v", err)
		default:
		}
		out, _ := exec.Command("xdotool", "search", "--name", "Vibe Coder Deck").Output()
		if strings.TrimSpace(string(out)) != "" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	tree, _ := exec.Command("xwininfo", "-root", "-tree").CombinedOutput()
	t.Fatalf("no window named Vibe Coder Deck after 30 s; X windows:\n%s", tree)
}

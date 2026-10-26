package pad

import (
	"errors"
	"strings"
	"testing"
)

func TestCLIBuildsMacropadCommands(t *testing.T) {
	var got []string
	cli := &CLI{Path: "macropad", Exec: func(name string, args ...string) ([]byte, int, error) {
		got = append(got, name+" "+strings.Join(args, " "))
		return []byte(`{"ok": true}`), 0, nil
	}}
	if err := cli.Set("key1", "ctrl+opt+cmd+1"); err != nil {
		t.Fatal(err)
	}
	if err := cli.Clear("key2"); err != nil {
		t.Fatal(err)
	}
	if got[0] != "macropad set key1 ctrl+opt+cmd+1 --json" || got[1] != "macropad clear key2 --json" {
		t.Fatalf("commands = %v", got)
	}
}

func TestCLIMapsExitCodes(t *testing.T) {
	cli := &CLI{Path: "macropad", Exec: func(string, ...string) ([]byte, int, error) {
		return []byte(`{"ok": false, "error": {"message": "pad not connected"}}`), 2, nil
	}}
	if err := cli.Set("key1", "esc"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("exit 2 should be ErrNotConnected, got %v", err)
	}
	cli.Exec = func(string, ...string) ([]byte, int, error) {
		return []byte(`{"ok": false, "error": {"message": "unknown key 'foo'"}}`), 1, nil
	}
	err := cli.Set("key1", "foo")
	if err == nil || !strings.Contains(err.Error(), "unknown key 'foo'") {
		t.Fatalf("exit 1 should surface the message, got %v", err)
	}
}

//go:build linux

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLaunchArgs(t *testing.T) {
	dir := t.TempDir()
	desktop := filepath.Join(dir, "my-app.desktop")
	os.WriteFile(desktop, []byte("[Desktop Entry]\nType=Application\nName=My App\nExec=\"/opt/My App/bin/app\" --flag %f %U\n"), 0o644)
	script := filepath.Join(dir, "run.sh")
	os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755)
	doc := filepath.Join(dir, "notes.txt")
	os.WriteFile(doc, []byte("x"), 0o644)
	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	entries := []desktopEntry{
		{ID: "org.mozilla.firefox", Name: "Firefox", Exec: "firefox %u"},
		{ID: "code", Name: "Visual Studio Code", Exec: "/usr/share/code/code --unity-launch %F"},
	}
	cases := []struct {
		target string
		want   []string
	}{
		{"Firefox", []string{"firefox"}},
		{"firefox", []string{"firefox"}},
		{"code", []string{"/usr/share/code/code", "--unity-launch"}},
		{"Visual Studio Code", []string{"/usr/share/code/code", "--unity-launch"}},
		{desktop, []string{"/opt/My App/bin/app", "--flag"}},
		{script, []string{script}},
		{"sh", []string{shPath}},
		{doc, []string{"xdg-open", doc}},
		{"https://example.com", []string{"xdg-open", "https://example.com"}},
	}
	for _, c := range cases {
		got, err := launchArgs(c.target, entries)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("launchArgs(%q) = %v %v, want %v", c.target, got, err, c.want)
		}
	}
	if _, err := launchArgs("no-such-app-xyz", entries); err == nil {
		t.Error("an unknown name must fail, not open a document")
	}
}

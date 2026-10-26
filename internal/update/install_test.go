package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipEntry is one file of a test zip; mode 0 means a plain file.
type zipEntry struct {
	name, body string
	mode       os.FileMode
}

func writeZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// bundleZip is a release zip as ditto writes it: the bundle plus __MACOSX junk.
func bundleZip(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "Vibe-Coder-Deck-v0.2.0-macos.zip")
	writeZip(t, path, []zipEntry{
		{name: "Vibe Coder Deck.app/", mode: 0o755 | os.ModeDir},
		{name: "Vibe Coder Deck.app/Contents/Info.plist", body: "<plist/>"},
		{name: "Vibe Coder Deck.app/Contents/MacOS/vibe-coder-deck", body: body, mode: 0o755},
		{name: "__MACOSX/Vibe Coder Deck.app/._Contents", body: "junk"},
	})
	return path
}

func oldBundle(t *testing.T, dir string) string {
	t.Helper()
	app := filepath.Join(dir, "Vibe Coder Deck.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "vibe-coder-deck"), []byte("old"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "stale"), []byte("x"), 0o644)
	return app
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallReplacesTheBundle(t *testing.T) {
	dir := t.TempDir()
	app := oldBundle(t, dir)
	if err := Install(bundleZip(t, dir, "new"), app); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(app, "Contents", "MacOS", "vibe-coder-deck")
	if readFile(t, bin) != "new" {
		t.Fatal("binary not replaced")
	}
	if st, _ := os.Stat(bin); st.Mode()&0o100 == 0 {
		t.Fatalf("exec bit lost: %v", st.Mode())
	}
	if _, err := os.Stat(filepath.Join(app, "Contents", "stale")); err == nil {
		t.Fatal("the old bundle's files survived")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "Vibe Coder Deck.app" && !strings.HasSuffix(e.Name(), ".zip") {
			t.Fatalf("leftover %q", e.Name())
		}
	}
}

func TestInstallRefusesEntriesOutsideTheBundle(t *testing.T) {
	dir := t.TempDir()
	app := oldBundle(t, dir)
	zp := filepath.Join(dir, "evil.zip")
	writeZip(t, zp, []zipEntry{{name: "Vibe Coder Deck.app/Contents/MacOS/vibe-coder-deck", body: "new", mode: 0o755}, {name: "../evil", body: "x"}})
	err := Install(zp, app)
	if err == nil || !strings.Contains(err.Error(), "evil") {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, filepath.Join(app, "Contents", "MacOS", "vibe-coder-deck")) != "old" {
		t.Fatal("target touched")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("wrote outside the bundle")
	}
}

func TestInstallLeavesTheOldBundleWhenItCannotBeMoved(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	app := oldBundle(t, dir)
	zp := bundleZip(t, dir, "new")
	parent := filepath.Join(dir, "locked")
	os.Mkdir(parent, 0o755)
	locked := filepath.Join(parent, "Vibe Coder Deck.app")
	os.Rename(app, locked)
	os.Chmod(parent, 0o555)
	t.Cleanup(func() { os.Chmod(parent, 0o755) })
	if err := Install(zp, locked); err == nil {
		t.Fatal("expected an error")
	}
	if readFile(t, filepath.Join(locked, "Contents", "MacOS", "vibe-coder-deck")) != "old" {
		t.Fatal("old bundle gone")
	}
}

func TestInstallReplacesAnExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "vibe-coder-deck.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	zp := filepath.Join(dir, "vibe-coder-deck-v0.2.0-windows-amd64.zip")
	writeZip(t, zp, []zipEntry{{name: "vibe-coder-deck.exe", body: "new", mode: 0o755}})
	if err := Install(zp, exe); err != nil {
		t.Fatal(err)
	}
	if readFile(t, exe) != "new" {
		t.Fatal("exe not replaced")
	}
	CleanOld(exe)
	if _, err := os.Stat(exe + ".old"); err == nil {
		t.Fatal(".old still there")
	}
}

func TestTargetFromExecutable(t *testing.T) {
	// A bare binary is only an installed app on Linux (see TestTargetForLinuxSkipsGoBuildDirs).
	cases := map[string]string{
		"/Applications/Vibe Coder Deck.app/Contents/MacOS/vibe-coder-deck": "/Applications/Vibe Coder Deck.app",
		`C:\Users\me\AppData\Local\vibe-coder-deck.exe`:                    `C:\Users\me\AppData\Local\vibe-coder-deck.exe`,
		"/usr/local/bin/vibe-coder-deck":                                   "",
		"":                                                                 "",
	}
	for in, want := range cases {
		if got := targetFor(in, "darwin"); got != want {
			t.Errorf("targetFor(%q, darwin) = %q, want %q", in, got, want)
		}
	}
}

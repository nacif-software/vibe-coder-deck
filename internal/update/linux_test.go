package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestPickAssetLinuxByArch(t *testing.T) {
	assets := []Asset{{Name: "Vibe-Coder-Deck-v0.2.0-macos.zip"}, {Name: "vibe-coder-deck-v0.2.0-windows-amd64.zip"}, {Name: "vibe-coder-deck-v0.2.0-linux-amd64.zip"}, {Name: "checksums.txt"}}
	if a, ok := PickAsset(assets, "linux", "amd64"); !ok || a.Name != "vibe-coder-deck-v0.2.0-linux-amd64.zip" {
		t.Fatalf("linux/amd64: %+v %v", a, ok)
	}
	if _, ok := PickAsset(assets, "linux", "arm64"); ok {
		t.Fatal("no arm64 asset")
	}
	if a, ok := PickAsset(assets, "darwin", "arm64"); !ok || a.Name != "Vibe-Coder-Deck-v0.2.0-macos.zip" {
		t.Fatalf("darwin: %+v %v", a, ok)
	}
	if a, ok := PickAsset(assets, "windows", "amd64"); !ok || a.Name != "vibe-coder-deck-v0.2.0-windows-amd64.zip" {
		t.Fatalf("windows: %+v %v", a, ok)
	}
}

func TestTargetForLinuxSkipsGoBuildDirs(t *testing.T) {
	cases := map[[2]string]string{
		{"/usr/local/bin/vibe-coder-deck", "linux"}:             "/usr/local/bin/vibe-coder-deck",
		{"/home/u/.local/bin/vibe-coder-deck", "linux"}:         "/home/u/.local/bin/vibe-coder-deck",
		{"/tmp/go-build123/b001/exe/main", "linux"}:             "",
		{"/home/u/.cache/go-build/ab/cd-d/exe/x", "linux"}:      "",
		{"/Applications/X.app/Contents/MacOS/x", "darwin"}:      "/Applications/X.app",
		{"/usr/bin/x", "darwin"}:                                "",
		{`C:\Program Files\VCD\vibe-coder-deck.exe`, "windows"}: `C:\Program Files\VCD\vibe-coder-deck.exe`,
	}
	for in, want := range cases {
		if got := targetFor(in[0], in[1]); got != want {
			t.Errorf("targetFor(%s, %s) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestInstallReplacesABareBinary(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "vibe-coder-deck-v0.2.0-linux-amd64.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, body := range map[string]string{"vibe-coder-deck": "new binary", "README.md": "read me", "70-vibe-coder-deck.rules": "rule"} {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o644)
		if name == "vibe-coder-deck" {
			h.SetMode(0o755)
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(body))
	}
	w.Close()
	f.Close()
	target := filepath.Join(dir, "bin", "vibe-coder-deck")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("old binary"), 0o755)
	if err := Install(zipPath, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new binary" {
		t.Fatalf("target: %q %v", got, err)
	}
	if info, _ := os.Stat(target); info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("not executable: %v", info.Mode())
	}
	if _, err := os.Stat(target + ".old"); err == nil {
		t.Fatal(".old left behind")
	}
	if entries, _ := os.ReadDir(filepath.Dir(target)); len(entries) != 1 {
		t.Fatalf("stray files next to the binary: %v", entries)
	}
}

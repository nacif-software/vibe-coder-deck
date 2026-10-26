package update

import "testing"

func TestPickAssetChoosesTheFileForThisOS(t *testing.T) {
	assets := []Asset{{Name: "checksums.txt"}, {Name: "Vibe-Coder-Deck-v0.2.0-macos.zip"}, {Name: "vibe-coder-deck-v0.2.0-windows-amd64.zip"}}
	if a, ok := PickAsset(assets, "darwin"); !ok || a.Name != "Vibe-Coder-Deck-v0.2.0-macos.zip" {
		t.Fatalf("darwin = %+v %v", a, ok)
	}
	if a, ok := PickAsset(assets, "windows"); !ok || a.Name != "vibe-coder-deck-v0.2.0-windows-amd64.zip" {
		t.Fatalf("windows = %+v %v", a, ok)
	}
	if _, ok := PickAsset(assets, "linux"); ok {
		t.Fatal("linux has no asset")
	}
}

func TestChecksumForFindsTheHashOfAFile(t *testing.T) {
	text := "abc123  Vibe-Coder-Deck-v0.2.0-macos.zip\ndef456 *vibe-coder-deck-v0.2.0-windows-amd64.zip\n\n789  dist/other.zip\n"
	if h, ok := ChecksumFor(text, "vibe-coder-deck-v0.2.0-windows-amd64.zip"); !ok || h != "def456" {
		t.Fatalf("windows = %q %v", h, ok)
	}
	if h, ok := ChecksumFor(text, "Vibe-Coder-Deck-v0.2.0-macos.zip"); !ok || h != "abc123" {
		t.Fatalf("mac = %q %v", h, ok)
	}
	if h, ok := ChecksumFor(text, "other.zip"); !ok || h != "789" {
		t.Fatalf("path = %q %v", h, ok)
	}
	if _, ok := ChecksumFor(text, "missing.zip"); ok {
		t.Fatal("missing found")
	}
}

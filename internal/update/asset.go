package update

import (
	"path"
	"strings"
)

// PickAsset is the release file for an operating system: the macOS bundle zip
// ("-macos") or the Windows zip ("-windows").
func PickAsset(assets []Asset, goos string) (Asset, bool) {
	var mark string
	switch goos {
	case "darwin":
		mark = "-macos"
	case "windows":
		mark = "-windows"
	default:
		return Asset{}, false
	}
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, mark) && strings.HasSuffix(name, ".zip") {
			return a, true
		}
	}
	return Asset{}, false
}

// ChecksumFor finds a file's hash in a checksums.txt ("<hex>  <name>" per
// line, as shasum writes it; a "*" binary marker and directories are ignored).
func ChecksumFor(text, name string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		file := strings.TrimPrefix(fields[len(fields)-1], "*")
		if path.Base(file) == name {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

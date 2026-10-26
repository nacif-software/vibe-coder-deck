package update

import (
	"path"
	"strings"
)

// PickAsset is the release file for a system: the macOS bundle zip
// ("-macos"), the Windows zip ("-windows") or the Linux zip for the
// architecture ("-linux-amd64").
func PickAsset(assets []Asset, goos, goarch string) (Asset, bool) {
	var mark string
	switch goos {
	case "darwin":
		mark = "-macos"
	case "windows":
		mark = "-windows"
	case "linux":
		mark = "-linux-" + goarch
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

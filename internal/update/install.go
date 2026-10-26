package update

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// TargetFromExecutable is what an update replaces: the .app bundle that holds
// the running binary on macOS, the .exe on Windows, the binary itself on
// Linux; "" when the program runs outside an installed app (go run, a bare
// binary on macOS).
func TargetFromExecutable(exe string) string { return targetFor(exe, runtime.GOOS) }

func targetFor(exe, goos string) string {
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		return exe[:i+len(".app")]
	}
	if strings.HasSuffix(strings.ToLower(exe), ".exe") {
		return exe
	}
	if goos == "linux" && exe != "" && !strings.Contains(exe, "go-build") {
		return exe
	}
	return ""
}

// Install replaces target (a .app bundle or an .exe) with the one inside the
// zip: the archive is unpacked next to the target, the current copy moved
// aside, the new one moved into place and the old removed. If the new one
// cannot be moved in, the old is put back.
func Install(zipPath, target string) error {
	if target == "" {
		return errors.New("no installed app to replace")
	}
	tmp, err := os.MkdirTemp(filepath.Dir(target), ".vibe-coder-deck-update-")
	if err != nil {
		return fmt.Errorf("preparing the update next to the app: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := extractZip(zipPath, tmp); err != nil {
		return err
	}
	fresh, err := findPayload(tmp, target)
	if err != nil {
		return err
	}
	return swap(fresh, target)
}

// CleanOld removes the previous version an update left behind (Windows
// cannot delete a running exe, only rename it; the next start deletes it).
func CleanOld(target string) {
	if target != "" {
		os.RemoveAll(target + ".old")
	}
}

// findPayload is the .app directory, the .exe file or (a bare Linux binary)
// the file named like the target inside an unpacked download.
func findPayload(dir, target string) (string, error) {
	wantDir, ext := strings.HasSuffix(strings.ToLower(target), ".app"), ".exe"
	if wantDir {
		ext = ".app"
	} else if !strings.HasSuffix(strings.ToLower(target), ".exe") {
		ext = filepath.Base(target)
	}
	var found string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "__MACOSX" {
			return fs.SkipDir
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ext) && d.IsDir() == wantDir {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("no %s inside the download", ext)
	}
	return found, nil
}

func swap(fresh, target string) error {
	old := target + ".old"
	os.RemoveAll(old)
	if _, err := os.Lstat(target); err == nil {
		if err := os.Rename(target, old); err != nil {
			return fmt.Errorf("moving the current app aside: %w", err)
		}
	}
	if err := os.Rename(fresh, target); err != nil {
		os.Rename(old, target) // put the current app back
		return fmt.Errorf("moving the new app into place: %w", err)
	}
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
		os.Chmod(target, 0o755) // a bare binary must stay executable whatever the zip said
	}
	os.RemoveAll(old) // a running exe on Windows stays until the next start
	return nil
}

// extractZip unpacks an archive into dir, keeping file modes and symlinks,
// skipping ditto's __MACOSX resource forks and refusing paths that would land
// outside dir.
func extractZip(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening the download: %w", err)
	}
	defer zr.Close()
	root := filepath.Clean(dir) + string(filepath.Separator)
	for _, f := range zr.File {
		if f.Name == "__MACOSX" || strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(dst, root) {
			return fmt.Errorf("refusing %q: it points outside the archive", f.Name)
		}
		mode := f.Mode()
		switch {
		case mode.IsDir() || strings.HasSuffix(f.Name, "/"):
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			link, err := readEntry(f)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(string(link), dst); err != nil {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			perm := mode.Perm()
			if perm == 0 {
				perm = 0o644
			}
			if err := writeEntry(f, dst, perm|0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func readEntry(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 1<<16))
}

func writeEntry(f *zip.File, dst string, perm os.FileMode) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

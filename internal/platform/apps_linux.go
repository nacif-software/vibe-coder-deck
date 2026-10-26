//go:build linux

package platform

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// InstalledApp is an application the user can bind a profile to: on Linux
// a .desktop entry (BundleID is its id, Path the file), or a bare executable.
type InstalledApp struct {
	Name     string `json:"name"`
	BundleID string `json:"bundle_id"`
	Path     string `json:"path"`
}

// desktopEntry is what matters of a .desktop file.
type desktopEntry struct {
	ID, Path, Name, Exec, Icon, WMClass string
	NoDisplay                           bool
}

// parseDesktopEntry reads the [Desktop Entry] group of a .desktop file; ok
// is false unless it is an application with a name.
func parseDesktopEntry(path string, r io.Reader) (desktopEntry, bool) {
	e := desktopEntry{ID: strings.TrimSuffix(filepath.Base(path), ".desktop"), Path: path}
	typ, inEntry := "", false
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "Type":
			typ = value
		case "Name":
			e.Name = value
		case "Exec":
			e.Exec = value
		case "Icon":
			e.Icon = value
		case "StartupWMClass":
			e.WMClass = value
		case "NoDisplay", "Hidden":
			if value == "true" {
				e.NoDisplay = true
			}
		}
	}
	return e, typ == "Application" && e.Name != ""
}

// dataDirs are the XDG data directories, the user's first.
func dataDirs() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".local", "share")
		}
	}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	dirs := []string{}
	if home != "" {
		dirs = append(dirs, home)
	}
	for _, d := range strings.Split(sys, ":") {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

var appsCache struct {
	mu      sync.Mutex
	key     string
	at      time.Time
	entries []desktopEntry
}

// installedEntries lists the .desktop applications, the first id found
// winning (the user's directory shadows the system's), sorted by name.
func installedEntries() []desktopEntry {
	dirs := dataDirs()
	key := strings.Join(dirs, ":")
	appsCache.mu.Lock()
	defer appsCache.mu.Unlock()
	if appsCache.key == key && time.Since(appsCache.at) < 30*time.Second {
		return appsCache.entries
	}
	seen := map[string]bool{}
	var entries []desktopEntry
	for _, dir := range dirs {
		files, _ := os.ReadDir(filepath.Join(dir, "applications"))
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".desktop") {
				continue
			}
			path := filepath.Join(dir, "applications", f.Name())
			r, err := os.Open(path)
			if err != nil {
				continue
			}
			e, ok := parseDesktopEntry(path, r)
			r.Close()
			if !ok || seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name) })
	appsCache.key, appsCache.at, appsCache.entries = key, time.Now(), entries
	return entries
}

func toInstalled(e desktopEntry) InstalledApp {
	return InstalledApp{Name: e.Name, BundleID: e.ID, Path: e.Path}
}

// InstalledApps are the applications that show in menus.
func InstalledApps() []InstalledApp {
	var out []InstalledApp
	for _, e := range installedEntries() {
		if !e.NoDisplay {
			out = append(out, toInstalled(e))
		}
	}
	return out
}

// AppInfo describes a .desktop file, or a bare executable by its name.
func AppInfo(path string) (InstalledApp, error) {
	if strings.HasSuffix(path, ".desktop") {
		r, err := os.Open(path)
		if err != nil {
			return InstalledApp{}, err
		}
		defer r.Close()
		e, ok := parseDesktopEntry(path, r)
		if !ok {
			return InstalledApp{}, fmt.Errorf("%s is not an application entry", path)
		}
		return toInstalled(e), nil
	}
	name := filepath.Base(path)
	return InstalledApp{Name: name, BundleID: name, Path: path}, nil
}

// ChooseApp would open a file chooser; on Linux the installed list is the way.
func ChooseApp() (InstalledApp, bool, error) {
	return InstalledApp{}, false, errors.New("choosing a file is not supported on Linux; pick an installed app")
}

var iconSizes = []int{16, 22, 24, 32, 48, 64, 96, 128, 256, 512}

// findIcon resolves an Icon= value to a PNG: an absolute path as is, else
// hicolor (then any theme) at the smallest size that is at least size (the
// largest when none is), else pixmaps.
func findIcon(icon string, size int, dirs []string) (string, bool) {
	if filepath.IsAbs(icon) {
		if strings.HasSuffix(strings.ToLower(icon), ".png") {
			_, err := os.Stat(icon)
			return icon, err == nil
		}
		return "", false
	}
	for _, dir := range dirs {
		themes, _ := os.ReadDir(filepath.Join(dir, "icons"))
		names := []string{"hicolor"}
		for _, t := range themes {
			if t.IsDir() && t.Name() != "hicolor" {
				names = append(names, t.Name())
			}
		}
		for _, theme := range names {
			var found string
			var foundSize int
			for _, s := range iconSizes {
				p := filepath.Join(dir, "icons", theme, strconv.Itoa(s)+"x"+strconv.Itoa(s), "apps", icon+".png")
				if _, err := os.Stat(p); err != nil {
					continue
				}
				if found == "" || (foundSize < size && s > foundSize) || (s >= size && s < foundSize) {
					found, foundSize = p, s
				}
			}
			if found != "" {
				return found, true
			}
		}
	}
	for _, dir := range dirs {
		p := filepath.Join(dir, "pixmaps", icon+".png")
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// AppIconPNG is the PNG icon of a .desktop entry at about size pixels.
func AppIconPNG(path string, size int) ([]byte, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	e, ok := parseDesktopEntry(path, r)
	if !ok || e.Icon == "" {
		return nil, fmt.Errorf("%s has no icon", path)
	}
	p, ok := findIcon(e.Icon, size, dataDirs())
	if !ok {
		return nil, fmt.Errorf("no PNG icon named %q", e.Icon)
	}
	return os.ReadFile(p)
}

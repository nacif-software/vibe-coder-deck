//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const firefoxDesktop = `[Desktop Entry]
Version=1.0
Name=Firefox
Comment=Browse the Web
Exec=firefox %u
Icon=firefox
Terminal=false
Type=Application
StartupWMClass=firefox
NoDisplay=false

[Desktop Action new-window]
Name=Open a New Window
Exec=firefox --new-window
`

func TestParseDesktopEntry(t *testing.T) {
	e, ok := parseDesktopEntry("/usr/share/applications/org.mozilla.firefox.desktop", strings.NewReader(firefoxDesktop))
	if !ok {
		t.Fatal("not parsed")
	}
	want := desktopEntry{ID: "org.mozilla.firefox", Path: "/usr/share/applications/org.mozilla.firefox.desktop", Name: "Firefox", Exec: "firefox %u", Icon: "firefox", WMClass: "firefox"}
	if e != want {
		t.Fatalf("got %+v\nwant %+v", e, want)
	}
	if _, ok := parseDesktopEntry("x.desktop", strings.NewReader("[Desktop Entry]\nType=Link\nName=Docs\nURL=https://x\n")); ok {
		t.Fatal("a Link is not an application")
	}
	if e, ok := parseDesktopEntry("h.desktop", strings.NewReader("[Desktop Entry]\nType=Application\nName=Helper\nNoDisplay=true\nExec=h\n")); !ok || !e.NoDisplay {
		t.Fatalf("NoDisplay: %+v %v", e, ok)
	}
}

func fakeXdg(t *testing.T) (home, dirs string) {
	t.Helper()
	home, sys := t.TempDir(), t.TempDir()
	writeFiles(t, home, map[string]string{
		"applications/code.desktop":   "[Desktop Entry]\nType=Application\nName=Visual Studio Code\nExec=/usr/share/code/code %F\nIcon=vscode\nStartupWMClass=Code\n",
		"applications/hidden.desktop": "[Desktop Entry]\nType=Application\nName=Hidden Helper\nExec=h\nNoDisplay=true\n",
	})
	writeFiles(t, sys, map[string]string{
		"applications/org.mozilla.firefox.desktop": firefoxDesktop,
		"applications/code.desktop":                "[Desktop Entry]\nType=Application\nName=Code (system copy)\nExec=code\n",
		"icons/hicolor/48x48/apps/firefox.png":     "48",
		"icons/hicolor/128x128/apps/firefox.png":   "128",
		"icons/hicolor/scalable/apps/vscode.svg":   "<svg/>",
		"pixmaps/legacy.png":                       "pix",
	})
	t.Setenv("XDG_DATA_HOME", home)
	t.Setenv("XDG_DATA_DIRS", sys+":"+t.TempDir())
	return home, sys
}

func TestInstalledAppsFromXdgDirs(t *testing.T) {
	home, sys := fakeXdg(t)
	apps := InstalledApps()
	if len(apps) != 2 || apps[0].Name != "Firefox" || apps[0].BundleID != "org.mozilla.firefox" || apps[0].Path != filepath.Join(sys, "applications", "org.mozilla.firefox.desktop") ||
		apps[1].Name != "Visual Studio Code" || apps[1].BundleID != "code" || apps[1].Path != filepath.Join(home, "applications", "code.desktop") {
		t.Fatalf("apps %+v", apps)
	}
	if info, err := AppInfo(apps[0].Path); err != nil || info != apps[0] {
		t.Fatalf("AppInfo: %+v %v", info, err)
	}
	if info, err := AppInfo("/opt/thing/bin/thing"); err != nil || info.Name != "thing" || info.BundleID != "thing" || info.Path != "/opt/thing/bin/thing" {
		t.Fatalf("AppInfo(bare path): %+v %v", info, err)
	}
	if _, _, err := ChooseApp(); err == nil {
		t.Fatal("ChooseApp is not supported")
	}
}

func TestFindIconPrefersSmallestLargeEnoughPng(t *testing.T) {
	_, sys := fakeXdg(t)
	dirs := dataDirs()
	cases := []struct {
		icon string
		size int
		want string
	}{
		{"firefox", 64, filepath.Join(sys, "icons/hicolor/128x128/apps/firefox.png")},
		{"firefox", 48, filepath.Join(sys, "icons/hicolor/48x48/apps/firefox.png")},
		{"firefox", 200, filepath.Join(sys, "icons/hicolor/128x128/apps/firefox.png")},
		{"legacy", 64, filepath.Join(sys, "pixmaps/legacy.png")},
		{filepath.Join(sys, "pixmaps/legacy.png"), 64, filepath.Join(sys, "pixmaps/legacy.png")},
	}
	for _, c := range cases {
		if got, ok := findIcon(c.icon, c.size, dirs); !ok || got != c.want {
			t.Errorf("findIcon(%s, %d) = %q %v, want %q", c.icon, c.size, got, ok, c.want)
		}
	}
	if _, ok := findIcon("vscode", 64, dirs); ok {
		t.Error("an SVG-only icon is not a PNG")
	}
	png, err := AppIconPNG(filepath.Join(sys, "applications", "org.mozilla.firefox.desktop"), 64)
	if err != nil || string(png) != "128" {
		t.Errorf("AppIconPNG: %q %v", png, err)
	}
	if _, err := AppIconPNG(filepath.Join(os.Getenv("XDG_DATA_HOME"), "applications", "code.desktop"), 64); err == nil {
		t.Error("no PNG for vscode")
	}
}

func TestLoginItemWritesAutostartDesktopFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if LoginItemEnabled() {
		t.Fatal("enabled before any file")
	}
	if err := SetLoginItem(true, "/opt/vcd", ""); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(autostartPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[Desktop Entry]", "Type=Application", `Exec=env VCK_LOGIN_START=1 "/opt/vcd" app`, "Name=Vibe Coder Deck"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("autostart file lacks %q:\n%s", want, b)
		}
	}
	if !LoginItemEnabled() {
		t.Fatal("not enabled after writing")
	}
	if err := SetLoginItem(false, "/opt/vcd", ""); err != nil || LoginItemEnabled() {
		t.Fatalf("disable: %v enabled=%v", err, LoginItemEnabled())
	}
	if err := SetLoginItem(false, "/opt/vcd", ""); err != nil {
		t.Fatal("disabling twice is fine:", err)
	}
}

//go:build linux

package platform

import (
	"os"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func TestIdentifyMatchesDesktopEntries(t *testing.T) {
	entries := []desktopEntry{
		{ID: "org.mozilla.firefox", Path: "/usr/share/applications/org.mozilla.firefox.desktop", Name: "Firefox", Exec: "firefox %u", WMClass: "firefox"},
		{ID: "code", Path: "/usr/share/applications/code.desktop", Name: "Visual Studio Code", Exec: "/usr/share/code/code --unity-launch %F"},
		{ID: "kitty", Path: "/usr/share/applications/kitty.desktop", Name: "kitty", Exec: "kitty"},
	}
	cases := []struct {
		exe, class                 string
		wantID, wantName, wantPath string
	}{
		{"/usr/lib/firefox/firefox-bin", "Firefox", "org.mozilla.firefox", "Firefox", "/usr/share/applications/org.mozilla.firefox.desktop"}, // StartupWMClass, case-insensitive
		{"/usr/share/code/code", "Code", "code", "Visual Studio Code", "/usr/share/applications/code.desktop"},                               // Exec basename
		{"/usr/bin/kitty", "kitty", "kitty", "kitty", "/usr/share/applications/kitty.desktop"},                                               // class == id
		{"/opt/other/bin/thing", "Thing", "thing", "Thing", "/opt/other/bin/thing"},                                                          // nothing known
	}
	for _, c := range cases {
		id, name, path := identifyWith(c.exe, c.class, entries)
		if id != c.wantID || name != c.wantName || path != c.wantPath {
			t.Errorf("identifyWith(%s, %s) = %q %q %q, want %q %q %q", c.exe, c.class, id, name, path, c.wantID, c.wantName, c.wantPath)
		}
	}
}

func TestFrontmostAppUnderXvfb(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		skipOrFail(t, "no DISPLAY")
	}
	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	screen := xproto.Setup(conn).DefaultScreen(conn)
	wid, _ := xproto.NewWindowId(conn)
	xproto.CreateWindow(conn, screen.RootDepth, wid, screen.Root, 0, 0, 200, 100, 0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil)
	atom := func(name string) xproto.Atom {
		r, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			t.Fatal(err)
		}
		return r.Atom
	}
	class := []byte("vcdtest\x00vcdtest\x00")
	xproto.ChangeProperty(conn, xproto.PropModeReplace, wid, xproto.AtomWmClass, xproto.AtomString, 8, uint32(len(class)), class)
	pid := uint32(os.Getpid())
	xproto.ChangeProperty(conn, xproto.PropModeReplace, wid, atom("_NET_WM_PID"), xproto.AtomCardinal, 32, 1, []byte{byte(pid), byte(pid >> 8), byte(pid >> 16), byte(pid >> 24)})
	xproto.MapWindow(conn, wid)
	conn.Sync()
	for i := 0; i < 30; i++ {
		id, name, path := FrontmostApp()
		if id == "vcdtest" {
			if name != "vcdtest" || path == "" {
				t.Fatalf("name %q path %q", name, path)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	id, name, path := FrontmostApp()
	t.Fatalf("our window never came to the front: %q %q %q", id, name, path)
}

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

// TestFrontmostAppUnderXvfb maps a window of its own and expects FrontmostApp
// to name it. It needs a window manager on DISPLAY (openbox in CI and in the
// Docker runner) and asks it to activate the window the way a pager does,
// which window managers honour whatever their focus policy; the first run of
// the CI job failed because the window was never focused by itself.
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
	atom := func(name string) xproto.Atom {
		r, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			t.Fatal(err)
		}
		return r.Atom
	}
	// Wait for the window manager: it is started next to the tests.
	wmCheck := atom("_NET_SUPPORTING_WM_CHECK")
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		r, err := xproto.GetProperty(conn, false, screen.Root, wmCheck, xproto.AtomWindow, 0, 1).Reply()
		if err == nil && len(r.Value) >= 4 && xgb.Get32(r.Value) != 0 {
			break
		}
		if time.Now().After(deadline) {
			skipOrFail(t, "no window manager on DISPLAY "+os.Getenv("DISPLAY"))
		}
	}
	wid, _ := xproto.NewWindowId(conn)
	xproto.CreateWindow(conn, screen.RootDepth, wid, screen.Root, 0, 0, 200, 100, 0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil)
	class := []byte("vcdtest\x00vcdtest\x00")
	xproto.ChangeProperty(conn, xproto.PropModeReplace, wid, xproto.AtomWmClass, xproto.AtomString, 8, uint32(len(class)), class)
	pid := uint32(os.Getpid())
	xproto.ChangeProperty(conn, xproto.PropModeReplace, wid, atom("_NET_WM_PID"), xproto.AtomCardinal, 32, 1, []byte{byte(pid), byte(pid >> 8), byte(pid >> 16), byte(pid >> 24)})
	xproto.MapWindow(conn, wid)
	conn.Sync()
	activate := func() { // _NET_ACTIVE_WINDOW from a pager (source 2): granted whatever the focus policy
		ev := xproto.ClientMessageEvent{Format: 32, Window: wid, Type: atom("_NET_ACTIVE_WINDOW"),
			Data: xproto.ClientMessageDataUnionData32New([]uint32{2, 0, 0, 0, 0})}
		xproto.SendEvent(conn, false, screen.Root, xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify, string(ev.Bytes()))
		conn.Sync()
	}
	for i := 0; i < 50; i++ {
		if i%5 == 0 {
			activate()
		}
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

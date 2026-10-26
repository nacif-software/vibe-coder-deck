//go:build linux

package platform

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// x11 is one connection to the X server, kept for the 400 ms polls.
type x11 struct {
	conn  *xgb.Conn
	root  xproto.Window
	atoms map[string]xproto.Atom
}

var x11Conn struct {
	once sync.Once
	x    *x11
}

// openX11 connects once. There is no connection under Wayland (XWayland
// only knows X clients) or without a DISPLAY: the Desktop profile stays.
func openX11() *x11 {
	x11Conn.once.Do(func() {
		if os.Getenv("DISPLAY") == "" {
			return
		}
		if os.Getenv("XDG_SESSION_TYPE") == "wayland" {
			log.Printf("frontmost: Wayland session, profile switching needs X11")
			return
		}
		conn, err := xgb.NewConn()
		if err != nil {
			log.Printf("frontmost: X11: %v", err)
			return
		}
		x := &x11{conn: conn, root: xproto.Setup(conn).DefaultScreen(conn).Root, atoms: map[string]xproto.Atom{}}
		for _, name := range []string{"_NET_ACTIVE_WINDOW", "_NET_WM_PID"} {
			r, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
			if err != nil {
				log.Printf("frontmost: X11 atom %s: %v", name, err)
				conn.Close()
				return
			}
			x.atoms[name] = r.Atom
		}
		x11Conn.x = x
	})
	return x11Conn.x
}

func (x *x11) property(window xproto.Window, atom xproto.Atom) ([]byte, error) {
	r, err := xproto.GetProperty(x.conn, false, window, atom, xproto.AtomAny, 0, 1<<16).Reply()
	if err != nil {
		return nil, err
	}
	return r.Value, nil
}

// active is the focused window's process id and WM_CLASS class.
func (x *x11) active() (pid int, class string, err error) {
	v, err := x.property(x.root, x.atoms["_NET_ACTIVE_WINDOW"])
	if err != nil {
		return 0, "", err
	}
	if len(v) < 4 {
		return 0, "", errors.New("no active window")
	}
	window := xproto.Window(xgb.Get32(v))
	if window == 0 {
		return 0, "", errors.New("no active window")
	}
	if v, err := x.property(window, x.atoms["_NET_WM_PID"]); err == nil && len(v) >= 4 {
		pid = int(xgb.Get32(v))
	}
	if v, err := x.property(window, xproto.AtomWmClass); err == nil {
		parts := strings.Split(strings.TrimRight(string(v), "\x00"), "\x00")
		class = parts[len(parts)-1]
	}
	if pid == 0 && class == "" {
		return 0, "", fmt.Errorf("window %#x has neither _NET_WM_PID nor WM_CLASS", window)
	}
	return pid, class, nil
}

// identifyWith names the app behind a window: the .desktop entry whose
// StartupWMClass is the class, else whose Exec is the executable, else whose
// id is the class; else the class itself.
func identifyWith(exe, class string, entries []desktopEntry) (bundleID, name, path string) {
	lower := strings.ToLower(class)
	exeBase := filepath.Base(exe)
	for _, e := range entries {
		if e.WMClass != "" && strings.EqualFold(e.WMClass, class) {
			return e.ID, e.Name, e.Path
		}
	}
	if exeBase != "" && exeBase != "." {
		for _, e := range entries {
			fields := strings.Fields(e.Exec)
			if len(fields) > 0 && filepath.Base(fields[0]) == exeBase {
				return e.ID, e.Name, e.Path
			}
		}
	}
	for _, e := range entries {
		if strings.ToLower(e.ID) == lower {
			return e.ID, e.Name, e.Path
		}
	}
	return lower, class, exe
}

// FrontmostApp is the app in front on X11: id, name and path for profiles.
// Empty under Wayland or without a display.
func FrontmostApp() (bundleID, name, path string) {
	x := openX11()
	if x == nil {
		return "", "", ""
	}
	pid, class, err := x.active()
	if err != nil {
		return "", "", ""
	}
	exe := ""
	if pid > 0 {
		exe, _ = os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	}
	return identifyWith(exe, class, installedEntries())
}

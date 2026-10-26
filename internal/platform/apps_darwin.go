//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

// frontApp writes the frontmost app's bundle id, name and bundle path (empty when none).
static void frontApp(char *bundle, int nb, char *name, int nn, char *path, int np) {
	bundle[0] = 0; name[0] = 0; path[0] = 0;
	@autoreleasepool {
		NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
		if (!app) return;
		if (app.bundleIdentifier) strlcpy(bundle, app.bundleIdentifier.UTF8String, nb);
		if (app.localizedName) strlcpy(name, app.localizedName.UTF8String, nn);
		if (app.bundleURL) strlcpy(path, app.bundleURL.path.UTF8String, np);
	}
}

// bundleInfo reads an app bundle's id and display name.
static int bundleInfo(const char *path, char *bundle, int nb, char *name, int nn) {
	bundle[0] = 0; name[0] = 0;
	@autoreleasepool {
		NSBundle *b = [NSBundle bundleWithPath:[NSString stringWithUTF8String:path]];
		if (!b || !b.bundleIdentifier) return 0;
		strlcpy(bundle, b.bundleIdentifier.UTF8String, nb);
		NSString *n = [b objectForInfoDictionaryKey:@"CFBundleDisplayName"] ?: [b objectForInfoDictionaryKey:@"CFBundleName"];
		if (!n) n = [[[NSString stringWithUTF8String:path] lastPathComponent] stringByDeletingPathExtension];
		strlcpy(name, n.UTF8String, nn);
		return 1;
	}
}

// appIcon returns a malloc'd PNG of the bundle's icon at size points.
static void *appIcon(const char *path, int size, int *len) {
	*len = 0;
	@autoreleasepool {
		NSImage *icon = [[NSWorkspace sharedWorkspace] iconForFile:[NSString stringWithUTF8String:path]];
		if (!icon) return NULL;
		NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL pixelsWide:size pixelsHigh:size
			bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSCalibratedRGBColorSpace bytesPerRow:0 bitsPerPixel:0];
		[NSGraphicsContext saveGraphicsState];
		[NSGraphicsContext setCurrentContext:[NSGraphicsContext graphicsContextWithBitmapImageRep:rep]];
		[icon drawInRect:NSMakeRect(0, 0, size, size) fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1.0];
		[NSGraphicsContext restoreGraphicsState];
		NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
		void *out = malloc(png.length);
		memcpy(out, png.bytes, png.length);
		*len = (int)png.length;
		return out;
	}
}
*/
import "C"

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"
)

// InstalledApp is an app bundle found on this Mac.
type InstalledApp struct {
	Name     string `json:"name"`
	BundleID string `json:"bundle_id"`
	Path     string `json:"path"`
}

// FrontmostApp is the bundle id, name and bundle path of the app in front.
func FrontmostApp() (bundleID, name, path string) {
	var b, n, p [1024]C.char
	C.frontApp(&b[0], 1024, &n[0], 1024, &p[0], 1024)
	return C.GoString(&b[0]), C.GoString(&n[0]), C.GoString(&p[0])
}

// AppInfo reads a bundle's id and name.
func AppInfo(path string) (InstalledApp, error) {
	var b, n [512]C.char
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if C.bundleInfo(cpath, &b[0], 512, &n[0], 512) == 0 {
		return InstalledApp{}, errors.New("not an app bundle: " + path)
	}
	return InstalledApp{Name: C.GoString(&n[0]), BundleID: C.GoString(&b[0]), Path: path}, nil
}

// InstalledApps lists the apps in /Applications, /System/Applications and
// ~/Applications (and one folder down, like /Applications/Utilities), by name.
func InstalledApps() []InstalledApp {
	home, _ := os.UserHomeDir()
	var out []InstalledApp
	seen := map[string]bool{}
	for _, dir := range []string{"/Applications", "/System/Applications", filepath.Join(home, "Applications")} {
		for _, pattern := range []string{"*.app", "*/*.app"} {
			matches, _ := filepath.Glob(filepath.Join(dir, pattern))
			for _, m := range matches {
				info, err := AppInfo(m)
				if err != nil || seen[info.BundleID] {
					continue
				}
				seen[info.BundleID] = true
				out = append(out, info)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// AppIconPNG is the app's icon as a PNG, size pixels square.
func AppIconPNG(path string, size int) ([]byte, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var n C.int
	p := C.appIcon(cpath, C.int(size), &n)
	if p == nil {
		return nil, errors.New("no icon")
	}
	defer C.free(p)
	return C.GoBytes(p, n), nil
}

// ChooseApp shows macOS's own "choose an app" dialog; ok is false when cancelled.
func ChooseApp() (InstalledApp, bool, error) {
	out, err := exec.Command("osascript", "-e", `POSIX path of (choose application with prompt "Add an app to Vibe Coder Deck" as alias)`).Output()
	if err != nil {
		return InstalledApp{}, false, nil // cancelled
	}
	info, err := AppInfo(strings.TrimRight(strings.TrimSpace(string(out)), "/"))
	return info, err == nil, err
}

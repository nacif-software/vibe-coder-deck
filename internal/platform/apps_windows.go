//go:build windows

package platform

import "errors"

type InstalledApp struct {
	Name     string `json:"name"`
	BundleID string `json:"bundle_id"`
	Path     string `json:"path"`
}

// FrontmostApp is not implemented on Windows yet: profiles do not switch.
func FrontmostApp() (bundleID, name, path string) { return "", "", "" }

func InstalledApps() []InstalledApp { return nil }

func AppIconPNG(path string, size int) ([]byte, error) {
	return nil, errors.New("no icons on Windows yet")
}

func ChooseApp() (InstalledApp, bool, error) {
	return InstalledApp{}, false, errors.New("not on Windows yet")
}

func AppInfo(path string) (InstalledApp, error) {
	return InstalledApp{}, errors.New("not on Windows yet")
}

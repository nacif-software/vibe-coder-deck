package platform

import (
	"fmt"
	"strings"
)

// holdModifiers splits a modifier-only action ("ropt", "ctrl+shift") into
// its modifier names, as IsModifier knows them.
func holdModifiers(action string) ([]string, error) {
	var mods []string
	for _, t := range strings.Split(action, "+") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || !IsModifier(t) {
			return nil, fmt.Errorf("%q is not a modifier-only action", action)
		}
		mods = append(mods, t)
	}
	return mods, nil
}

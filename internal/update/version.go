package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a release number: the MAJOR.MINOR.PATCH of a tag such as v1.2.3.
type Version struct{ Major, Minor, Patch int }

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less orders release numbers numerically (0.9.0 < 0.10.0).
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// Parse reads the release number at the start of a version string the way a
// tag or `git describe` writes it: "v1.2.3", "1.2.3", "v1.2.3-4-gabc1234"
// (ahead of the tag), "v1.2.3-dirty". Anything else ("dev", a bare hash) is
// not a release.
func Parse(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || p == "" {
			return Version{}, false
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, true
}

// IsRelease reports whether s names a release exactly (v1.2.3, no suffix):
// a build made from a tag, as opposed to a development build.
func IsRelease(s string) bool {
	v, ok := Parse(s)
	return ok && strings.TrimPrefix(strings.TrimSpace(s), "v") == v.String()
}

// Newer reports whether latest is a release newer than current. A current
// that is not a release at all (a dev build) is never behind.
func Newer(current, latest string) bool {
	c, ok := Parse(current)
	if !ok {
		return false
	}
	l, ok := Parse(latest)
	return ok && c.Less(l)
}

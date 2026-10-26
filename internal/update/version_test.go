package update

import "testing"

func TestParseReadsTheReleaseNumberOfAVersionString(t *testing.T) {
	cases := map[string]struct {
		want Version
		ok   bool
	}{
		"v1.2.3":            {Version{1, 2, 3}, true},
		"1.2.3":             {Version{1, 2, 3}, true},
		"v1.2.3-4-gabc1234": {Version{1, 2, 3}, true}, // git describe: ahead of the tag
		"v1.2.3-dirty":      {Version{1, 2, 3}, true},
		"v0.10.0":           {Version{0, 10, 0}, true},
		"dev":               {Version{}, false},
		"abc1234":           {Version{}, false},
		"v1.2":              {Version{}, false},
		"":                  {Version{}, false},
	}
	for in, c := range cases {
		got, ok := Parse(in)
		if ok != c.ok || got != c.want {
			t.Errorf("Parse(%q) = %v, %v; want %v, %v", in, got, ok, c.want, c.ok)
		}
	}
	if s := (Version{1, 2, 3}).String(); s != "1.2.3" {
		t.Fatalf("String = %q", s)
	}
}

func TestNewerComparesReleaseNumbers(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.1.9", false},
		{"v1.0.0", "v1.0.0", false},
		{"v0.9.0", "v0.10.0", true}, // numeric, not lexical
		{"v0.1.0-3-gabc1234", "v0.1.0", false},
		{"v0.1.0-3-gabc1234", "v0.1.1", true},
		{"dev", "v1.0.0", false}, // a build that is no release is never behind
		{"v1.0.0", "dev", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestIsReleaseIsTrueOnlyForAnExactTag(t *testing.T) {
	for in, want := range map[string]bool{"v1.2.3": true, "1.2.3": true, "v1.2.3-4-gabc1234": false, "v1.2.3-dirty": false, "dev": false} {
		if got := IsRelease(in); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", in, got, want)
		}
	}
}

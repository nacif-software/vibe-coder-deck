package pad

import "testing"

// Usages from the USB HID Usage Tables: keyboard page 0x07, consumer page
// 0x0C, button page 0x09. Left ctrl/shift/alt/gui = 0xE0..0xE3.
func TestTrackerBuildsCombos(t *testing.T) {
	var tr Tracker
	feed := func(page, usage uint32, down bool) (string, bool) {
		return tr.Feed(HIDInput{Page: page, Usage: usage, Down: down})
	}

	for _, m := range []uint32{0xE0, 0xE2, 0xE3} { // ctrl, alt, gui held
		if c, ok := feed(0x07, m, true); ok {
			t.Fatalf("modifier down produced combo %q", c)
		}
	}
	if c, ok := feed(0x07, 0x1E, true); !ok || c != "ctrl+opt+cmd+1" { // '1'
		t.Errorf("ctrl+opt+cmd+1 = %q, %v", c, ok)
	}
	if _, ok := feed(0x07, 0x1E, false); ok {
		t.Error("key up produced a combo")
	}
	for _, m := range []uint32{0xE0, 0xE2, 0xE3} {
		feed(0x07, m, false)
	}

	feed(0x07, 0xE3, true)                                  // gui
	feed(0x07, 0xE1, true)                                  // shift
	if c, _ := feed(0x07, 0x21, true); c != "shift+cmd+4" { // '4'
		t.Errorf("cmd+shift+4 = %q", c)
	}
	feed(0x07, 0xE1, false)
	feed(0x07, 0xE3, false)

	if c, _ := feed(0x07, 0x29, true); c != "esc" {
		t.Errorf("esc = %q", c)
	}
	if c, _ := feed(0x0C, 0xE9, true); c != "volume-up" {
		t.Errorf("volume-up = %q", c)
	}
	if c, _ := feed(0x0C, 0xEA, true); c != "volume-down" {
		t.Errorf("volume-down = %q", c)
	}
	if c, _ := feed(0x09, 0x02, true); c != "right-click" {
		t.Errorf("button 2 = %q", c)
	}
	if _, ok := feed(0x07, 0x01, true); ok { // ErrorRollOver, not a key
		t.Error("rollover usage produced a combo")
	}
}

func TestMatches(t *testing.T) {
	cases := []struct {
		action, combo string
		want          bool
	}{
		{"ctrl+opt+cmd+1", "ctrl+opt+cmd+1", true},
		{"cmd+shift+4", "shift+cmd+4", true},
		{"Command+Shift+4", "shift+cmd+4", true},
		{"Esc", "esc", true},
		{"cmd+a, cmd+c", "cmd+a", true}, // a sequence starts with its first combo
		{"cmd+a, cmd+c", "cmd+c", false},
		{"vol-up", "volume-up", true},
		{"click", "click", true},
		{"ctrl+opt+cmd+1", "ctrl+opt+cmd+2", false},
		{"", "esc", false},
	}
	for _, c := range cases {
		if got := Matches(c.action, c.combo); got != c.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", c.action, c.combo, got, c.want)
		}
	}
}

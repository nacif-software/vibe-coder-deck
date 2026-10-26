package deck

import (
	"strings"
	"testing"
)

func TestIconValidation(t *testing.T) {
	ok := []string{"", "icon:skull", "icon:rocket", "🚀", "OK", "data:image/png;base64,iVBORw0KGgo=", "data:image/svg+xml;base64,PHN2Zy8+"}
	for _, icon := range ok {
		if err := (Key{Slot: Key1, Mode: ModeOff, Icon: icon}).Validate(); err != nil {
			t.Errorf("icon %q: %v", icon, err)
		}
	}
	bad := []string{"icon:teleport", "<svg onload=x>", "data:text/html;base64,PGI+", "data:image/png,raw", "too long label",
		"data:image/png;base64," + strings.Repeat("A", 400<<10)}
	for _, icon := range bad {
		if err := (Key{Slot: Key1, Mode: ModeOff, Icon: icon}).Validate(); err == nil {
			t.Errorf("icon %.40q should fail", icon)
		}
	}
}

func TestIconsHaveDrawings(t *testing.T) {
	icons := Icons()
	if len(icons) < 20 {
		t.Fatalf("only %d icons", len(icons))
	}
	seen := map[string]bool{}
	for _, ic := range icons {
		if ic.Name == "" || ic.SVG == "" || seen[ic.Name] {
			t.Fatalf("bad icon %+v", ic)
		}
		seen[ic.Name] = true
	}
	if !seen["skull"] {
		t.Fatal("no skull: key 1's keycap has one")
	}
}

func TestIconIsStoredAndKey1StartsWithTheSkull(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	k1, _ := s.Key(Key1)
	if k1.Icon != "icon:skull" {
		t.Fatalf("key1 icon = %q", k1.Icon)
	}
	if err := s.SaveKey(Key{Slot: Key2, Mode: ModeKeys, Action: "play", Icon: "🎵", SingleInstance: true}); err != nil {
		t.Fatal(err)
	}
	k2, _ := s.Key(Key2)
	if k2.Icon != "🎵" || k2.View("ctrl+opt+cmd", nil).Icon != "🎵" {
		t.Fatalf("key2 icon = %q", k2.Icon)
	}
}

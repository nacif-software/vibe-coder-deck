package pad

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The fixtures come from the Python macropad tool's dry run
// (`macropad set SLOT ACTION --dry-run --json`), which is verified on the
// hardware: the Go packets have to be byte for byte the same.
func loadFixtures(t *testing.T) (cases []struct {
	Slot    string   `json:"slot"`
	Action  *string  `json:"action"`
	Packets []string `json:"packets"`
}, invalid []struct {
	Action  string `json:"action"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}) {
	t.Helper()
	b, err := os.ReadFile("testdata/packets.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Cases   json.RawMessage `json:"cases"`
		Invalid json.RawMessage `json:"invalid"`
	}
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fx.Cases, &cases); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fx.Invalid, &invalid); err != nil {
		t.Fatal(err)
	}
	return cases, invalid
}

func trimmedHex(p []byte) string {
	return hex.EncodeToString([]byte(strings.TrimRight(string(p), "\x00")))
}

func TestPacketsMatchTheMacropadTool(t *testing.T) {
	cases, _ := loadFixtures(t)
	for _, c := range cases {
		name := c.Slot + " = <clear>"
		var action *Action
		if c.Action != nil {
			name = c.Slot + " = " + *c.Action
			a, err := ParseAction(*c.Action)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			action = &a
		}
		packets, err := BindPackets(c.Slot, 1, action)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var got []string
		for _, p := range packets {
			if len(p) != PacketSize {
				t.Errorf("%s: packet of %d bytes", name, len(p))
			}
			got = append(got, trimmedHex(p))
		}
		if strings.Join(got, " ") != strings.Join(c.Packets, " ") {
			t.Errorf("%s:\n got  %v\n want %v", name, got, c.Packets)
		}
	}
}

func TestParseActionRejectsWhatTheToolRejects(t *testing.T) {
	_, invalid := loadFixtures(t)
	for _, c := range invalid {
		_, err := ParseAction(c.Action)
		if (err == nil) != c.OK {
			t.Errorf("ParseAction(%q): err = %v, want ok=%v (%s)", c.Action, err, c.OK, c.Message)
		}
	}
}

func TestParseActionKnowsTheKind(t *testing.T) {
	a, _ := ParseAction("cmd+a, cmd+c")
	if a.Kind != KindKeys || len(a.Steps) != 2 || a.Steps[1].Mods != 0x08 || a.Steps[1].Code != 0x06 {
		t.Fatalf("sequence = %+v", a)
	}
	if a, _ := ParseAction("volume-up"); a.Kind != KindMedia || a.Media != 0xE9 {
		t.Fatalf("media = %+v", a)
	}
	if a, _ := ParseAction("ctrl+scroll-down"); a.Kind != KindMouse || a.Mouse.Wheel != -1 || a.Mouse.Mods != 0x01 || a.Mouse.Buttons != 0 {
		t.Fatalf("mouse = %+v", a)
	}
	if a, _ := ParseAction("left-click"); a.Kind != KindMouse || a.Mouse.Buttons != 1 {
		t.Fatalf("alias = %+v", a)
	}
	if _, err := ParseAction("delete"); err == nil || !strings.Contains(err.Error(), "backspace") {
		t.Fatalf("ambiguous delete: %v", err)
	}
	if _, err := ParseAction("foo"); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("unknown: %v", err)
	}
}

func TestBindPacketsChecksSlotAndLayer(t *testing.T) {
	if _, err := BindPackets("key9", 1, nil); err == nil {
		t.Fatal("key9 accepted")
	}
	if _, err := BindPackets("key1", 4, nil); err == nil {
		t.Fatal("layer 4 accepted")
	}
}

func TestLEDPackets(t *testing.T) {
	packets, err := LEDPackets(2)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range packets {
		got = append(got, trimmedHex(p))
	}
	if strings.Join(got, " ") != "03a101 03b01802 03aaa1" {
		t.Fatalf("led = %v", got)
	}
	if _, err := LEDPackets(3); err == nil {
		t.Fatal("mode 3 accepted")
	}
}

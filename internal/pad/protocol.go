package pad

import "fmt"

// The pad's configuration protocol, decoded from the vendor tool and verified
// on the hardware (docs/protocol.md in vibe-coder-keyboard): 64-byte packets
// written to the interrupt OUT endpoint of USB interface 1. Each packet is
// 0x03 followed by the payload, zero padded. Binding a slot is "select
// layer", one or more binding packets, "commit".

// PacketSize is the length of every packet.
const PacketSize = 64

const (
	reportID    = 0x03
	typeKeys    = 0x1
	typeMedia   = 0x2
	typeMouse   = 0x3
	typeLED     = 0x8
	selectLayer = 0xA1
	commit      = 0xAA
	commitKeys  = 0xAA
	commitLED   = 0xA1
	ledTarget   = 0xB0
)

// slotIDs are the pad's slot numbers: buttons 1-6, knob left / press / right.
var slotIDs = map[string]byte{
	"key1": 1, "key2": 2, "key3": 3, "key4": 4, "key5": 5, "key6": 6,
	"knob-left": 13, "knob-press": 14, "knob-right": 15,
}

func packet(payload ...byte) []byte {
	p := make([]byte, PacketSize)
	p[0] = reportID
	copy(p[1:], payload)
	return p
}

func kindByte(layer int, typ byte) byte { return byte(layer)<<4 | typ }

// BindPackets are the packets that bind an action to a slot on a layer (1-3);
// a nil action clears the slot (one empty keystroke, as the vendor tool has no
// clear). The pad keeps the last write to a slot whatever the layer.
func BindPackets(slot string, layer int, action *Action) ([][]byte, error) {
	id, ok := slotIDs[slot]
	if !ok {
		return nil, fmt.Errorf("unknown slot %q", slot)
	}
	if layer < 1 || layer > 3 {
		return nil, fmt.Errorf("layer must be 1, 2 or 3, got %d", layer)
	}
	a := Action{Kind: KindKeys, Steps: []Keystroke{{}}}
	if action != nil {
		a = *action
	}
	var body [][]byte
	switch a.Kind {
	case KindMedia:
		body = [][]byte{packet(id, kindByte(layer, typeMedia), byte(a.Media), byte(a.Media>>8))}
	case KindMouse:
		body = [][]byte{packet(id, kindByte(layer, typeMouse), a.Mouse.Buttons, 0, 0, byte(a.Mouse.Wheel), a.Mouse.Mods)}
	default:
		steps := a.Steps
		if len(steps) == 0 {
			steps = []Keystroke{{}}
		}
		n := byte(len(steps))
		// Index 0 carries only the first step's modifiers; 1..n are the keystrokes.
		body = append(body, packet(id, kindByte(layer, typeKeys), n, 0, steps[0].Mods, 0))
		for i, s := range steps {
			body = append(body, packet(id, kindByte(layer, typeKeys), n, byte(i+1), s.Mods, s.Code))
		}
	}
	out := make([][]byte, 0, len(body)+2)
	out = append(out, packet(selectLayer, byte(layer)))
	out = append(out, body...)
	return append(out, packet(commit, commitKeys)), nil
}

// LEDPackets set the backlight: 0 off, 1 last-pressed key lit, 2 rainbow wave.
func LEDPackets(mode int) ([][]byte, error) {
	if mode < 0 || mode > 2 {
		return nil, fmt.Errorf("LED mode must be 0, 1 or 2, got %d", mode)
	}
	return [][]byte{
		packet(selectLayer, 1),
		packet(ledTarget, kindByte(1, typeLED), byte(mode)),
		packet(commit, commitLED),
	}, nil
}

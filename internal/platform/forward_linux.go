//go:build linux

package platform

import (
	"strings"

	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

// forwarder decides what to do with each event of a grabbed pad: a
// dispatched hotkey (modifiers+digit) goes to hotkey and never reaches the
// desktop; everything else is emitted again on the virtual keyboard.
// Modifier downs wait until the next key shows whether they belong to a
// hotkey, so a hotkey never leaks a bare Super tap (GNOME's overview).
type forwarder struct {
	modifiers string
	hotkey    func(digit int, down bool)
	emit      func(code uint16, down bool)
	combo     func(string)
	tracker   pad.Tracker
	buffered  []uint16        // modifiers down, not forwarded yet
	swallowed map[uint16]bool // modifiers of a hotkey: their ups are dropped
	digits    map[uint16]int  // hotkey digit keys held, by code
}

func newForwarder(modifiers string, hotkey func(digit int, down bool), emit func(code uint16, down bool), combo func(string)) *forwarder {
	return &forwarder{modifiers: modifiers, hotkey: hotkey, emit: emit, combo: combo, swallowed: map[uint16]bool{}, digits: map[uint16]int{}}
}

// buttonNames are the pad's spellings of the mouse buttons the tracker knows.
var buttonNames = map[uint16]string{0x110: "click", 0x111: "right-click", 0x112: "middle-click"}

// Event takes one EV_KEY event: value 0 up, 1 down, 2 repeat (ignored).
func (f *forwarder) Event(code uint16, value int32) {
	if value == 2 {
		return
	}
	down := value == 1
	name := KeyName(int(code))
	if b, ok := buttonNames[code]; ok {
		name = b
	}
	combo, reported := f.tracker.FeedKey(name, down)
	if IsModifier(name) {
		f.modifier(code, down)
		return
	}
	if !down {
		if digit, ok := f.digits[code]; ok {
			delete(f.digits, code)
			f.hotkey(digit, false)
			return
		}
		f.emit(code, false)
		return
	}
	if reported {
		f.combo(combo)
		if digit, ok := f.hotkeyDigit(combo); ok {
			for _, m := range f.buffered {
				f.swallowed[m] = true
			}
			f.buffered = nil
			f.digits[code] = digit
			f.hotkey(digit, true)
			return
		}
	}
	for _, m := range f.buffered {
		f.emit(m, true)
	}
	f.buffered = nil
	f.emit(code, true)
}

func (f *forwarder) modifier(code uint16, down bool) {
	if down {
		f.buffered = append(f.buffered, code)
		return
	}
	if f.swallowed[code] {
		delete(f.swallowed, code)
		return
	}
	for i, m := range f.buffered {
		if m == code { // a bare tap: nothing was typed with it
			f.buffered = append(f.buffered[:i], f.buffered[i+1:]...)
			f.emit(code, true)
			f.emit(code, false)
			return
		}
	}
	f.emit(code, false)
}

// hotkeyDigit is the slot digit when combo is modifiers+1 … modifiers+9.
func (f *forwarder) hotkeyDigit(combo string) (int, bool) {
	prefix := f.modifiers + "+"
	if !strings.HasPrefix(combo, prefix) || len(combo) != len(prefix)+1 {
		return 0, false
	}
	d := int(combo[len(prefix)] - '0')
	return d, d >= 1 && d <= 9
}

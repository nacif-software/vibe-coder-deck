package platform

// heldKeys drops the keyboard's auto-repeat while recording: a held key sends
// keydown over and over, but the macro wants one keydown, the wait and the
// keyup, so holding a key only makes its delay longer.
type heldKeys map[string]bool

// repeat reports whether a key event is an auto-repeat to skip, and keeps
// track of the keys that are down.
func (h heldKeys) repeat(typ, name string) bool {
	switch typ {
	case "keydown":
		if h[name] {
			return true
		}
		h[name] = true
	case "keyup":
		delete(h, name)
	}
	return false
}

// Package debuglog is the switch for debug mode: when it is on, the deck logs
// what it normally keeps quiet (polled requests, recorder events, every
// keystroke from the pad, every hotkey) to the same log as everything else.
package debuglog

import (
	"log"
	"sync/atomic"
)

var on atomic.Bool

func Enabled() bool { return on.Load() }

func Set(enabled bool) {
	if on.Swap(enabled) != enabled {
		log.Printf("debug mode %s", map[bool]string{true: "on", false: "off"}[enabled])
	}
}

// Printf logs only in debug mode, with a "debug:" prefix.
func Printf(format string, args ...any) {
	if on.Load() {
		log.Printf("debug: "+format, args...)
	}
}

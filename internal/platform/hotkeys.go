// Package platform is everything that differs per operating system: global
// hotkeys, keystrokes, the frontmost app, notifications.
package platform

import (
	"fmt"

	"golang.design/x/hotkey"
	"golang.design/x/hotkey/mainthread"
)

// RunMain runs f with an OS event loop on the main thread, which registering
// hotkeys needs on macOS. f runs on another goroutine; RunMain returns when
// f returns.
func RunMain(f func()) { mainthread.Init(f) }

var digitKeys = []hotkey.Key{
	hotkey.Key1, hotkey.Key2, hotkey.Key3, hotkey.Key4, hotkey.Key5,
	hotkey.Key6, hotkey.Key7, hotkey.Key8, hotkey.Key9,
}

// Listen registers modifiers+1 … modifiers+9 as global hotkeys and calls
// onKey(digit, down) when one goes down and up again, in that order. The pad
// sends these for dispatched slots and holds them while its key is held.
func Listen(onKey func(digit int, down bool)) (stop func(), err error) {
	var registered []*hotkey.Hotkey
	stop = func() {
		for _, hk := range registered {
			hk.Unregister()
		}
	}
	for digit := 1; digit <= 9; digit++ {
		hk := hotkey.New(hotkeyModifiers(), digitKeys[digit-1])
		if err := hk.Register(); err != nil {
			stop()
			return nil, fmt.Errorf("register %s: %w%s", hk, err, permissionHint())
		}
		registered = append(registered, hk)
		// One goroutine per key reads both channels, so a release is never
		// handled before its press.
		go func(digit int, hk *hotkey.Hotkey) {
			for {
				select {
				case _, ok := <-hk.Keydown():
					if !ok {
						return
					}
					onKey(digit, true)
				case _, ok := <-hk.Keyup():
					if !ok {
						return
					}
					onKey(digit, false)
				}
			}
		}(digit, hk)
	}
	return stop, nil
}

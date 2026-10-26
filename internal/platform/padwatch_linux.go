//go:build linux

package platform

import (
	"fmt"
	"log"
	"sync"
	"time"

	"golang.design/x/mainthread"
	"golang.org/x/sys/unix"

	"github.com/nacif-software/vibe-coder-deck/internal/debuglog"
	"github.com/nacif-software/vibe-coder-deck/internal/pad"
)

// PadInfo is one input device of the pad the kernel reports as connected.
type PadInfo struct {
	Product, Manufacturer string
	VendorID, ProductID   int
}

// PadPress is one keystroke the pad sent, as a combo in the pad's syntax.
type PadPress struct {
	Combo string
	At    int64 // unix milliseconds
}

const keptPresses = 200

// padPoll is how often the watcher looks for the pad coming and going.
var padPoll = 2 * time.Second

// RunMain runs f with the process's main thread held, as on the other
// systems; nothing on Linux needs it, but the callers are shared.
func RunMain(f func()) { mainthread.Init(f) }

type padReader struct {
	r        *inputReader
	info     PadInfo
	keyboard bool // can send the digits: where the hotkeys come from
	reading  bool // grabbed and read
}

var padw struct {
	mu      sync.Mutex
	started bool
	onKey   func(digit int, down bool)
	nodes   map[string]*padReader // by /dev/input/eventN
	allOpen bool
	presses []PadPress
	dropped int // presses discarded from the front of presses
	last    PadPress
	lastHot struct {
		digit int
		down  bool
		at    time.Time
	}
	emitWarned bool
	grabWarned bool
}

// WatchPad follows the pad's input devices: each one that can send the
// digits is grabbed and read, its hotkeys dispatched and the rest forwarded
// through the virtual keyboard.
func WatchPad() {
	padw.mu.Lock()
	if padw.started {
		padw.mu.Unlock()
		return
	}
	padw.started = true
	padw.nodes = map[string]*padReader{}
	padw.allOpen = true
	padw.mu.Unlock()
	go func() {
		for {
			scanPad()
			time.Sleep(padPoll)
		}
	}()
}

func scanPad() {
	present := map[string]bool{}
	allOpen := true
	for _, node := range padNodes(pad.VendorID, pad.ProductID) {
		present[node.Event] = true
		padw.mu.Lock()
		pr, known := padw.nodes[node.Event]
		padw.mu.Unlock()
		if !known {
			r, err := openInput(node.Event)
			if err != nil {
				allOpen = false
				debuglog.Printf("pad: %s: %v", node.Event, err)
				continue
			}
			info := PadInfo{Product: node.Product, Manufacturer: node.Manufacturer, VendorID: node.Vendor, ProductID: node.ProductID}
			if info.Product == "" {
				info.Product = node.Name
			}
			log.Printf("pad: %s added (%s, vid %#x pid %#x)", node.Event, node.Name, node.Vendor, node.ProductID)
			pr = &padReader{r: r, info: info, keyboard: r.HasKey(2)} // KEY_1: a keyboard interface, where the hotkeys come from
			padw.mu.Lock()
			padw.nodes[node.Event] = pr
			padw.mu.Unlock()
		}
		padw.mu.Lock()
		grab := pr.keyboard && !pr.reading
		padw.mu.Unlock()
		if !grab {
			continue
		}
		// Grabbing without the virtual keyboard would swallow everything the
		// pad types; wait for /dev/uinput (the udev rule) instead.
		if _, err := virtualKeyboard(); err != nil {
			padw.mu.Lock()
			warned := padw.grabWarned
			padw.grabWarned = true
			padw.mu.Unlock()
			if !warned {
				log.Printf("pad: not grabbing %s until the virtual keyboard works: %v (run `vibe-coder-deck udev --install`)", node.Event, err)
			}
			continue
		}
		if err := pr.r.Grab(true); err != nil {
			log.Printf("pad: %s: cannot grab: %v (its keys will also reach the desktop)", node.Event, err)
		}
		padw.mu.Lock()
		pr.reading = true
		padw.mu.Unlock()
		go readPad(node.Event, pr.r)
	}
	padw.mu.Lock()
	for event, pr := range padw.nodes {
		if !present[event] {
			pr.r.Close()
			delete(padw.nodes, event)
			log.Printf("pad: %s removed", event)
		}
	}
	padw.allOpen = allOpen
	padw.mu.Unlock()
}

// readPad reads one grabbed node until the device goes away.
func readPad(event string, r *inputReader) {
	fwd := newForwarder(HotkeyModifiers, dispatchHotkey, emitForwarded, recordPress)
	err := r.Read(func(ev inputEvent) {
		if ev.Type == unix.EV_KEY {
			debuglog.Printf("pad: %s code %d value %d", event, ev.Code, ev.Value)
			fwd.Event(ev.Code, ev.Value)
		}
	})
	padw.mu.Lock()
	if pr, ok := padw.nodes[event]; ok && pr.r == r {
		delete(padw.nodes, event)
		log.Printf("pad: %s gone (%v)", event, err)
	}
	padw.mu.Unlock()
	r.Close()
}

func emitForwarded(code uint16, down bool) {
	kbd, err := virtualKeyboard()
	if err != nil {
		padw.mu.Lock()
		warned := padw.emitWarned
		padw.emitWarned = true
		padw.mu.Unlock()
		if !warned {
			log.Printf("pad: cannot forward keys: %v", err)
		}
		return
	}
	if err := kbd.Emit(code, down); err != nil {
		debuglog.Printf("pad: forward %d: %v", code, err)
	}
}

// dispatchHotkey hands a digit to Listen's callback; the pad may have two
// keyboard interfaces, so the same stroke within 30 ms counts once.
func dispatchHotkey(digit int, down bool) {
	padw.mu.Lock()
	now := time.Now()
	if padw.lastHot.digit == digit && padw.lastHot.down == down && now.Sub(padw.lastHot.at) < 30*time.Millisecond {
		padw.mu.Unlock()
		return
	}
	padw.lastHot.digit, padw.lastHot.down, padw.lastHot.at = digit, down, now
	onKey := padw.onKey
	padw.mu.Unlock()
	debuglog.Printf("pad: hotkey %s+%d %v", HotkeyModifiers, digit, down)
	if onKey != nil {
		onKey(digit, down)
	}
}

func recordPress(combo string) {
	padw.mu.Lock()
	defer padw.mu.Unlock()
	now := time.Now().UnixMilli()
	if padw.last.Combo == combo && now-padw.last.At < 30 {
		return
	}
	padw.last = PadPress{Combo: combo, At: now}
	padw.presses = append(padw.presses, padw.last)
	if extra := len(padw.presses) - keptPresses; extra > 0 {
		padw.presses = padw.presses[extra:]
		padw.dropped += extra
	}
}

// Listen dispatches the pad's modifiers+1 … modifiers+9 to onKey(digit,
// down). It needs the virtual keyboard, since everything else the grabbed
// pad types is forwarded through it.
func Listen(onKey func(digit int, down bool)) (stop func(), err error) {
	if _, err := virtualKeyboard(); err != nil {
		return nil, fmt.Errorf("cannot open /dev/uinput to forward the pad's keys: %w; run `vibe-coder-deck udev --install`", err)
	}
	padw.mu.Lock()
	padw.onKey = onKey
	padw.mu.Unlock()
	WatchPad()
	return func() {
		padw.mu.Lock()
		padw.onKey = nil
		padw.mu.Unlock()
	}, nil
}

// PadStatus reports the pad's input devices (empty when it is unplugged)
// and whether every one of them could be opened.
func PadStatus() (devices []PadInfo, inputAllowed bool) {
	padw.mu.Lock()
	defer padw.mu.Unlock()
	for _, pr := range padw.nodes {
		devices = append(devices, pr.info)
	}
	return devices, padw.allOpen
}

// InputMonitoringAllowed is whether the pad's keys can be read: true while
// no pad is plugged in, so nothing nags.
func InputMonitoringAllowed() bool {
	padw.mu.Lock()
	defer padw.mu.Unlock()
	return padw.allOpen
}

// PadPresses returns the keystrokes numbered since and after, and the next number.
func PadPresses(since int) ([]PadPress, int) {
	padw.mu.Lock()
	defer padw.mu.Unlock()
	next := padw.dropped + len(padw.presses)
	i := since - padw.dropped
	if i < 0 {
		i = 0
	}
	if i > len(padw.presses) {
		i = len(padw.presses)
	}
	return append([]PadPress(nil), padw.presses[i:]...), next
}

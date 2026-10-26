//go:build linux

package platform

import (
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// uinput ioctls (linux/uinput.h): _IOW('U', n, int) for the SET_*BIT ones,
// _IOW('U', 3, struct uinput_setup) with a 92-byte struct, _IO('U', 1|2).
const (
	uiSetEvbit   = 0x40045564
	uiSetKeybit  = 0x40045565
	uiSetRelbit  = 0x40045566
	uiDevSetup   = 0x405c5503
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
)

// uinputPath is the uinput device; tests point it elsewhere.
var uinputPath = "/dev/uinput"

// uinputSetup is struct uinput_setup: input_id, name[80], ff_effects_max.
type uinputSetup struct {
	Bus, Vendor, Product, Version uint16
	Name                          [80]byte
	FFEffectsMax                  uint32
}

// uinputDevice is a virtual input device: the keyboard/mouse this program
// types with, or the fake pad the tests plug in.
type uinputDevice struct {
	f *os.File
}

// newUinputDevice creates a device that can send every key in keys (plus
// the three mouse buttons and relative motion, so compositors take it as a
// keyboard and a mouse) and waits a moment for the system to pick it up.
func newUinputDevice(name string, bus, vendor, product uint16, keys []uint16) (*uinputDevice, error) {
	f, err := os.OpenFile(uinputPath, os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd())
	fail := func(what string, err error) (*uinputDevice, error) {
		f.Close()
		return nil, fmt.Errorf("uinput %s: %w", what, err)
	}
	for _, ev := range []int{unix.EV_KEY, unix.EV_REL, unix.EV_SYN} {
		if err := unix.IoctlSetInt(fd, uiSetEvbit, ev); err != nil {
			return fail("set event type", err)
		}
	}
	for _, code := range keys {
		if err := unix.IoctlSetInt(fd, uiSetKeybit, int(code)); err != nil {
			return fail("set key", err)
		}
	}
	for _, code := range buttonCodes {
		if err := unix.IoctlSetInt(fd, uiSetKeybit, int(code)); err != nil {
			return fail("set button", err)
		}
	}
	for _, rel := range []int{relX, relY, relWheel} {
		if err := unix.IoctlSetInt(fd, uiSetRelbit, rel); err != nil {
			return fail("set rel", err)
		}
	}
	setup := uinputSetup{Bus: bus, Vendor: vendor, Product: product, Version: 1}
	copy(setup.Name[:], name)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uiDevSetup, uintptr(unsafe.Pointer(&setup))); errno != 0 {
		return fail("setup", errno)
	}
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uiDevCreate, 0); errno != 0 {
		return fail("create", errno)
	}
	time.Sleep(150 * time.Millisecond) // the device is not usable the instant it exists
	return &uinputDevice{f: f}, nil
}

// Emit sends one key or button event followed by a sync.
func (d *uinputDevice) Emit(code uint16, down bool) error {
	var v int32
	if down {
		v = 1
	}
	return writeEvents(d.f, inputEvent{Type: unix.EV_KEY, Code: code, Value: v}, inputEvent{Type: unix.EV_SYN, Code: synReport})
}

// Close destroys the device.
func (d *uinputDevice) Close() error {
	unix.Syscall(unix.SYS_IOCTL, d.f.Fd(), uiDevDestroy, 0)
	return d.f.Close()
}

var vk struct {
	once sync.Once
	dev  *uinputDevice
	err  error
}

// virtualKeyboard is the device this program types with, created once.
func virtualKeyboard() (*uinputDevice, error) {
	vk.once.Do(func() {
		keys := make([]uint16, 0, len(keyCodeNames))
		for code := range keyCodeNames {
			keys = append(keys, code)
		}
		vk.dev, vk.err = newUinputDevice("Vibe Coder Deck", unix.BUS_VIRTUAL, 1, 1, keys)
	})
	return vk.dev, vk.err
}

// AccessibilityAllowed is whether this program may type: /dev/uinput opens.
func AccessibilityAllowed() bool {
	f, err := os.OpenFile(uinputPath, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// PostKey presses or releases a key, modifier or media key by name.
func PostKey(name string, down bool) error {
	code, err := KeyCode(name)
	if err != nil {
		return err
	}
	kbd, err := virtualKeyboard()
	if err != nil {
		return err
	}
	return kbd.Emit(code, down)
}

// PostClick clicks a mouse button (left, right, middle) where the pointer is.
func PostClick(button string) error {
	code, err := ButtonCode(button)
	if err != nil {
		return err
	}
	kbd, err := virtualKeyboard()
	if err != nil {
		return err
	}
	if err := kbd.Emit(code, true); err != nil {
		return err
	}
	return kbd.Emit(code, false)
}

// typeText types s as on a US keyboard; characters it has no key for are refused.
func typeText(s string) error {
	kbd, err := virtualKeyboard()
	if err != nil {
		return err
	}
	shift := modifierCodes["shift"]
	for _, r := range s {
		ch, ok := usLayout[r]
		if !ok {
			return fmt.Errorf("cannot type %q: only US-keyboard characters can be typed on Linux", r)
		}
		if ch.Shift {
			if err := kbd.Emit(shift, true); err != nil {
				return err
			}
		}
		if err := kbd.Emit(ch.Code, true); err != nil {
			return err
		}
		if err := kbd.Emit(ch.Code, false); err != nil {
			return err
		}
		if ch.Shift {
			if err := kbd.Emit(shift, false); err != nil {
				return err
			}
		}
	}
	return nil
}

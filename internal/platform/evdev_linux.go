//go:build linux

package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// inputEvent is struct input_event on a 64-bit kernel: a timeval, then type,
// code and value (24 bytes).
type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

const inputEventSize = int(unsafe.Sizeof(inputEvent{}))

// Event codes and ioctls from linux/input.h and linux/input-event-codes.h
// that golang.org/x/sys/unix does not spell out.
const (
	synReport = 0
	relX      = 0x00
	relY      = 0x01
	relWheel  = 0x08
	keyMax    = 0x2ff

	eviocgrab = 0x40044590 // _IOW('E', 0x90, int)
)

// ioctlRead is _IOC(_IOC_READ, 'E', nr, size): EVIOCGNAME(size) is nr 0x06,
// EVIOCGBIT(ev, size) is nr 0x20+ev.
func ioctlRead(nr, size int) uint { return uint(2<<30 | size<<16 | 'E'<<8 | nr) }

// sysInput is where the kernel lists input devices; tests point it at a fake tree.
var sysInput = "/sys/class/input"

// inputReader reads one /dev/input/eventN.
type inputReader struct {
	f *os.File
}

func openInput(path string) (*inputReader, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return &inputReader{f: f}, nil
}

func (r *inputReader) control(fn func(fd int) error) error {
	conn, err := r.f.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := conn.Control(func(fd uintptr) { inner = fn(int(fd)) }); err != nil {
		return err
	}
	return inner
}

// Grab takes (or releases) the device for this reader alone: while grabbed,
// nothing else on the system sees its events.
func (r *inputReader) Grab(on bool) error {
	v := 0
	if on {
		v = 1
	}
	return r.control(func(fd int) error { return unix.IoctlSetInt(fd, eviocgrab, v) })
}

// HasKey reports whether the device can send the key code (EVIOCGBIT).
func (r *inputReader) HasKey(code uint16) bool {
	bits := make([]byte, keyMax/8+1)
	err := r.control(func(fd int) error {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(ioctlRead(0x20+unix.EV_KEY, len(bits))), uintptr(unsafe.Pointer(&bits[0])))
		if errno != 0 {
			return errno
		}
		return nil
	})
	return err == nil && int(code) < len(bits)*8 && bits[code/8]&(1<<(code%8)) != 0
}

// Name is the device's name (EVIOCGNAME).
func (r *inputReader) Name() string {
	buf := make([]byte, 256)
	err := r.control(func(fd int) error {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(ioctlRead(0x06, len(buf))), uintptr(unsafe.Pointer(&buf[0])))
		if errno != 0 {
			return errno
		}
		return nil
	})
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(buf), "\x00")
}

// Read calls fn for every event until the reader is closed.
func (r *inputReader) Read(fn func(ev inputEvent)) error {
	buf := make([]byte, 64*inputEventSize)
	for {
		n, err := r.f.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		for i := 0; i+inputEventSize <= n; i += inputEventSize {
			fn(*(*inputEvent)(unsafe.Pointer(&buf[i])))
		}
	}
}

func (r *inputReader) Close() error { return r.f.Close() }

// findInputByName is the /dev/input/eventN of the first device with that name.
func findInputByName(name string) (string, bool) {
	entries, _ := os.ReadDir(sysInput)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(sysInput, e.Name(), "device", "name"))
		if err == nil && strings.TrimSpace(string(b)) == name {
			return "/dev/input/" + e.Name(), true
		}
	}
	return "", false
}

func writeEvents(w io.Writer, events ...inputEvent) error {
	buf := make([]byte, 0, len(events)*inputEventSize)
	for i := range events {
		buf = append(buf, unsafe.Slice((*byte)(unsafe.Pointer(&events[i])), inputEventSize)...)
	}
	n, err := w.Write(buf)
	if err == nil && n != len(buf) {
		err = fmt.Errorf("short write: %d of %d bytes", n, len(buf))
	}
	return err
}

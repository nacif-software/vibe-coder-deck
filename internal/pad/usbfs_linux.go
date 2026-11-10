//go:build linux

package pad

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The pad's config interface has only an interrupt OUT endpoint, so the
// kernel's HID driver refuses it ("couldn't find an input interrupt
// endpoint") and makes no hidraw node for it. usbfs reaches the endpoint
// anyway, as libusb does: claim the interface on /dev/bus/usb/BBB/DDD and
// send each packet as an interrupt transfer.

// sysUSBDevices lists the USB devices and interfaces; tests use a fake tree.
const sysUSBDevices = "/sys/bus/usb/devices"

type usbPort struct {
	node  string // /dev/bus/usb/BBB/DDD
	iface int
	ep    uint8 // the interrupt OUT endpoint's address
}

func sysfsValue(dir, name string) string {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	return strings.TrimSpace(string(b))
}

// findUSBPort finds the pad's device node and the OUT endpoint of its
// interface iface in sysfs (root is /sys/bus/usb/devices).
func findUSBPort(root string, vid, pid, iface int) (usbPort, error) {
	devices, _ := os.ReadDir(root)
	for _, d := range devices {
		dir := filepath.Join(root, d.Name())
		if strings.Contains(d.Name(), ":") || sysfsValue(dir, "idVendor") != fmt.Sprintf("%04x", vid) || sysfsValue(dir, "idProduct") != fmt.Sprintf("%04x", pid) {
			continue
		}
		bus, _ := strconv.Atoi(sysfsValue(dir, "busnum"))
		dev, _ := strconv.Atoi(sysfsValue(dir, "devnum"))
		port := usbPort{node: fmt.Sprintf("/dev/bus/usb/%03d/%03d", bus, dev), iface: iface}
		interfaces, _ := filepath.Glob(filepath.Join(root, d.Name()+":*"))
		for _, in := range interfaces {
			if n, err := strconv.ParseInt(sysfsValue(in, "bInterfaceNumber"), 16, 0); err != nil || int(n) != iface {
				continue
			}
			endpoints, _ := filepath.Glob(filepath.Join(in, "ep_*"))
			for _, ep := range endpoints {
				addr, err := strconv.ParseUint(sysfsValue(ep, "bEndpointAddress"), 16, 8)
				if err == nil && sysfsValue(ep, "type") == "Interrupt" && sysfsValue(ep, "direction") == "out" {
					port.ep = uint8(addr)
					return port, nil
				}
			}
		}
		return usbPort{}, fmt.Errorf("the pad is plugged in but its interface %d has no interrupt OUT endpoint", iface)
	}
	return usbPort{}, ErrNotConnected
}

// struct usbdevfs_bulktransfer and struct usbdevfs_ioctl of <linux/usbdevice_fs.h>.
type usbdevfsBulkTransfer struct {
	Ep, Len, Timeout uint32
	Data             unsafe.Pointer
}

type usbdevfsIoctlArg struct {
	Ifno, IoctlCode int32
	Data            unsafe.Pointer
}

// ioc is the kernel's _IOC for the 'U' (usbfs) requests.
func ioc(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'U'<<8 | nr }

var (
	usbdevfsBulk             = ioc(3, 2, unsafe.Sizeof(usbdevfsBulkTransfer{}))
	usbdevfsClaimInterface   = ioc(2, 15, 4)
	usbdevfsReleaseInterface = ioc(2, 16, 4)
	usbdevfsIoctl            = ioc(3, 18, unsafe.Sizeof(usbdevfsIoctlArg{}))
	usbdevfsDisconnect       = ioc(0, 22, 0)
)

func ioctl(fd int, req uintptr, arg unsafe.Pointer) (int, error) {
	r, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return int(r), errno
	}
	return int(r), nil
}

// sendUSB writes the packets to the endpoint through usbfs.
func sendUSB(port usbPort, packets [][]byte) error {
	fd, err := unix.Open(port.node, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
			return fmt.Errorf("%s is not writable: run `vibe-coder-deck udev --install` to add the udev rule, then replug the pad", port.node)
		}
		return fmt.Errorf("opening %s: %w", port.node, err)
	}
	defer unix.Close(fd)
	// A driver bound to the interface would make the claim fail; usually
	// there is none (the HID driver gives up on it), so the error is ignored.
	detach := usbdevfsIoctlArg{Ifno: int32(port.iface), IoctlCode: int32(usbdevfsDisconnect)}
	ioctl(fd, usbdevfsIoctl, unsafe.Pointer(&detach))
	iface := uint32(port.iface)
	if _, err := ioctl(fd, usbdevfsClaimInterface, unsafe.Pointer(&iface)); err != nil {
		return fmt.Errorf("claiming the pad's config interface on %s: %w (is another tool using it?)", port.node, err)
	}
	defer ioctl(fd, usbdevfsReleaseInterface, unsafe.Pointer(&iface))
	for i, p := range packets {
		buf := append([]byte(nil), p...)
		xfer := usbdevfsBulkTransfer{Ep: uint32(port.ep), Len: uint32(len(buf)), Timeout: 2000, Data: unsafe.Pointer(&buf[0])}
		n, err := ioctl(fd, usbdevfsBulk, unsafe.Pointer(&xfer))
		runtime.KeepAlive(buf)
		if err != nil {
			return fmt.Errorf("writing packet %d of %d to %s: %w", i+1, len(packets), port.node, err)
		}
		if n != len(buf) {
			return fmt.Errorf("writing packet %d of %d to %s: %d of %d bytes", i+1, len(packets), port.node, n, len(buf))
		}
	}
	return nil
}

//go:build windows

package pad

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows the HID class driver owns the pad's config interface; its device
// path carries the interface number (&mi_01). Writing a report to it goes
// out through the interrupt OUT endpoint. Not yet tried on a real machine.

var (
	cfgmgr                              = windows.NewLazySystemDLL("cfgmgr32.dll")
	procInterfaceListSize               = cfgmgr.NewProc("CM_Get_Device_Interface_List_SizeW")
	procInterfaceList                   = cfgmgr.NewProc("CM_Get_Device_Interface_ListW")
	guidDevinterfaceHID                 = windows.GUID{Data1: 0x4d1e55b2, Data2: 0xf16f, Data3: 0x11cf, Data4: [8]byte{0x88, 0xcb, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}
	cmGetDeviceInterfacePresent uintptr = 0
)

// hidPaths lists the device paths of every HID interface present.
func hidPaths() ([]string, error) {
	var size uint32
	if r, _, _ := procInterfaceListSize.Call(uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&guidDevinterfaceHID)), 0, cmGetDeviceInterfacePresent); r != 0 {
		return nil, fmt.Errorf("CM_Get_Device_Interface_List_Size: CONFIGRET %d", r)
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]uint16, size)
	if r, _, _ := procInterfaceList.Call(uintptr(unsafe.Pointer(&guidDevinterfaceHID)), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), cmGetDeviceInterfacePresent); r != 0 {
		return nil, fmt.Errorf("CM_Get_Device_Interface_List: CONFIGRET %d", r)
	}
	// A multi-string: NUL-separated paths, then an empty one.
	var paths []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i > start {
				paths = append(paths, windows.UTF16ToString(buf[start:i]))
			}
			start = i + 1
		}
	}
	return paths, nil
}

func sendPackets(packets [][]byte) error {
	paths, err := hidPaths()
	if err != nil {
		return err
	}
	want := fmt.Sprintf("vid_%04x&pid_%04x", VendorID, ProductID)
	iface := fmt.Sprintf("&mi_%02x", configInterface)
	var path string
	for _, p := range paths {
		l := strings.ToLower(p)
		if strings.Contains(l, want) && strings.Contains(l, iface) {
			path = p
			break
		}
	}
	if path == "" {
		return ErrNotConnected
	}
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(path), windows.GENERIC_WRITE|windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return fmt.Errorf("opening the pad's config interface: %w", err)
	}
	defer windows.CloseHandle(h)
	for i, p := range packets {
		// The HID stack wants the report ID first. The config packets carry
		// the vendor's own 0x03 as their first byte; try them as a report with
		// ID 0 in front, then as they are.
		if err := writeReport(h, append([]byte{0}, p...)); err != nil {
			if err2 := writeReport(h, p); err2 != nil {
				return fmt.Errorf("writing packet %d of %d to the pad failed (%v; %v)", i+1, len(packets), err, err2)
			}
		}
	}
	return nil
}

func writeReport(h windows.Handle, report []byte) error {
	var done uint32
	if err := windows.WriteFile(h, report, &done, nil); err != nil {
		return err
	}
	if int(done) != len(report) {
		return errors.New("short write")
	}
	return nil
}

//go:build windows

package pad

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows the HID class driver owns the pad's config interface (USB
// interface 1): its device paths carry the interface number (&mi_01) and one
// path per top-level collection (&col01, &col02). A write to a collection
// goes out through the interrupt OUT endpoint, but the driver only takes a
// buffer of exactly the collection's OutputReportByteLength whose first byte
// is a report ID the descriptor declares; anything else is ERROR_INVALID_PARAMETER
// ("The parameter is incorrect"). The config packets are 64 bytes starting
// with the vendor's 0x03, so each is tried as it is (report 3) and behind a
// 0 (a report without ID), sized to the collection, on every collection of
// the interface, with HidD_SetOutputReport (a control transfer) as a last
// resort.

var (
	cfgmgr                              = windows.NewLazySystemDLL("cfgmgr32.dll")
	procInterfaceListSize               = cfgmgr.NewProc("CM_Get_Device_Interface_List_SizeW")
	procInterfaceList                   = cfgmgr.NewProc("CM_Get_Device_Interface_ListW")
	hid                                 = windows.NewLazySystemDLL("hid.dll")
	procGetPreparsedData                = hid.NewProc("HidD_GetPreparsedData")
	procFreePreparsedData               = hid.NewProc("HidD_FreePreparsedData")
	procGetCaps                         = hid.NewProc("HidP_GetCaps")
	procSetOutputReport                 = hid.NewProc("HidD_SetOutputReport")
	guidDevinterfaceHID                 = windows.GUID{Data1: 0x4d1e55b2, Data2: 0xf16f, Data3: 0x11cf, Data4: [8]byte{0x88, 0xcb, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30}}
	cmGetDeviceInterfacePresent uintptr = 0
)

const hidpStatusSuccess = 0x00110000

// hidCaps is HIDP_CAPS from hidpi.h.
type hidCaps struct {
	Usage, UsagePage                                                       uint16
	InputReportByteLength, OutputReportByteLength, FeatureReportByteLength uint16
	Reserved                                                               [17]uint16
	NumberLinkCollectionNodes                                              uint16
	NumberInputButtonCaps, NumberInputValueCaps, NumberInputDataIndices    uint16
	NumberOutputButtonCaps, NumberOutputValueCaps, NumberOutputDataIndices uint16
	NumberFeatureButtonCaps, NumberFeatureValueCaps                        uint16
	NumberFeatureDataIndices                                               uint16
}

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
	cols := hidCollections(paths, VendorID, ProductID, ConfigInterface)
	if len(cols) == 0 {
		return ErrNotConnected
	}
	var failures []string
	for _, path := range cols {
		err := writeCollection(path, packets)
		if err == nil {
			return nil
		}
		failures = append(failures, collectionName(path)+": "+err.Error())
	}
	return fmt.Errorf("writing to the pad's config interface failed (%s)", strings.Join(failures, "; "))
}

// writeCollection sends the packets to one HID collection of the pad.
func writeCollection(path string, packets [][]byte) error {
	h, err := openHID(path)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	caps, err := getCaps(h)
	if err != nil {
		return err
	}
	where := fmt.Sprintf("usage page 0x%04x usage 0x%02x, output report %d bytes", caps.UsagePage, caps.Usage, caps.OutputReportByteLength)
	if caps.OutputReportByteLength == 0 {
		return errors.New(where + ": no output report")
	}
	forms := []string{"as report 3", "behind report ID 0"}
	form := -1 // the form the first packet got through with
	for i, p := range packets {
		reports := hidReports(p, int(caps.OutputReportByteLength))
		var errs []string
		sent := false
		for f, r := range reports {
			if form >= 0 && f != form {
				continue
			}
			if err := writeReport(h, r); err != nil {
				errs = append(errs, forms[f]+": "+err.Error())
				continue
			}
			form, sent = f, true
			break
		}
		if !sent {
			return fmt.Errorf("%s: packet %d of %d: %s", where, i+1, len(packets), strings.Join(errs, "; "))
		}
	}
	return nil
}

func openHID(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE|windows.GENERIC_READ, share, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		h, err = windows.CreateFile(p, windows.GENERIC_WRITE, share, nil, windows.OPEN_EXISTING, 0, 0)
	}
	if err != nil {
		return 0, fmt.Errorf("open: %w", err)
	}
	return h, nil
}

func getCaps(h windows.Handle) (hidCaps, error) {
	var caps hidCaps
	var data uintptr
	if r, _, err := procGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&data))); r == 0 {
		return caps, fmt.Errorf("HidD_GetPreparsedData: %v", err)
	}
	defer procFreePreparsedData.Call(data)
	if r, _, _ := procGetCaps.Call(data, uintptr(unsafe.Pointer(&caps))); r != hidpStatusSuccess {
		return caps, fmt.Errorf("HidP_GetCaps: status 0x%08x", r)
	}
	return caps, nil
}

// writeReport sends one output report down the interrupt pipe (WriteFile)
// and, when the driver refuses, as a SET_REPORT control transfer.
func writeReport(h windows.Handle, report []byte) error {
	var done uint32
	err := windows.WriteFile(h, report, &done, nil)
	if err == nil && int(done) != len(report) {
		err = fmt.Errorf("short write (%d of %d bytes)", done, len(report))
	}
	if err == nil {
		return nil
	}
	if r, _, err2 := procSetOutputReport.Call(uintptr(h), uintptr(unsafe.Pointer(&report[0])), uintptr(len(report))); r == 0 {
		return fmt.Errorf("WriteFile: %v; HidD_SetOutputReport: %v", err, err2)
	}
	return nil
}

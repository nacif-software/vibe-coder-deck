package pad

import (
	"fmt"
	"strings"
)

// hidReports are the forms a config packet can take as a Windows HID output
// report of size bytes (the HID class driver wants exactly the collection's
// OutputReportByteLength): as it is, its first byte 0x03 being the report ID,
// and behind a 0 for a descriptor whose output report carries no ID. Packets
// are zero padded, so cutting them to size loses nothing.
func hidReports(packet []byte, size int) [][]byte {
	if size <= 0 {
		return nil
	}
	fit := func(b []byte) []byte {
		out := make([]byte, size)
		copy(out, b)
		return out
	}
	return [][]byte{fit(packet), fit(append([]byte{0}, packet...))}
}

// hidCollections picks the HID device interface paths of one USB interface
// of a device, in the order given: Windows makes one path per top-level
// collection ("&col01", "&col02") and spells them in either case.
func hidCollections(paths []string, vendor, product, iface int) []string {
	id := fmt.Sprintf("vid_%04x&pid_%04x", vendor, product)
	mi := fmt.Sprintf("&mi_%02x", iface)
	var out []string
	for _, p := range paths {
		l := strings.ToLower(p)
		if strings.Contains(l, id) && strings.Contains(l, mi) {
			out = append(out, p)
		}
	}
	return out
}

// collectionName is the "colNN" part of a HID device path (lower case), or
// the path when it has none.
func collectionName(path string) string {
	l := strings.ToLower(path)
	i := strings.Index(l, "&col")
	if i < 0 {
		return path
	}
	end := i + 1
	for end < len(l) && l[end] != '#' && l[end] != '&' {
		end++
	}
	return l[i+1 : end]
}

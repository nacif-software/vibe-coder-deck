package pad

import (
	"bytes"
	"testing"
)

func TestHIDReportsFitTheCollectionsOutputReport(t *testing.T) {
	p := packet(0xA1, 1) // select layer 1: 0x03 0xA1 0x01, zero padded to 64
	cases := []struct {
		size int
		want [][]byte
	}{
		{65, [][]byte{append(append([]byte{}, p...), 0), append([]byte{0}, p...)}},
		{64, [][]byte{p, append([]byte{0}, p[:63]...)}},
		{2, [][]byte{{0x03, 0xA1}, {0x00, 0x03}}},
		{0, nil},
	}
	for _, c := range cases {
		got := hidReports(p, c.size)
		if len(got) != len(c.want) {
			t.Fatalf("size %d: %d forms, want %d", c.size, len(got), len(c.want))
		}
		for i := range got {
			if len(got[i]) != c.size {
				t.Errorf("size %d form %d: %d bytes", c.size, i, len(got[i]))
			}
			if !bytes.Equal(got[i], c.want[i]) {
				t.Errorf("size %d form %d:\n got % x\nwant % x", c.size, i, got[i], c.want[i])
			}
		}
	}
	if !bytes.Equal(p, packet(0xA1, 1)) {
		t.Error("the packet was modified")
	}
}

func TestHIDCollectionsAreThePadsConfigInterface(t *testing.T) {
	paths := []string{
		`\\?\hid#vid_1189&pid_8890&mi_00#7&1a2b3c4d&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
		`\\?\HID#VID_1189&PID_8890&MI_01&Col01#7&1a2b3c4d&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
		`\\?\hid#vid_046d&pid_c52b&mi_01&col02#8&2b3c4d5e&0&0001#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
		`\\?\hid#vid_1189&pid_8890&mi_01&col02#7&1a2b3c4d&0&0001#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
		`\\?\hid#vid_1189&pid_8890&mi_02#7&1a2b3c4d&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
	}
	got := hidCollections(paths, VendorID, ProductID, ConfigInterface)
	want := []string{paths[1], paths[3]}
	if len(got) != len(want) {
		t.Fatalf("got %d paths %q, want %q", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d: got %q, want %q", i, got[i], want[i])
		}
	}
	if got := hidCollections(nil, VendorID, ProductID, ConfigInterface); got != nil {
		t.Errorf("no paths: got %q", got)
	}
}

func TestCollectionNameIsTheColPart(t *testing.T) {
	cases := map[string]string{
		`\\?\HID#VID_1189&PID_8890&MI_01&Col02#7&1a2b3c4d&0&0001#{4d1e55b2-f16f-11cf-88cb-001111000030}`: "col02",
		`\\?\hid#vid_1189&pid_8890&mi_01#7&1a2b3c4d&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`:       `\\?\hid#vid_1189&pid_8890&mi_01#7&1a2b3c4d&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
	}
	for in, want := range cases {
		if got := collectionName(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

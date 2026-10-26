package pad

import "fmt"

// USB writes the pad directly over USB with the protocol in protocol.go: no
// macropad CLI, no libusb. macOS talks IOKit, Linux the hidraw node of the
// config interface, Windows the HID device of that interface.
type USB struct{}

// NewUSB is the default Writer.
func NewUSB() *USB { return &USB{} }

func (u *USB) Set(slot, action string) error {
	a, err := ParseAction(action)
	if err != nil {
		return fmt.Errorf("%s: %w", slot, err)
	}
	packets, err := BindPackets(slot, 1, &a)
	if err != nil {
		return err
	}
	return sendPackets(packets)
}

func (u *USB) Clear(slot string) error {
	packets, err := BindPackets(slot, 1, nil)
	if err != nil {
		return err
	}
	return sendPackets(packets)
}

// SetLED sets the backlight mode (0 off, 1 last key lit, 2 rainbow).
func (u *USB) SetLED(mode int) error {
	packets, err := LEDPackets(mode)
	if err != nil {
		return err
	}
	return sendPackets(packets)
}

// The config interface of the pad: USB interface 1, whose only endpoint is
// an interrupt OUT pipe.
const ConfigInterface = 1

func flatten(packets [][]byte) []byte {
	flat := make([]byte, 0, len(packets)*PacketSize)
	for _, p := range packets {
		flat = append(flat, p...)
	}
	return flat
}

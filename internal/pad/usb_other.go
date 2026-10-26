//go:build !darwin && !linux && !windows

package pad

import "errors"

func sendPackets(packets [][]byte) error {
	return errors.New("writing the pad is not supported on this system")
}

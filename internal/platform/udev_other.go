//go:build !linux

package platform

import "errors"

// InstallUdevRule is for Linux; elsewhere there is nothing to install.
func InstallUdevRule() error { return errors.New("udev rules are only for Linux") }

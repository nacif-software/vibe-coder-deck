package platform

import _ "embed"

// UdevRule gives the logged-in Linux user access to the pad's input and
// hidraw devices and to /dev/uinput (the "uaccess" tag, honoured by
// systemd-logind). It is embedded so every build can print it.
//
//go:embed udev/70-vibe-coder-deck.rules
var UdevRule string

// UdevRulePath is where the rule goes on a Linux system.
const UdevRulePath = "/etc/udev/rules.d/70-vibe-coder-deck.rules"

package usbgadget

import "slices"

// OwnGadgetPrefix namespaces ConfigFS USB gadgets owned by any2jb so readiness
// checks and lifecycle cleanup distinguish them from foreign gadgets.
const OwnGadgetPrefix = "any2jb-"

var reservedUDCNames = []string{
	"network", "sip", "driver-present", "driver-loaded", "driver-restart",
	"usb-controllers", "usb-platform", "usb-runtime", "runtime", "engine",
	"input-adapter", "linux-udc", "linux-configfs", "linux-gadget-access",
}

// ValidUDCName accepts sysfs UDC directory names while rejecting path
// traversal, reserved check IDs, and ambiguous placeholders such as "auto".
func ValidUDCName(name string) bool {
	if name == "" || len(name) > 128 || name == "." || name == ".." || name == "auto" {
		return false
	}
	if slices.Contains(reservedUDCNames, name) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' || c == ':') {
			return false
		}
	}
	return true
}

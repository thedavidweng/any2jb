package usbgadget_test

import (
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/usbgadget"
)

func TestValidUDCNameEnforcesGadgetPolicy(t *testing.T) {
	for _, valid := range []string{"dummy_udc.0", "fe980000.usb", "10004800.usb:0"} {
		if !usbgadget.ValidUDCName(valid) {
			t.Errorf("rejected valid UDC name %q", valid)
		}
	}
	for _, invalid := range []string{
		"", ".", "..", "auto", "network", "linux-udc",
		"../udc0", "udc/0", "udc 0", strings.Repeat("a", 129),
	} {
		if usbgadget.ValidUDCName(invalid) {
			t.Errorf("accepted invalid UDC name %q", invalid)
		}
	}
}

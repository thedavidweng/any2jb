//go:build darwin

package readiness

import (
	"slices"
	"testing"
	"time"
)

func TestRegistryObservation(t *testing.T) {
	fixture := `+-o usb-drd10@a <class AppleARMIODevice>
  | +-o gay_bowser_usbgadget <class gay_bowser_usbgadget>
+-o unrelated@b <class AppleARMIODevice>
  +-o gay_bowser_usbgadget <class gay_bowser_usbgadget>
+-o usb-drd1@c <class AppleARMIODevice>
  +-o no_gadget <class something>
+-o usb-drd2@d <class AppleARMIODevice>
  +-o gay_bowser_usbgadget <class gay_bowser_usbgadget>
+-o usb-drd16@e <class AppleARMIODevice>
  +-o gay_bowser_usbgadget <class gay_bowser_usbgadget>
`
	if got := parseControllers(fixture); !slices.Equal(got, []string{"usb-drd2", "usb-drd10"}) {
		t.Fatal(got)
	}
	if got := parseControllers("no matching IOService nodes"); len(got) != 0 {
		t.Fatal("invented controllers", got)
	}
}

func TestDriverObservation(t *testing.T) {
	for _, tc := range []struct {
		text   string
		loaded bool
	}{
		{"1 0 0x123 gay.bowser.usbgadget (1.0)", true},
		{"1 0 0x123 gay.bowser.usbgadget-other (1.0)", false},
		{"usbgadget", false},
	} {
		if got := parseLoaded(tc.text); got != tc.loaded {
			t.Fatal(tc, got)
		}
	}
	boot := time.Unix(1000, 0)
	if !restartPending(time.Unix(1001, 0), boot) || restartPending(time.Unix(999, 0), boot) {
		t.Fatal("restart marker did not track boot")
	}
}

func TestSIPOutputIsExactAndConservative(t *testing.T) {
	for input, want := range map[string]string{
		"System Integrity Protection status: enabled.\n":                      "enabled",
		"System Integrity Protection status: disabled.\n":                     "disabled",
		"System Integrity Protection status: unknown (Custom Configuration).": "unknown",
		"disabled": "unknown",
		"":         "unknown",
	} {
		if got := parseSIPStatus(input); got != want {
			t.Fatalf("%q: %s", input, got)
		}
	}
}

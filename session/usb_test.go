package session_test

import (
	"testing"

	"github.com/thedavidweng/any2jb/session"
)

func TestOnlySelectedObservedControllerCanBePromotedByKeyboardReady(t *testing.T) {
	checks := []session.Check{
		{ID: "driver-loaded", State: session.RequiresAttention, GatesTransport: "usb-hid"},
		{ID: "usb-controllers", State: session.RequiresAttention, GatesTransport: "usb-hid"},
		{ID: "usb-drd0", Controller: "usb-drd0", State: session.Available, GatesTransport: "usb-hid"},
		{ID: "usb-drd1", Controller: "usb-drd1", State: session.Available, GatesTransport: "usb-hid"},
	}
	for _, ready := range []string{"", "usb-drd1", "usb-drd2"} {
		got := session.ObserveKeyboardEnumeration(checks, "usb-drd0", ready)
		if got[1].State != session.RequiresAttention || got[2].State != session.Available {
			t.Fatalf("discovery/mismatched evidence promoted a controller: %+v", got)
		}
	}
	got := session.ObserveKeyboardEnumeration(checks, "usb-drd0", "usb-drd0")
	if got[0].State != session.RequiresAttention || got[1].State != session.CheckVerified || got[2].State != session.CheckVerified || got[3].State != session.Available {
		t.Fatalf("wrong evidence promotion: %+v", got)
	}
	transport := session.Transport{Mode: "keyboard", Controller: "usb-drd0", Capabilities: session.Capabilities{Keyboard: true}}
	if !got[0].Blocks(transport) || got[3].Blocks(transport) {
		t.Fatal("driver blocker lost or unselected controller blocks the selected one")
	}
	if checks[2].State != session.Available {
		t.Fatal("promotion mutated the original observation")
	}
}

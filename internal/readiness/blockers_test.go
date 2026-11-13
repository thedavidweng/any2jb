package readiness_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/thedavidweng/any2jb/internal/readiness"
	"github.com/thedavidweng/any2jb/internal/session"
)

func TestIndividualReadinessBlockers(t *testing.T) {
	for _, tc := range []struct {
		name, id, action string
		change           func(*readiness.Observation)
	}{
		{"network access", "network", "Check network interface access, then refresh readiness.", func(o *readiness.Observation) { o.NetworkError = errors.New("private details") }},
		{"no network", "network", "Connect this Mac to the console LAN, then refresh readiness.", func(o *readiness.Observation) { o.Addresses = nil }},
		{"ambiguous network", "network", "Choose an assigned LAN IPv4 address below, then refresh readiness.", func(o *readiness.Observation) {
			o.Addresses = append(o.Addresses, readiness.Address{Interface: "en1", IPv4: "10.0.0.2"})
		}},
		{"driver absent", "driver-present", "Review driver setup and macOS approval requirements, or choose guided manual input.", func(o *readiness.Observation) { o.Driver.Present = false }},
		{"driver presence error", "driver-present", "Review driver setup and macOS approval requirements, or choose guided manual input.", func(o *readiness.Observation) { o.Driver.PresentError = errors.New("private details") }},
		{"driver unloaded", "driver-loaded", "Complete macOS driver approval, restart if requested, then refresh readiness; or choose guided manual input.", func(o *readiness.Observation) { o.Driver.Loaded = false }},
		{"restart pending", "driver-restart", "Restart this Mac, then refresh readiness before using USB input.", func(o *readiness.Observation) { o.Driver.RestartPending = true }},
		{"restart unknown", "driver-restart", "Check the driver restart marker and system boot time, then refresh readiness.", func(o *readiness.Observation) { o.Driver.RestartError = errors.New("private details") }},
		{"driver inspection error", "driver-loaded", "Check driver inspection access, then refresh readiness; or choose guided manual input.", func(o *readiness.Observation) { o.Driver.LoadError = errors.New("private details") }},
		{"controller discovery error", "usb-controllers", "Check USB registry access and refresh readiness, or choose guided manual input.", func(o *readiness.Observation) { o.ControllerError = errors.New("private details") }},
		{"no controllers", "usb-controllers", "Check driver loading and the USB-C data cable direction, then refresh readiness; or choose guided manual input.", func(o *readiness.Observation) { o.Controllers = nil }},
		{"unsupported platform", "usb-platform", "Choose guided manual input. Ordinary USB-C ports do not establish USB device-mode support.", func(o *readiness.Observation) { o.USBSupported = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := readiness.Observation{Addresses: []readiness.Address{{Interface: "en0", IPv4: "192.168.1.10"}},
				USBSupported: true, Driver: readiness.Driver{Present: true, Loaded: true}, Controllers: []string{"usb-drd0"}}
			tc.change(&o)
			p := readiness.Inspector{Observe: func(context.Context) readiness.Observation { return o }}
			checks, err := p.Inspect(context.Background(), session.Run{})
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(checks, func(c session.Check) bool { return c.ID == tc.id })
			if i < 0 || checks[i].State != session.RequiresAttention || checks[i].NextAction != tc.action {
				t.Fatalf("wanted %s / %q, got %+v", tc.id, tc.action, checks)
			}
			if tc.id != "network" && checks[i].GatesTransport != "usb-hid" {
				t.Fatal("USB check gates manual input")
			}
			if tc.name == "restart pending" {
				i := slices.IndexFunc(checks, func(c session.Check) bool { return c.ID == "driver-loaded" })
				if checks[i].State != session.RequiresAttention {
					t.Fatal("pending restart still loaded")
				}
			}
		})
	}
}

func TestControllerDiscoveryIsNotUSBVerification(t *testing.T) {
	o := readiness.Observation{USBSupported: true, Controllers: []string{"usb-drd0", "usb-drd1"}}
	p := readiness.Inspector{Observe: func(context.Context) readiness.Observation { return o }}
	checks, _ := p.Inspect(context.Background(), session.Run{})
	for _, id := range []string{"usb-drd0", "usb-drd1"} {
		i := slices.IndexFunc(checks, func(c session.Check) bool { return c.ID == id })
		if i < 0 || checks[i].State != session.Available ||
			checks[i].NextAction != "Connect a USB-C data cable so the Mac receives power from the console; verify keyboard enumeration before sending input." {
			t.Fatal(checks)
		}
	}
	i := slices.IndexFunc(checks, func(c session.Check) bool { return c.ID == "usb-controllers" })
	if i < 0 || checks[i].State != session.RequiresAttention || checks[i].NextAction != "More than one controller was discovered. Verify the connected controller before using USB input, or choose guided manual input." {
		t.Fatal(checks)
	}
}

package readiness_test

import (
	"context"
	"slices"
	"testing"

	"github.com/thedavidweng/any2jb/internal/readiness"
	"github.com/thedavidweng/any2jb/internal/session"
)

func TestSIPReadinessIsIndependentAndUSBScoped(t *testing.T) {
	for _, status := range []string{"disabled", "enabled", "unknown"} {
		t.Run(status, func(t *testing.T) {
			o := readiness.Observation{USBSupported: true, SIPStatus: status}
			checks, err := (readiness.Inspector{Observe: func(context.Context) readiness.Observation { return o }}).Inspect(context.Background(), session.Run{})
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(checks, func(c session.Check) bool { return c.ID == "sip" })
			if i < 0 {
				t.Fatal("missing SIP readiness check")
			}
			c := checks[i]
			if c.GatesTransport != "usb-hid" || (c.State == session.CheckVerified) != (status == "disabled") {
				t.Fatal(c)
			}
			if c.Blocks(session.Transport{Capabilities: session.Capabilities{Manual: true}}) {
				t.Fatal("SIP gates guided manual input")
			}
			if c.Blocks(session.Transport{Capabilities: session.Capabilities{Keyboard: true}}) != (status == "enabled") {
				t.Fatal("only observed enabled SIP blocks USB", c)
			}
			if status != "disabled" && c.NextAction == "" {
				t.Fatal("SIP blocker has no next action")
			}
		})
	}
}

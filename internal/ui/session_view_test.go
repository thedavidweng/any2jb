package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/internal/transport"
	"github.com/thedavidweng/any2jb/locales"
)

// TestBlockedStartKeepsItsTransportLabel holds the primary action's name to the
// selected transport. A blocked automatic launch must not be relabelled as a
// guided manual launch it would not perform.
func TestBlockedStartKeepsItsTransportLabel(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{
		ID: "usb-runtime", State: session.RequiresAttention, GatesTransport: "usb-hid",
		Message:    "Verified USB helpers are not available.",
		NextAction: "Permit the helper build before launching.",
	}}
	c := session.New(f.Environment(), session.Profile{ID: "fixture", Revision: "pinned", SaveName: "NS2"})
	if _, err := c.ConfigureTransport(session.Transport{
		Mode:         "keyboard",
		Capabilities: session.Capabilities{Keyboard: true},
	}, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	a := testApp(t, c)
	a.Manual = transport.NewManual(func(session.Run) (string, error) { return "", nil })
	tt := ui.NewTester(a.view, 700, 1000)
	if err := tt.Click(locales.CheckAgain()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.NeedsSetup)
	if s := a.Controller.Snapshot(); s.CanStart() {
		t.Fatalf("the fixture is not blocked: %+v", s.Checks)
	}
	// The first stage recommends manual input instead and says so.
	if !tt.HasText(locales.MacInstallBlocked()) || !tt.HasText(locales.FlowUsbUnavailable()) {
		t.Errorf("the first stage does not explain its recommendation: %q", tt.Texts())
	}
	// Reaching launch with the blocked USB method keeps that method's name.
	a.flow.stage = flowLaunch
	tt.Frame()
	start, ok := tt.AccessByRole(ui.RoleButton, locales.StartJailbreak())
	if !ok || !start.Disabled {
		t.Fatalf("a blocked USB launch is not held: %+v", start)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, locales.GuidedManualLaunch()); ok {
		t.Fatal("the primary action was renamed to a guided manual launch")
	}
}

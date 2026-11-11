package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

// TestBlockedStartKeepsItsTransportLabel holds the primary action's name to the
// selected transport. A blocked automatic launch must not be relabelled as a
// guided manual launch it would not perform.
func TestBlockedStartKeepsItsTransportLabel(t *testing.T) {
	f := session.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "usb-runtime", State: session.RequiresAttention,
		Message:    "Verified USB helpers are not available.",
		NextAction: "Permit the helper build before launching."}}
	c := session.New(f.Environment(), session.Profile{ID: "fixture", Revision: "pinned", SaveName: "NS2"})
	if _, err := c.ConfigureTransport(session.Transport{Mode: "keyboard",
		Capabilities: session.Capabilities{Keyboard: true}}, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	a := testApp(t, c)
	a.manual = transport.NewManual(func(session.Run) (string, error) { return "", nil })
	tt := ui.NewTester(a.view, 700, 1000)
	if err := tt.Click(locales.CheckReadiness()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.NeedsSetup)
	if s := a.controller.Snapshot(); s.CanStart() {
		t.Fatalf("the fixture is not blocked: %+v", s.Checks)
	}
	start, ok := tt.AccessByRole(ui.RoleButton, locales.StartJailbreak())
	if !ok || !start.Disabled {
		t.Fatalf("a blocked automatic launch is not a disabled Start button: %+v", start)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, locales.GuidedManualLaunch()); ok {
		t.Fatal("the primary action was renamed to a guided manual launch")
	}
	if !tt.HasText("Permit the helper build before launching.") {
		t.Errorf("the blocking reason is not shown: %q", tt.Texts())
	}
}

package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

func TestCivSwitchPanelStatesUnavailableReasonWithoutSmileBASICControls(t *testing.T) {
	a := &app{controller: session.New(session.NewFakeEnvironment().Environment(), session.Profile{ID: "test", Revision: "pinned"})}
	tt := ui.NewTester(a.view, 900, 1800)
	if err := tt.Click("CivSwitch"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{locales.CivPrerequisites(), locales.CivNoRPC(), locales.CivComposite(), a.controller.CivSwitchUnavailable()} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q: %v", text, tt.Texts())
		}
	}
	for _, text := range []string{locales.CivStart(), locales.CivAttach()} {
		if control, ok := tt.AccessByRole(ui.RoleButton, text); !ok || !control.Disabled {
			t.Fatalf("unavailable action %q is usable", text)
		}
	}
	for _, text := range []string{locales.GuidedManualLaunch(), locales.SmilebasicPrerequisites(), locales.PrepareBootstrap()} {
		if tt.HasText(text) {
			t.Fatalf("SmileBASIC action leaked into CivSwitch: %q", text)
		}
	}
	if err := tt.Click(locales.CivBack()); err != nil {
		t.Fatal(err)
	}
	if a.civSwitchPanel.open {
		t.Fatal("could not leave unavailable path")
	}
}

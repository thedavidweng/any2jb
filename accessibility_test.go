package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
)

func newTestApp(t *testing.T) *app {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := &app{manifest: m, preferences: preferences{Profile: m.Default, Edition: "full", Transport: "manual"}}
	if err := a.initializeSession(session.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	p := a.controller.Snapshot().Profile
	a.controller.UseSavedBootstrap(session.BootstrapRecord{
		ProfileID: p.ID, Revision: p.Revision, SourceSHA256: p.SourceSHA256,
		BootstrapSHA256: p.BootstrapSHA256, SaveName: p.SaveName, Edition: "full", Confirmed: true,
	})
	return a
}

func focusedName(tt *ui.Tester) string {
	for _, info := range tt.AccessAll() {
		if info.Focused {
			return info.Name
		}
	}
	return ""
}

func TestAccessibleControlsHaveNames(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 800, 1000)
	if err := tt.Click(locales.CheckReadiness()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	for _, info := range tt.AccessAll() {
		switch info.Role {
		case ui.RoleButton, ui.RoleCheckBox, ui.RoleTextField, ui.RolePopUpButton, ui.RoleComboBox:
			if info.Name == "" {
				t.Errorf("%v control without an accessible name", info.Role)
			}
		}
	}
	start, ok := tt.AccessByRole(ui.RoleButton, locales.GuidedManualLaunch())
	if !ok || start.Disabled {
		t.Fatal("guided manual launch should be an enabled button once ready")
	}
}

func TestSessionStatusIsSemantic(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 800, 1000)
	if err := tt.Click(locales.CheckReadiness()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	if _, ok := tt.AccessByRole(ui.RoleStatus, locales.ReadyToLaunch()); !ok {
		t.Error("session stage is not exposed as a status live region")
	}
	if _, ok := tt.AccessByRole(ui.RoleStatus, locales.CheckVerified()); !ok {
		t.Error("readiness check state is not exposed as a status live region")
	}
}

func TestTabOrderFollowsVisualOrder(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 800, 1000)
	if err := tt.Click(locales.CheckReadiness()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	expected := []string{
		locales.CivTitle(), locales.ToolsTitle(), locales.LanOverride(),
		locales.ApplyPreferences(), locales.DeveloperOptions(), locales.UsbKeyboardTitle(),
		locales.CheckReadiness(), locales.GuidedManualLaunch(), locales.PrepareBootstrap(), locales.AboutBuild(),
	}
	seen := []string{}
	for range 40 {
		tt.Key(0, ui.KeyTab)
		name := focusedName(tt)
		if name == "" || (len(seen) > 0 && seen[len(seen)-1] == name) {
			continue
		}
		seen = append(seen, name)
	}
	// Focus can start anywhere after the click, so match the visual order
	// cyclically and require the traversal to wrap around the window.
	lap := append(append([]string{}, seen...), seen...)
	next := 0
	for _, name := range lap {
		if next < len(expected) && name == expected[next] {
			next++
		}
	}
	if next != len(expected) {
		t.Fatalf("tab order %q does not contain the visual sequence %q", seen, expected)
	}
	if len(seen) < len(expected)+1 {
		t.Fatalf("focus did not cycle through the window: %q", seen)
	}
}

package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/diagnostics"
	"github.com/thedavidweng/any2jb/driver"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func newTestApp(t *testing.T) *app {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := &app{manifest: m, preferences: preferences{Profile: m.Default, Edition: "full", Transport: "manual"}}
	if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
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

// TestPanelScreensAreAccessible applies the name sweep and the tab traversal to
// each panel, not only to the main session screen: every focusable control is
// named, the tab order reaches all of them, and focus wraps back to its start.
func TestPanelScreensAreAccessible(t *testing.T) {
	screens := []struct {
		name string
		view func(t *testing.T) func(*ui.Context)
	}{
		{"Advanced Tools", func(t *testing.T) func(*ui.Context) {
			adapter := &fakeToolAdapter{state: retained.State{SaveKit: &retained.SaveKitState{
				Users: []string{"11111111111111111111111111111111"}}}}
			a := retainedApp(t, sessiontest.NewFakeEnvironment(), adapter, []string{"rpc-status", "savekit"})
			tt := ui.NewTester(a.advancedToolsView, 900, 2600)
			if _, err := a.controller.Inspect(); err != nil {
				t.Fatal(err)
			}
			uiStage(t, a, tt, session.Ready)
			if _, err := a.controller.Start(); err != nil {
				t.Fatal(err)
			}
			uiStage(t, a, tt, session.RetainedRPC)
			return a.advancedToolsView
		}},
		{"SaveKit", func(t *testing.T) func(*ui.Context) {
			a, _, _, _ := leasedSaveKit(t, map[string]string{"save.bin": "save\n"})
			return a.saveKitView
		}},
		{"Diagnostics", func(t *testing.T) func(*ui.Context) {
			a := &app{manifest: testManifest(t), controller: session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test"}),
				diagnostic: diagnosticUI{ready: true, logs: diagnostics.Logs{Host: "host failure"}}}
			return a.diagnosticsView
		}},
		{"Driver", func(t *testing.T) func(*ui.Context) {
			a := newTestApp(t)
			a.driverPanel.gate = driver.New(driver.Options{Manifest: a.manifest})
			a.driverPanel.open = true
			return a.view
		}},
		{"CivSwitch", func(t *testing.T) func(*ui.Context) {
			a := &app{manifest: testManifest(t), controller: session.New(sessiontest.NewFakeEnvironment().Environment(),
				session.Profile{ID: "test", Revision: "pinned"})}
			a.civSwitchPanel.open = true
			return a.civSwitchView
		}},
	}
	for _, screen := range screens {
		t.Run(screen.name, func(t *testing.T) {
			tt := ui.NewTester(screen.view(t), 900, 2600)
			focusable := map[string]bool{}
			for _, info := range tt.AccessAll() {
				if info.Focusable && info.Name == "" {
					t.Errorf("%v control without an accessible name", info.Role)
				}
				if info.Focusable && !info.Disabled && info.Name != "" {
					focusable[info.Name] = true
				}
			}
			if len(focusable) == 0 {
				t.Fatal("the screen has no focusable control to traverse")
			}
			seen := map[string]bool{}
			start := focusedName(tt)
			for range 4*len(focusable) + 16 {
				tt.Key(0, ui.KeyTab)
				name := focusedName(tt)
				if name == "" {
					continue
				}
				if start == "" {
					start = name
				} else if name == start {
					break
				}
				seen[name] = true
			}
			for name := range focusable {
				if !seen[name] {
					t.Errorf("the tab order never reaches %q", name)
				}
			}
		})
	}
}

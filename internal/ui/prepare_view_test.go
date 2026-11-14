package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/locales"
)

// passPrepare ticks the selected route's checklist and continues to its setup.
func passPrepare(t *testing.T, a *app, tt *ui.Tester) {
	t.Helper()
	if a.flow.stage != flowPrepare {
		t.Fatalf("not preparing the Switch: stage %d", a.flow.stage)
	}
	for _, label := range prepareLabels(a.civSwitchPanel.open) {
		if box, ok := tt.AccessByRole(ui.RoleCheckBox, label); !ok || !box.Checked {
			clickManual(t, tt, label)
		}
	}
	clickManual(t, tt, locales.FlowNextSelection())
	if a.flow.stage != flowSelection {
		t.Fatalf("the ticked checklist did not continue: stage %d", a.flow.stage)
	}
}

func prepareLabels(civ bool) []string {
	shared := []string{locales.PrepNetwork(), locales.PrepUpdates(), locales.PrepCable()}
	if civ {
		return append(shared, locales.CivCartridge(), locales.CivLanguage(), locales.CivChat())
	}
	return append(shared, locales.PrepVersion(), locales.PrepSmilebasic(), locales.PrepKeyboard(), locales.PrepDirect())
}

// compositeApp is at the prepare stage with an input method CivSwitch can use.
func compositeApp(t *testing.T, path string) (*app, *ui.Tester) {
	t.Helper()
	f := sessiontest.NewFakeEnvironment()
	f.Checks = append(f.Checks, usbCheck("driver-loaded", session.CheckVerified))
	env := f.Environment()
	env.CivSwitch = &civPanelAdapter{}
	a, err := loadApp(path)
	if err != nil {
		t.Fatal(err)
	}
	a.driverPanel.gate = blockedDriverGate(a.Manifest)
	a.Preferences.Transport = "composite"
	if err := a.openSession(env, false); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 720, 1400)
	uiStage(t, a, tt, session.Ready)
	clickManual(t, tt, locales.FlowNextSelection())
	waitIdle(t, a, tt)
	if a.flow.stage != flowPrepare {
		t.Fatalf("Continue did not open Prepare the Switch: stage %d", a.flow.stage)
	}
	return a, tt
}

// TestPrepareChecklist covers each route's items, the Continue gate and its
// reason, and the guide link, in both languages and at both widths.
func TestPrepareChecklist(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		for _, civ := range []bool{false, true} {
			route := map[bool]string{false: "smile", true: "civ"}[civ]
			t.Run(language+"/"+route, func(t *testing.T) {
				withLocale(t, language)
				a, tt := compositeApp(t, filepath.Join(t.TempDir(), "preferences.json"))
				if civ {
					clickManual(t, tt, locales.CivTitle())
				}
				tt.Advance(time.Second)
				want := prepareLabels(civ)
				for _, label := range append(prepareLabels(!civ), want...) {
					_, shown := tt.AccessByRole(ui.RoleCheckBox, label)
					if shown != contains(want, label) {
						t.Fatalf("%q shown = %v on the %s route", label, shown, route)
					}
					// Confirmations are not observations: they never carry a state.
					if _, ok := tt.AccessByRole(ui.RoleStatus, label); ok {
						t.Fatalf("%q is presented as a state", label)
					}
				}
				if !tt.HasText(locales.PrepNotSaved()) {
					t.Fatal("the checklist does not say it is asked again")
				}
				for _, w := range []int{720, 560} {
					tt.SetSize(w, 1000)
					tt.Frame()
					renderFlow(t, tt, fmt.Sprintf("%s-prepare-%s-unticked-%d", language, route, w))
				}
				tt.SetSize(720, 1400)
				tt.Frame()
				clickManual(t, tt, locales.PrepUpdatesLink())
				if opened := tt.OpenedURLs(); len(opened) == 0 || opened[len(opened)-1] != "https://gitlab.com/a/90dns" {
					t.Fatalf("the 90dns guide opened %q", opened)
				}
				for i, label := range want {
					if !disabled(t, tt, locales.FlowNextSelection()) || !tt.HasText(locales.PrepBlocked()) {
						t.Fatalf("Continue is usable, or gives no reason, with %d of %d ticked", i, len(want))
					}
					clickManual(t, tt, label)
				}
				if disabled(t, tt, locales.FlowNextSelection()) || tt.HasText(locales.PrepBlocked()) {
					t.Fatal("the fully ticked checklist still blocks Continue")
				}
				for _, w := range []int{720, 560} {
					tt.SetSize(w, 1000)
					tt.Frame()
					renderFlow(t, tt, fmt.Sprintf("%s-prepare-%s-ticked-%d", language, route, w))
				}
				// SmileBASIC's confirmations are the app's alone; the
				// controller's readiness never sees them.
				if !civ && a.Controller.Snapshot().CanStart() != testApp(t, a.Controller).Controller.Snapshot().CanStart() {
					t.Fatal("confirmations changed readiness")
				}
				tt.SetSize(720, 1400)
				tt.Frame()
				clickManual(t, tt, locales.FlowNextSelection())
				if a.flow.stage != flowSelection {
					t.Fatalf("Continue went to stage %d", a.flow.stage)
				}
				if _, ok := tt.AccessByRole(ui.RoleRadio, locales.CivTitle()); ok {
					t.Fatal("the route choice is still in route and setup")
				}
				route := map[bool]string{false: locales.FlowSmile(), true: locales.CivTitle()}[civ]
				if !tt.HasText(route) || !tt.HasText(locales.FlowRoute()) {
					t.Fatalf("route and setup does not name the chosen route: %v", tt.Texts())
				}
			})
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// The CivSwitch items are its launch prerequisites: what was ticked is what
// StartCivSwitch receives, and one unticked item keeps launch blocked.
func TestCivSwitchChecklistGatesItsLaunch(t *testing.T) {
	a, tt := compositeApp(t, filepath.Join(t.TempDir(), "preferences.json"))
	clickManual(t, tt, locales.CivTitle())
	for _, label := range prepareLabels(true) {
		clickManual(t, tt, label)
	}
	p := a.civSwitchPanel.prerequisites
	if !p.PhysicalSwitch1Cartridge100 || !p.EnglishUS || !p.SameReachableNetwork || !p.LobbyChatFocused {
		t.Fatalf("the checklist did not reach the prerequisites: %+v", p)
	}
	// One network item serves both the shared list and CivSwitch.
	if a.flow.prepared.network {
		t.Fatal("the CivSwitch network confirmation was stored twice")
	}
	clickManual(t, tt, locales.FlowNextSelection())
	a.civSwitchPanel.prerequisites.ConsoleIPv4 = "192.0.2.2"
	tt.Frame()
	clickManual(t, tt, locales.FlowNextLaunch())
	a.civSwitchPanel.prerequisites.LobbyChatFocused = false
	tt.Frame()
	if !disabled(t, tt, locales.CivStart()) {
		t.Fatal("CivSwitch launch is usable with an unticked prerequisite")
	}
	if _, err := a.Controller.StartCivSwitch(a.civSwitchPanel.prerequisites); err == nil {
		t.Fatal("the controller accepted an unticked prerequisite")
	}
	a.civSwitchPanel.prerequisites.LobbyChatFocused = true
	tt.Frame()
	clickManual(t, tt, locales.CivStart())
	uiStage(t, a, tt, session.Verified)
	if a.Controller.Snapshot().ConsoleIPv4 != "192.0.2.2" {
		t.Fatal("StartCivSwitch did not receive the confirmed prerequisites")
	}
	a.Controller.Finish()
}

// Every launch asks again: nothing ticked survives reopening the app, and
// nothing is written to the preferences file.
func TestPrepareConfirmationsAreNotPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	for _, civ := range []bool{false, true} {
		a, tt := compositeApp(t, path)
		if civ {
			clickManual(t, tt, locales.CivTitle())
		}
		for _, label := range prepareLabels(civ) {
			clickManual(t, tt, label)
		}
		if err := a.SavePreferences(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]any
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		for key := range saved {
			if !contains([]string{"usb", "profile", "edition", "lanIPv4", "transport"}, key) {
				t.Fatalf("preferences gained %q", key)
			}
		}
		if strings.Contains(strings.ToLower(string(data)), "prepar") || strings.Contains(string(data), "true") {
			t.Fatalf("a confirmation was saved: %s", data)
		}
		_, tt = compositeApp(t, path)
		if civ {
			clickManual(t, tt, locales.CivTitle())
		}
		for _, label := range prepareLabels(civ) {
			if box, ok := tt.AccessByRole(ui.RoleCheckBox, label); !ok || box.Checked {
				t.Fatalf("%q came back ticked after reopening: %+v", label, box)
			}
		}
		if !disabled(t, tt, locales.FlowNextSelection()) {
			t.Fatal("a reopened app skipped the checklist")
		}
	}
}

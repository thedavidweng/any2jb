package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestWizardSavedBootstrapStagesAndCornerAccess(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		t.Run(language, func(t *testing.T) {
			withLocale(t, language)
			path := filepath.Join(t.TempDir(), "preferences.json")
			a, err := loadApp(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
				t.Fatal(err)
			}
			if err := a.confirmExistingBootstrap(); err != nil {
				t.Fatal(err)
			}
			a, err = loadApp(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
				t.Fatal(err)
			}
			tt := ui.NewTester(a.view, 760, 1000)
			check := func(name string, present, absent []string) {
				t.Helper()
				for _, label := range present {
					if _, ok := tt.AccessByRole(ui.RoleButton, label); !ok {
						t.Fatalf("%s missing %q: %v", name, label, tt.Texts())
					}
				}
				for _, label := range absent {
					if _, ok := tt.AccessByRole(ui.RoleButton, label); ok {
						t.Fatalf("%s leaked %q", name, label)
					}
				}
				if focusedName(tt) != locales.FlowPosition(string(rune('1'+a.flow.stage)), []string{locales.FlowDetection(), locales.FlowSelection(), locales.FlowLaunch(), locales.FlowTools()}[a.flow.stage]) {
					t.Fatalf("%s did not focus its stage heading: %s", name, focusedName(tt))
				}
				if dir := os.Getenv("ANY2JB_FLOW_RENDER_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					file, err := os.Create(filepath.Join(dir, language+"-"+name+".png"))
					if err != nil {
						t.Fatal(err)
					}
					if err := png.Encode(file, tt.Image()); err != nil {
						t.Fatal(err)
					}
					if err := file.Close(); err != nil {
						t.Fatal(err)
					}
				}
				for _, corner := range []string{locales.Diagnostics(), locales.Settings()} {
					clickManual(t, tt, corner)
					if !tt.HasText(corner) {
						t.Fatalf("%s corner did not open at %s", corner, name)
					}
					clickManual(t, tt, corner)
				}
			}
			check("1-detection", []string{locales.CheckReadiness()}, []string{locales.GuidedManualLaunch(), locales.PrepareBootstrap(), locales.ToolsStatus()})
			clickManual(t, tt, locales.CheckReadiness())
			uiStage(t, a, tt, session.Ready)
			clickManual(t, tt, locales.FlowNextSelection())
			check("2-selection", []string{locales.FlowNextLaunch()}, []string{locales.CheckReadiness(), locales.PrepareBootstrap(), locales.GuidedManualLaunch(), locales.ToolsStatus()})
			if !tt.HasText(locales.FlowSaved()) {
				t.Fatal("matching saved bootstrap did not skip setup")
			}
			if _, ok := tt.AccessByRole(ui.RoleRadio, locales.CivTitle()); !ok {
				t.Fatal("route choice missing from route and setup")
			}
			clickManual(t, tt, locales.FlowNextLaunch())
			check("3-launch", []string{locales.GuidedManualLaunch()}, []string{locales.PrepareBootstrap(), locales.CheckReadiness(), locales.ToolsStatus()})
			if _, ok := tt.AccessByRole(ui.RoleRadio, locales.CivTitle()); ok {
				t.Fatal("route choice leaked into launch")
			}
			clickManual(t, tt, locales.GuidedManualLaunch())
			uiStage(t, a, tt, session.RetainedRPC)
			check("4-tools", []string{locales.ToolsStatus(), locales.Finish()}, []string{locales.GuidedManualLaunch(), locales.PrepareBootstrap(), locales.CheckReadiness()})
			if _, ok := tt.AccessByRole(ui.RoleButton, locales.ToolsStatus()); !ok {
				t.Fatal("verified tools are unreachable")
			}
			clickManual(t, tt, locales.Finish())
			uiStage(t, a, tt, session.Finished)
			if _, ok := tt.AccessByRole(ui.RoleButton, locales.ToolsStatus()); ok {
				t.Fatal("tools survived Finish")
			}

		})
	}
}

func TestInputMethodChoiceAppliesItselfAndChecksThatMethod(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 760, 1000)
	clickManual(t, tt, locales.FlowManual())
	clickManual(t, tt, locales.FlowKeyboard())
	tt.Frame()
	tt.Frame()
	if a.preferences.Transport != "keyboard" || a.controller.Snapshot().Transport.Mode != "keyboard" {
		t.Fatal("choosing a method did not apply it")
	}
	uiStage(t, a, tt, session.Ready)
	if len(a.controller.Snapshot().Checks) == 0 {
		t.Fatal("choosing a method did not check what that method needs")
	}
	if disabled(t, tt, locales.CheckReadiness()) {
		t.Fatal("the applied method cannot be checked again")
	}
}

func TestDetectionShowsOnlyItsOwnControls(t *testing.T) {
	a := testApp(t, session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 760, 1000)
	for _, label := range []string{"Guided manual launch", "Check live RPC status", "Prepare bootstrap", "CivSwitch"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, label); ok {
			t.Errorf("later-stage action %q appears during detection", label)
		}
	}
	for _, label := range []string{"Diagnostics", "Settings", "Continue to route and setup"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, label); !ok {
			t.Errorf("missing %q", label)
		}
	}
}

func TestUnreportedRouteChecksDoNotEnableContinueOrClaimReadiness(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "network", State: session.CheckVerified}}
	a := testApp(t, session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 760, 1400)
	clickManual(t, tt, locales.CheckReadiness())
	uiStage(t, a, tt, session.Ready)
	clickManual(t, tt, locales.FlowNextSelection())
	if !disabled(t, tt, locales.FlowNextLaunch()) {
		t.Fatal("unreported route checks enabled Continue")
	}
	if tt.HasText(locales.ReadyToLaunch()) || !tt.HasText(locales.RouteUnchecked()) {
		t.Fatal(tt.Texts())
	}
}

func TestInputMethodScopesDetectionGate(t *testing.T) {
	for _, mode := range []string{"manual", "keyboard", "composite"} {
		t.Run(mode, func(t *testing.T) {
			f := sessiontest.NewFakeEnvironment()
			f.Checks = []session.Check{
				{ID: "network", State: session.CheckVerified, Message: "LAN assigned."},
				{ID: "usb-platform", GatesTransport: "usb-hid", State: session.RequiresAttention, Message: "USB device-mode unavailable."},
			}
			c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})
			if _, err := c.ConfigureTransport(preferredTransport(mode), ""); err != nil {
				t.Fatal(err)
			}
			a := testApp(t, c)
			tt := ui.NewTester(a.view, 760, 1000)
			if err := tt.Click("Check readiness"); err != nil {
				t.Fatal(err)
			}
			stage := session.NeedsSetup
			if mode == "manual" {
				stage = session.Ready
			}
			uiStage(t, a, tt, stage)
			next, ok := tt.AccessByRole(ui.RoleButton, "Continue to route and setup")
			if !ok || next.Disabled != (mode != "manual") {
				t.Fatalf("%s gate: %+v", mode, next)
			}
			if tt.HasText("USB device-mode unavailable.") != (mode != "manual") {
				t.Fatal(tt.Texts())
			}
		})
	}
}

func TestSelectionHidesLaunchAndExplainsCivSwitchMethod(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})
	if _, err := c.ConfigureTransport(preferredTransport("keyboard"), ""); err != nil {
		t.Fatal(err)
	}
	a := testApp(t, c)
	tt := ui.NewTester(a.view, 760, 1400)
	if err := tt.Click("Check readiness"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	if err := tt.Click("Continue to route and setup"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Start"); ok {
		t.Fatal("launch control appears in selection")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Check live RPC status"); ok {
		t.Fatal("tools appear before verification")
	}
	if err := tt.Click("CivSwitch"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("CivSwitch requires composite keyboard + gamepad HID. The selected input method cannot provide both. Return to input and detection to choose another method.") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Back to input and detection"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Check readiness"); !ok {
		t.Fatal("no return to detection")
	}
}

func TestUSBDiscoveryAllowsSelectionButDoesNotEnableLaunch(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "usb-controllers", State: session.Available, GatesTransport: "usb-hid", Message: "Discovered, enumeration not verified."}}
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})
	c.ConfigureTransport(preferredTransport("keyboard"), "")
	a := testApp(t, c)
	tt := ui.NewTester(a.view, 760, 1000)
	clickManual(t, tt, "Check readiness")
	uiStage(t, a, tt, session.NeedsSetup)
	if disabled(t, tt, "Continue to route and setup") {
		t.Fatal("discovery must allow route selection without claiming enumeration")
	}
	if c.Snapshot().CanStart() {
		t.Fatal("discovery relaxed the controller gate")
	}
}

func TestInputMethodLabelTracksLocaleChange(t *testing.T) {
	withLocale(t, "en")
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 760, 1000)
	if !tt.HasText(locales.FlowManual()) {
		t.Fatal(tt.Texts())
	}
	withLocale(t, "zh-CN")
	tt.Frame()
	if !tt.HasText(locales.FlowManual()) || tt.HasText("Guided manual input") {
		t.Fatal(tt.Texts())
	}
}

func TestCompositeCivSwitchUsesSetupLaunchAndVerifiedToolsStages(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	env := f.Environment()
	env.CivSwitch = &civPanelAdapter{}
	c := session.New(env, session.Profile{ID: "test", Revision: "pinned"})
	if _, err := c.ConfigureTransport(preferredTransport("composite"), ""); err != nil {
		t.Fatal(err)
	}
	a := testApp(t, c)
	tt := ui.NewTester(a.view, 760, 1600)
	clickManual(t, tt, locales.CheckReadiness())
	uiStage(t, a, tt, session.Ready)
	clickManual(t, tt, locales.FlowNextSelection())
	clickManual(t, tt, locales.CivTitle())
	if _, ok := tt.AccessByRole(ui.RoleButton, locales.CivStart()); ok {
		t.Fatal("CivSwitch launch leaked into setup")
	}
	a.civSwitchPanel.prerequisites = session.CivSwitchPrerequisites{ConsoleIPv4: "192.0.2.2", PhysicalSwitch1Cartridge100: true, EnglishUS: true, SameReachableNetwork: true, LobbyChatFocused: true}
	tt.Frame()
	clickManual(t, tt, locales.FlowNextLaunch())
	if _, ok := tt.AccessByRole(ui.RoleTextField, locales.CivAddress()); ok {
		t.Fatal("CivSwitch setup leaked into launch")
	}
	clickManual(t, tt, locales.CivStart())
	uiStage(t, a, tt, session.Verified)
	if _, ok := tt.AccessByRole(ui.RoleButton, locales.CivControlStart()); !ok {
		t.Fatal("verified CivSwitch tools unavailable")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, locales.CivStart()); ok {
		t.Fatal("CivSwitch launch leaked into tools")
	}
	clickManual(t, tt, locales.CivFinish())
	uiStage(t, a, tt, session.Finished)
	clickManual(t, tt, locales.CivBack())
	if a.flow.stage != flowSelection {
		t.Fatal("return from CivSwitch bypassed route selection")
	}
}

// The route choice is one radio group: its selection is a state assistive
// technology reads, not only a fill, and the arrows move it.
func TestRouteChoiceIsAnAnnouncedRadioGroup(t *testing.T) {
	a := testApp(t, session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 760, 1000)
	a.command(a.controller.Inspect())
	uiStage(t, a, tt, session.Ready)
	a.flow.stage = flowSelection
	tt.Frame()
	chosen := func(label string) bool {
		t.Helper()
		info, ok := tt.AccessByRole(ui.RoleRadio, label)
		if !ok {
			t.Fatalf("route %q is not a radio button: %v", label, tt.Texts())
		}
		return info.Checked
	}
	if _, ok := tt.AccessByRole(ui.RoleRadioGroup, locales.FlowRoute()); !ok {
		t.Fatal("the route choice is not a named radio group")
	}
	if !chosen(locales.FlowSmile()) || chosen(locales.CivTitle()) {
		t.Fatal("the default route is not the one announced as chosen")
	}
	clickManual(t, tt, locales.CivTitle())
	tt.Frame()
	if !a.civSwitchPanel.open || !chosen(locales.CivTitle()) || chosen(locales.FlowSmile()) {
		t.Fatal("choosing CivSwitch did not move the selection")
	}
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if a.civSwitchPanel.open || !chosen(locales.FlowSmile()) {
		t.Fatal("the arrows do not move the route selection")
	}
}

// The reason Continue is disabled takes the footer's free width, so at a
// normal window size it reads as one line beside the button.
func TestFooterReasonUsesTheFreeWidth(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 960, 900)
	hint, ok := tt.Access(locales.FlowCheckFirst())
	if !ok {
		t.Fatal("the disabled Continue has no reason")
	}
	line, _ := tt.Access(locales.AppDescriptionShort())
	if hint.Bounds.H > 1.5*line.Bounds.H {
		t.Fatalf("the footer reason wraps in %v", hint.Bounds)
	}
}

// The tools stage opens with what the session is, then what can be done in it.
func TestToolsStageLeadsWithTheSessionState(t *testing.T) {
	a := testApp(t, session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test", Revision: "pinned", RetainsRPC: true, Tools: []string{"rpc-status"}}))
	tt := ui.NewTester(a.view, 960, 1400)
	a.command(a.controller.Inspect())
	uiStage(t, a, tt, session.Ready)
	a.command(a.controller.Start())
	uiStage(t, a, tt, session.RetainedRPC)
	state, ok := tt.Access(locales.RetainedSession())
	tools, found := tt.Access(locales.ToolsTitle())
	if !ok || !found {
		t.Fatal("session state or tools title missing")
	}
	if state.Bounds.Y > tools.Bounds.Y {
		t.Fatalf("session state %v comes after the tools %v", state.Bounds, tools.Bounds)
	}
	a.controller.Finish()
}

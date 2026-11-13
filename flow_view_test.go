package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/driver"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

// renderFlow writes the tester's frame when ANY2JB_FLOW_RENDER_DIR is set.
func renderFlow(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("ANY2JB_FLOW_RENDER_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, name+".png"))
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
			a.driverPanel.gate = blockedDriverGate(a.manifest)
			if err := a.openSession(sessiontest.NewFakeEnvironment().Environment(), false); err != nil {
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
				titles := flowTitles()
				if focusedName(tt) != locales.FlowPosition(fmt.Sprint(int(a.flow.stage)+1), titles[a.flow.stage]) {
					t.Fatalf("%s did not focus its stage heading: %s", name, focusedName(tt))
				}
				// The sidebar names every stage and marks the current one.
				for _, title := range titles {
					if !tt.HasText(title) {
						t.Fatalf("%s sidebar lacks %q", name, title)
					}
				}
				if !tt.HasText("●") {
					t.Fatalf("%s sidebar does not mark the current stage", name)
				}
				renderFlow(t, tt, language+"-"+name)
				for _, corner := range []string{locales.Diagnostics(), locales.Settings()} {
					clickManual(t, tt, corner)
					if !tt.HasText(corner) {
						t.Fatalf("%s corner did not open at %s", corner, name)
					}
					clickManual(t, tt, corner)
				}
			}
			uiStage(t, a, tt, session.Ready)
			check("1-detection", []string{locales.CheckAgain(), locales.FlowNextSelection()}, []string{locales.GuidedManualLaunch(), locales.PrepareBootstrap(), locales.ToolsStatus()})
			clickManual(t, tt, locales.FlowTypeYourself())
			uiStage(t, a, tt, session.Ready)
			check("2-prepare", []string{locales.FlowNextSelection()}, []string{locales.CheckAgain(), locales.FlowNextLaunch(), locales.PrepareBootstrap(), locales.GuidedManualLaunch(), locales.ToolsStatus()})
			if _, ok := tt.AccessByRole(ui.RoleRadio, locales.CivTitle()); !ok {
				t.Fatal("route choice missing from Prepare the Switch")
			}
			passPrepare(t, a, tt)
			check("3-selection", []string{locales.FlowNextLaunch()}, []string{locales.CheckAgain(), locales.PrepareBootstrap(), locales.GuidedManualLaunch(), locales.ToolsStatus()})
			if !tt.HasText(locales.FlowSaved()) {
				t.Fatal("matching saved bootstrap did not skip setup")
			}
			if _, ok := tt.AccessByRole(ui.RoleRadio, locales.CivTitle()); ok {
				t.Fatal("route choice leaked into route and setup")
			}
			clickManual(t, tt, locales.FlowNextLaunch())
			check("4-launch", []string{locales.GuidedManualLaunch()}, []string{locales.PrepareBootstrap(), locales.CheckAgain(), locales.ToolsStatus()})
			if _, ok := tt.AccessByRole(ui.RoleRadio, locales.CivTitle()); ok {
				t.Fatal("route choice leaked into launch")
			}
			clickManual(t, tt, locales.GuidedManualLaunch())
			uiStage(t, a, tt, session.RetainedRPC)
			check("5-tools", []string{locales.ToolsStatus(), locales.Finish()}, []string{locales.GuidedManualLaunch(), locales.PrepareBootstrap(), locales.CheckAgain()})
			clickManual(t, tt, locales.Finish())
			uiStage(t, a, tt, session.Finished)
			if _, ok := tt.AccessByRole(ui.RoleButton, locales.ToolsStatus()); ok {
				t.Fatal("tools survived Finish")
			}
		})
	}
}

func usbCheck(id string, state session.CheckState) session.Check {
	return session.Check{ID: id, State: state, GatesTransport: "usb-hid", Message: "fixture"}
}

// macFixtures are the first stage's typical observations, from a Mac that has
// done nothing yet to one whose driver runs.
func macFixtures() map[string][]session.Check {
	network := session.Check{ID: "network", State: session.CheckVerified, LANIPv4: "192.0.2.24", Message: "fixture"}
	base := []session.Check{network, usbCheck("usb-runtime", session.CheckVerified), usbCheck("driver-restart", session.CheckVerified)}
	with := func(checks ...session.Check) []session.Check { return append(slices.Clone(base), checks...) }
	controller := usbCheck("usb-drd0", session.Available)
	controller.Controller = "usb-drd0"
	return map[string][]session.Check{
		"sip-enabled": with(usbCheck("sip", session.RequiresAttention), usbCheck("driver-present", session.RequiresAttention),
			usbCheck("driver-loaded", session.RequiresAttention), usbCheck("usb-controllers", session.RequiresAttention)),
		"sip-disabled": with(usbCheck("sip", session.CheckVerified), usbCheck("driver-present", session.CheckVerified),
			usbCheck("driver-loaded", session.RequiresAttention), usbCheck("usb-controllers", session.RequiresAttention)),
		"ready": with(usbCheck("sip", session.CheckVerified), usbCheck("driver-present", session.CheckVerified),
			usbCheck("driver-loaded", session.CheckVerified), usbCheck("usb-controllers", session.Available), controller),
		"restart": {network, usbCheck("usb-runtime", session.CheckVerified), usbCheck("sip", session.CheckVerified),
			usbCheck("driver-present", session.CheckVerified), usbCheck("driver-loaded", session.RequiresAttention),
			usbCheck("driver-restart", session.RequiresAttention), usbCheck("usb-controllers", session.RequiresAttention)},
		"blocked": {network, usbCheck("usb-runtime", session.RequiresAttention), usbCheck("sip", session.RequiresAttention),
			usbCheck("driver-present", session.RequiresAttention), usbCheck("driver-loaded", session.RequiresAttention),
			usbCheck("driver-restart", session.CheckVerified), usbCheck("usb-controllers", session.RequiresAttention)},
		"unsupported": {network, usbCheck("usb-platform", session.RequiresAttention)},
	}
}

// TestWizardFirstStage drives the first stage from observed checks alone: the
// four rows, their states, which method Continue applies and when it may.
func TestWizardFirstStage(t *testing.T) {
	type row struct{ status, detail string }
	cases := []struct {
		name    string
		blocked bool
		rows    func() [4]row
		// mode is what Continue applies; "" keeps it disabled.
		mode string
	}{
		{"sip-enabled", false, func() [4]row {
			return [4]row{{locales.MacDone(), locales.MacNetworkDone("192.0.2.24")}, {locales.MacNeedsAction(), locales.MacAllowAction()},
				{locales.MacNeedsAction(), locales.MacInstallAction()}, {locales.MacWaiting(), locales.MacConnectWaiting()}}
		}, ""},
		{"sip-disabled", false, func() [4]row {
			return [4]row{{locales.MacDone(), locales.MacNetworkDone("192.0.2.24")}, {locales.MacInProgress(), locales.MacAllowProgress()},
				{locales.MacNeedsAction(), locales.MacInstallNotRunning()}, {locales.MacWaiting(), locales.MacConnectWaiting()}}
		}, ""},
		{"ready", false, func() [4]row {
			return [4]row{{locales.MacDone(), locales.MacNetworkDone("192.0.2.24")}, {locales.MacDone(), locales.MacAllowDone()},
				{locales.MacDone(), locales.MacInstallDone()}, {locales.MacFound(), locales.MacConnectFound()}}
		}, "keyboard"},
		{"restart", false, func() [4]row {
			return [4]row{{locales.MacDone(), locales.MacNetworkDone("192.0.2.24")}, {locales.MacInProgress(), locales.MacAllowProgress()},
				{locales.MacNeedsAction(), locales.MacRestart()}, {locales.MacWaiting(), locales.MacConnectWaiting()}}
		}, ""},
		{"blocked", true, func() [4]row {
			return [4]row{{locales.MacDone(), locales.MacNetworkDone("192.0.2.24")}, {locales.MacWaiting(), ""},
				{locales.MacNotAvailable(), locales.MacInstallBlocked()}, {locales.MacWaiting(), locales.MacConnectWaiting()}}
		}, ""},
		{"unsupported", false, func() [4]row {
			return [4]row{{locales.MacDone(), locales.MacNetworkDone("192.0.2.24")}, {locales.MacNotAvailable(), ""},
				{locales.MacNotAvailable(), ""}, {locales.MacNotAvailable(), locales.MacConnectUnsupported()}}
		}, ""},
	}
	for _, language := range []string{"en", "zh-CN"} {
		for _, tc := range cases {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				withLocale(t, language)
				f := sessiontest.NewFakeEnvironment()
				f.Checks = macFixtures()[tc.name]
				a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
				if err != nil {
					t.Fatal(err)
				}
				a.driverPanel.gate = driver.New(driver.Options{Manifest: a.manifest, DeveloperSourceRoot: t.TempDir()})
				if tc.blocked {
					a.driverPanel.gate = blockedDriverGate(a.manifest)
				}
				if err := a.openSession(f.Environment(), false); err != nil {
					t.Fatal(err)
				}
				tt := ui.NewTester(a.view, 720, 720)
				uiStage(t, a, tt, session.Ready)
				steps := a.macSteps(a.controller.Snapshot())
				for i, want := range tc.rows() {
					if steps[i].status != want.status || steps[i].detail != want.detail {
						t.Errorf("row %d = %q / %q, want %q / %q", i, steps[i].status, steps[i].detail, want.status, want.detail)
					}
					if want.detail != "" && !tt.HasText(want.detail) {
						t.Errorf("row %d sentence is not shown: %q", i, want.detail)
					}
				}
				// Startup security is part of the one "allow" step, never a row of its own.
				for _, gone := range []string{locales.StartupSecurityTitle(), locales.StartupSecurityUnverified()} {
					if tt.HasText(gone) {
						t.Fatalf("separate startup-security row is back: %q", gone)
					}
				}
				if _, ok := tt.AccessByRole(ui.RoleButton, locales.MacInstallButton()); ok != (steps[2].action == installDriver) {
					t.Fatalf("install action shown = %v for %+v", ok, steps[2])
				}
				for _, w := range []int{720, 560} {
					tt.SetSize(w, 720)
					tt.Frame()
					renderFlow(t, tt, fmt.Sprintf("%s-mac-%s-%d", language, tc.name, w))
				}
				if steps[1].action == showRecoverySteps {
					// A step that needs action shows its steps and guides at once.
					if _, shown := tt.AccessByRole(ui.RoleLink, locales.MacGuideStartupSecurity()); shown != (steps[1].tone == toneAttention) {
						t.Fatalf("guides shown = %v for %+v", shown, steps[1])
					}
					if steps[1].tone != toneAttention {
						clickManual(t, tt, locales.MacAllowShowSteps())
						tt.Advance(time.Second)
					}
					for _, text := range []string{locales.MacRecovery1(), locales.MacRecovery4(), locales.MacRecovery5(), locales.MacRecoveryNote()} {
						if !tt.HasText(text) {
							t.Fatalf("the Recovery steps lack %q", text)
						}
					}
					tt.SetSize(560, 720)
					tt.Frame()
					renderFlow(t, tt, fmt.Sprintf("%s-mac-%s-steps-560", language, tc.name))
					tt.SetSize(720, 720)
					tt.Frame()
					checkAppleGuides(t, tt, language)
				}
				next, ok := tt.AccessByRole(ui.RoleButton, locales.FlowNextSelection())
				if !ok || next.Disabled != (tc.mode == "") {
					t.Fatalf("Continue for %q: %+v", tc.mode, next)
				}
				// Manual input is never chosen for the user; it stays a quiet link.
				if _, ok := tt.AccessByRole(ui.RoleLink, locales.FlowTypeYourself()); !ok {
					t.Fatal("the manual alternative is unreachable")
				}
				if _, ok := tt.AccessByRole(ui.RoleButton, locales.FlowTypeYourself()); ok {
					t.Fatal("the manual alternative looks like a button")
				}
				if tc.mode == "" && !tt.HasText(map[bool]string{true: locales.FlowUsbUnavailable(), false: locales.FlowDetectionBlocked()}[tc.blocked || tc.name == "unsupported"]) {
					t.Fatalf("disabled Continue gives no reason: %q", tt.Texts())
				}
				tt.SetSize(720, 1600)
				tt.Frame()
				label, mode := locales.FlowNextSelection(), tc.mode
				if mode == "" {
					label, mode = locales.FlowTypeYourself(), "manual"
				}
				clickManual(t, tt, label)
				s := a.controller.Snapshot()
				if a.flow.stage != flowPrepare || a.preferences.Transport != mode || s.Transport.Mode != mode {
					t.Fatalf("%q went to stage %d with %q / %q", label, a.flow.stage, a.preferences.Transport, s.Transport.Mode)
				}
				waitIdle(t, a, tt)
				if tc.name == "ready" && a.controller.Snapshot().CanStart() {
					t.Fatal("a discovered, unverified USB connection enabled launch")
				}
			})
		}
	}
}

// checkAppleGuides opens each Apple guide and checks the page is the one for
// the app's language, where Apple publishes one.
func checkAppleGuides(t *testing.T, tt *ui.Tester, language string) {
	t.Helper()
	support := "https://support.apple.com/"
	if language == "zh-CN" {
		support += "zh-cn/"
	}
	for label, want := range map[string]string{
		locales.MacGuideStartupSecurity(): support + "guide/mac-help/mchl768f7291/mac",
		locales.MacGuideRecovery():        support + "guide/mac-help/mchl82829c17/mac",
		locales.MacGuideSip():             "https://developer.apple.com/documentation/security/disabling-and-enabling-system-integrity-protection",
	} {
		clickManual(t, tt, label)
		opened := tt.OpenedURLs()
		if len(opened) == 0 || opened[len(opened)-1] != want {
			t.Fatalf("%q opened %q, want %q", label, opened, want)
		}
	}
}

func waitIdle(t *testing.T, a *app, tt *ui.Tester) {
	t.Helper()
	deadline := time.After(time.Second)
	for a.controller.Snapshot().Busy {
		select {
		case <-deadline:
			t.Fatal("the controller stayed busy")
		case <-time.After(time.Millisecond):
		}
	}
	tt.Frame()
}

func TestWizardChecksThisMacOnceWhenTheSessionOpens(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		f := sessiontest.NewFakeEnvironment()
		a := testApp(t, nil)
		if err := a.openSession(f.Environment(), interrupted); err != nil {
			t.Fatal(err)
		}
		tt := ui.NewTester(a.view, 720, 720)
		want, inspections := session.Ready, 1
		if interrupted {
			want, inspections = session.Indeterminate, 0
		}
		uiStage(t, a, tt, want)
		for range 5 {
			tt.Frame()
		}
		got := 0
		for _, action := range f.Trace() {
			if action == "platform.inspect" {
				got++
			}
		}
		if got != inspections {
			t.Fatalf("interrupted=%v: %d inspections, want %d", interrupted, got, inspections)
		}
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
	for _, label := range []string{"Diagnostics", "Settings", "Continue", "Check again"} {
		if _, ok := tt.AccessByRole(ui.RoleButton, label); !ok {
			t.Errorf("missing %q", label)
		}
	}
	for _, role := range []ui.Role{ui.RoleTextField, ui.RolePopUpButton} {
		for _, info := range tt.AccessAll() {
			if info.Role == role {
				t.Errorf("the first stage asks for input: %v %q", role, info.Name)
			}
		}
	}
}

func TestUnreportedRouteChecksDoNotEnableContinueOrClaimReadiness(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "network", State: session.CheckVerified}}
	a := testApp(t, session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 760, 1400)
	clickManual(t, tt, locales.CheckAgain())
	uiStage(t, a, tt, session.Ready)
	clickManual(t, tt, locales.FlowTypeYourself())
	uiStage(t, a, tt, session.Ready)
	passPrepare(t, a, tt)
	if !disabled(t, tt, locales.FlowNextLaunch()) {
		t.Fatal("unreported route checks enabled Continue")
	}
	if tt.HasText(locales.ReadyToLaunch()) || !tt.HasText(locales.RouteUnchecked()) {
		t.Fatal(tt.Texts())
	}
}

func TestSelectionHidesLaunchAndExplainsCivSwitchMethod(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	c := session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})
	a := testApp(t, c)
	tt := ui.NewTester(a.view, 760, 1400)
	clickManual(t, tt, locales.CheckAgain())
	uiStage(t, a, tt, session.Ready)
	clickManual(t, tt, locales.FlowTypeYourself())
	uiStage(t, a, tt, session.Ready)
	passPrepare(t, a, tt)
	if _, ok := tt.AccessByRole(ui.RoleButton, "Start"); ok {
		t.Fatal("launch control appears in selection")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Check live RPC status"); ok {
		t.Fatal("tools appear before verification")
	}
	clickManual(t, tt, locales.FlowBackDetection())
	if a.flow.stage != flowPrepare {
		t.Fatal("route and setup does not go back to Prepare the Switch")
	}
	// The method gate is stated where the route is chosen, before any ticking.
	clickManual(t, tt, locales.CivTitle())
	if !tt.HasText(locales.FlowCivUnavailable()) || !tt.HasText(locales.PrepCivMethod()) || !disabled(t, tt, locales.FlowNextSelection()) {
		t.Fatal(tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleCheckBox, locales.CivCartridge()); ok {
		t.Fatal("a checklist is offered for a route this method cannot use")
	}
	clickManual(t, tt, locales.FlowBackDetection())
	if _, ok := tt.AccessByRole(ui.RoleButton, locales.CheckAgain()); !ok {
		t.Fatal("no return to detection")
	}
}

func TestFirstStageTracksLocaleChange(t *testing.T) {
	withLocale(t, "en")
	a := newTestApp(t)
	tt := ui.NewTester(a.view, 760, 1000)
	if !tt.HasText(locales.MacAllow()) {
		t.Fatal(tt.Texts())
	}
	withLocale(t, "zh-CN")
	tt.Frame()
	if !tt.HasText(locales.MacAllow()) || tt.HasText("Allow the USB driver") {
		t.Fatal(tt.Texts())
	}
}

func TestCompositeCivSwitchUsesSetupLaunchAndVerifiedToolsStages(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	// Composite input runs on the same USB driver as the plain keyboard.
	f.Checks = append(f.Checks, usbCheck("driver-loaded", session.CheckVerified))
	env := f.Environment()
	env.CivSwitch = &civPanelAdapter{}
	c := session.New(env, session.Profile{ID: "test", Revision: "pinned"})
	if _, err := c.ConfigureTransport(preferredTransport("composite"), ""); err != nil {
		t.Fatal(err)
	}
	a := testApp(t, c)
	tt := ui.NewTester(a.view, 760, 1600)
	clickManual(t, tt, locales.CheckAgain())
	uiStage(t, a, tt, session.Ready)
	clickManual(t, tt, locales.FlowNextSelection())
	uiStage(t, a, tt, session.Ready)
	if a.controller.Snapshot().Transport.Mode != "composite" {
		t.Fatal("Continue dropped the composite method chosen in Settings")
	}
	clickManual(t, tt, locales.CivTitle())
	passPrepare(t, a, tt)
	if _, ok := tt.AccessByRole(ui.RoleButton, locales.CivStart()); ok {
		t.Fatal("CivSwitch launch leaked into setup")
	}
	a.civSwitchPanel.prerequisites.ConsoleIPv4 = "192.0.2.2"
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
	if a.flow.stage != flowPrepare || a.civSwitchPanel.open {
		t.Fatal("return from CivSwitch bypassed the SmileBASIC checklist")
	}
}

// The route choice is one radio group: its selection is a state assistive
// technology reads, not only a fill, and the arrows move it.
func TestRouteChoiceIsAnAnnouncedRadioGroup(t *testing.T) {
	a := testApp(t, session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 760, 1000)
	a.command(a.controller.Inspect())
	uiStage(t, a, tt, session.Ready)
	a.flow.stage = flowPrepare
	tt.Frame()
	chosen := func(label string) bool {
		t.Helper()
		info, ok := tt.AccessByRole(ui.RoleRadio, label)
		if !ok {
			t.Fatalf("route %q is not a radio button: %v", label, tt.Texts())
		}
		return info.Checked
	}
	if _, ok := tt.AccessByRole(ui.RoleRadioGroup, locales.PrepRoute()); !ok {
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

func TestLinuxFirstStageHandlesNoUDCAndUDCHosts(t *testing.T) {
	t.Run("host-only Linux PC without UDC offers guided manual input", func(t *testing.T) {
		f := sessiontest.NewFakeEnvironment()
		f.Checks = []session.Check{
			{ID: "network", State: session.CheckVerified, LANIPv4: "192.168.1.40"},
			{ID: "runtime", State: session.CheckVerified},
			{ID: "engine", State: session.CheckVerified},
			{ID: "input-adapter", State: session.CheckVerified},
			usbCheck("usb-platform", session.CheckVerified),
			usbCheck("linux-udc", session.RequiresAttention),
			usbCheck("linux-configfs", session.RequiresAttention),
			usbCheck("linux-gadget-access", session.RequiresAttention),
			usbCheck("usb-controllers", session.RequiresAttention),
			usbCheck("usb-runtime", session.CheckVerified),
		}
		a := testApp(t, session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"}))
		tt := ui.NewTester(a.view, 900, 1100)
		clickManual(t, tt, locales.CheckAgain())
		uiStage(t, a, tt, session.Ready)
		if !tt.HasText(locales.FlowDetectionLinux()) || !tt.HasText(locales.LinuxUDCNone()) || !tt.HasText(locales.FlowUsbUnavailable()) {
			t.Fatalf("unexpected texts for no-UDC Linux host: %v", tt.Texts())
		}
		clickManual(t, tt, locales.FlowTypeYourself())
		uiStage(t, a, tt, session.Ready)
		if a.flow.stage != flowPrepare {
			t.Fatalf("guided manual did not advance from Linux Stage 1: stage=%v", a.flow.stage)
		}
	})

	t.Run("UDC-capable Linux host advances with ConfigFS keyboard", func(t *testing.T) {
		f := sessiontest.NewFakeEnvironment()
		udc := usbCheck("fe980000.usb", session.Available)
		udc.Controller = "fe980000.usb"
		f.Checks = []session.Check{
			{ID: "network", State: session.CheckVerified, LANIPv4: "192.168.1.41"},
			{ID: "runtime", State: session.CheckVerified},
			{ID: "engine", State: session.CheckVerified},
			{ID: "input-adapter", State: session.CheckVerified},
			usbCheck("usb-platform", session.CheckVerified),
			usbCheck("linux-udc", session.CheckVerified),
			usbCheck("linux-configfs", session.CheckVerified),
			usbCheck("linux-gadget-access", session.CheckVerified),
			usbCheck("usb-controllers", session.Available),
			udc,
			usbCheck("usb-runtime", session.CheckVerified),
		}
		a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := a.openSession(f.Environment(), false); err != nil {
			t.Fatal(err)
		}
		tt := ui.NewTester(a.view, 900, 1100)
		uiStage(t, a, tt, session.Ready)
		if !tt.HasText(locales.LinuxConfigFSDone()) || disabled(t, tt, locales.FlowNextSelection()) {
			t.Fatalf("UDC-capable Linux host did not unlock Continue: %v", tt.Texts())
		}
		clickManual(t, tt, locales.FlowNextSelection())
		if a.flow.stage != flowPrepare || a.preferences.Transport != "keyboard" {
			t.Fatalf("Continue did not select keyboard and advance: stage=%v transport=%q", a.flow.stage, a.preferences.Transport)
		}
	})
}

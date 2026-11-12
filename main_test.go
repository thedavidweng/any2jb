package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/driver"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestSessionFailureHasSafeRecovery(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.PrepareError = errors.New("simulated failure")
	a := testApp(t, session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 640, 900)
	if err := tt.Click("Check again"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	advanceToLaunch(t, a, tt)
	advanceToLaunch(t, a, tt)
	if err := tt.Click("Guided manual launch"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Failed)
	if !tt.HasText("Session failed") || !tt.HasText("The operation failed before console input. Check diagnostics, then run readiness checks.") {
		t.Fatal(tt.Texts())
	}
}

func TestShell(t *testing.T) {
	a := testApp(t, nil)
	tt := ui.NewTester(a.view, 640, 400)
	for _, text := range []string{"any2jb", "No console session is running."} {
		if !tt.HasText(text) {
			t.Errorf("missing %q in shell: %q", text, tt.Texts())
		}
	}
	clickManual(t, tt, locales.Settings())
	if err := tt.Click("About this build"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Development shell. Console workflows are not available yet.") {
		t.Errorf("missing build limitations: %q", tt.Texts())
	}
}

func TestStartupFailureIsReportedInTheWindow(t *testing.T) {
	a := testApp(t, nil)
	a.startupError = "developer USB helper staging: fixture"
	tt := ui.NewTester(a.view, 640, 400)
	if _, ok := tt.AccessByRole(ui.RoleStatus, a.startupError); !ok {
		t.Fatalf("the startup failure is not an accessible status: %q", tt.Texts())
	}
}

// testManifest loads the bundled manifest every production app shell has, so a
// view test can build the same app main builds.
func testManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// testApp builds the shell main builds: the bundled manifest, a preferences
// file of its own, and preferences that already match the controller, so the
// view does not wait for a preference change. A nil controller is the
// pre-session shell.
func testApp(t *testing.T, c *session.SessionController) *app {
	t.Helper()
	m := testManifest(t)
	prefs := preferences{Profile: m.Default, Edition: "full", Transport: "manual"}
	if c != nil {
		s := c.Snapshot()
		prefs = preferences{Profile: s.Profile.ID, Edition: s.Edition,
			Transport: s.Transport.Mode, LANIPv4: s.LANOverride}
	}
	return &app{manifest: m, controller: c, driverPanel: driverPanel{gate: blockedDriverGate(m)},
		preferencesPath: filepath.Join(t.TempDir(), "preferences.json"), preferences: prefs}
}

// blockedDriverGate is the packaged build's gate: the driver components are
// not cleared for redistribution.
func blockedDriverGate(m *manifest.Manifest) *driver.Gate {
	return driver.New(driver.Options{Manifest: m})
}

func uiStage(t *testing.T, a *app, tt *ui.Tester, stage session.Stage) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		s := a.controller.Snapshot()
		if s.Stage == stage && !s.Busy {
			tt.Frame()
			return
		}
		select {
		case <-deadline:
			t.Fatalf("wanted %s, got %+v", stage, s)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestSessionDashboardAndRecovery(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.InputGate = make(chan struct{})
	a := testApp(t, session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 640, 900)
	if !tt.HasText(locales.FlowCheckFirst()) {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Check again"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	advanceToLaunch(t, a, tt)
	if err := tt.Click("Guided manual launch"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for a.controller.Snapshot().Stage != session.WaitingForConsole {
		select {
		case <-deadline:
			t.Fatal("input did not start")
		case <-time.After(time.Millisecond):
		}
	}
	tt.Frame()
	if !tt.HasText("Session in progress") {
		t.Fatal(tt.Texts())
	}
	cancel, ok := tt.AccessByRole(ui.RoleButton, "Cancel")
	if !ok || !cancel.Disabled {
		t.Fatal("Cancel missing or enabled after input")
	}
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Indeterminate)
	if !tt.HasText("Restart the console before another attempt.") {
		t.Fatal(tt.Texts())
	}
}

func TestFailingReadinessIsActionable(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.Checks = []session.Check{{ID: "network", State: session.RequiresAttention, Message: "No reachable LAN address.", NextAction: "Connect the host to the console LAN."}}
	a := testApp(t, session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 640, 900)
	if err := tt.Click("Check again"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.NeedsSetup)
	for _, text := range []string{locales.MacNeedsAction(), locales.MacNetworkAction(), locales.FlowDetectionBlocked()} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q: %q", text, tt.Texts())
		}
	}
	// No input method helps without the network, so neither way on is open.
	for _, label := range []string{locales.FlowNextSelection(), locales.FlowTypeYourself()} {
		if control, ok := tt.Access(label); !ok || !control.Disabled {
			t.Fatalf("%q enabled with failing readiness", label)
		}
	}
}

func TestDashboardCancelAndFinish(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	gate := make(chan struct{})
	f.PrepareGate = gate
	a := testApp(t, session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.view, 640, 900)
	if err := tt.Click("Check again"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	advanceToLaunch(t, a, tt)
	if err := tt.Click("Guided manual launch"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	close(gate)
	advanceToLaunch(t, a, tt)
	if err := tt.Click("Guided manual launch"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Verified)
	if !tt.HasText("Verified session") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Finish"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Finished)
	if !tt.HasText("Session finished") {
		t.Fatal(tt.Texts())
	}
}

func advanceToLaunch(t *testing.T, a *app, tt *ui.Tester) {
	t.Helper()
	if a.flow.stage == flowDetection {
		clickManual(t, tt, locales.FlowTypeYourself())
		waitIdle(t, a, tt)
	}
	if a.flow.stage == flowSelection {
		clickManual(t, tt, locales.FlowNextLaunch())
	}
}

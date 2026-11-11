package main

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestSessionFailureHasSafeRecovery(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	f.PrepareError = errors.New("simulated failure")
	a := &app{controller: session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})}
	tt := ui.NewTester(a.view, 640, 520)
	if err := tt.Click("Check readiness"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	if err := tt.Click("Guided manual launch"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Failed)
	if !tt.HasText("Session failed") || !tt.HasText("The operation failed before console input. Check diagnostics, then run readiness checks.") {
		t.Fatal(tt.Texts())
	}
}

func TestShell(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(a.view, 640, 400)
	for _, text := range []string{"any2jb", "No console session is running."} {
		if !tt.HasText(text) {
			t.Errorf("missing %q in shell: %q", text, tt.Texts())
		}
	}
	if err := tt.Click("About this build"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Development shell. Console workflows are not available yet.") {
		t.Errorf("missing build limitations: %q", tt.Texts())
	}
}

func TestStartupFailureIsReportedInTheWindow(t *testing.T) {
	a := &app{startupError: "developer USB helper staging: fixture"}
	tt := ui.NewTester(a.view, 640, 400)
	if _, ok := tt.AccessByRole(ui.RoleStatus, a.startupError); !ok {
		t.Fatalf("the startup failure is not an accessible status: %q", tt.Texts())
	}
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
	a := &app{controller: session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})}
	tt := ui.NewTester(a.view, 640, 520)
	if !tt.HasText("Home") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Check readiness"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	if !tt.HasText("Simulated readiness, not a real hardware check.") {
		t.Fatal(tt.Texts())
	}
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
	a := &app{controller: session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})}
	tt := ui.NewTester(a.view, 640, 520)
	if err := tt.Click("Check readiness"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.NeedsSetup)
	for _, text := range []string{"Setup needs attention", "No reachable LAN address.", "Connect the host to the console LAN."} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q: %q", text, tt.Texts())
		}
	}
	start, ok := tt.AccessByRole(ui.RoleButton, "Guided manual launch")
	if !ok || !start.Disabled {
		t.Fatal("Start missing or enabled with failing readiness")
	}
}

func TestDashboardCancelAndFinish(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	gate := make(chan struct{})
	f.PrepareGate = gate
	a := &app{controller: session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned"})}
	tt := ui.NewTester(a.view, 640, 520)
	if err := tt.Click("Check readiness"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	if err := tt.Click("Guided manual launch"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	close(gate)
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

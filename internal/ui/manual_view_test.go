package ui

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/internal/transport"
)

func manualApp(t *testing.T, scenario string) (*app, *ui.Tester) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the structured host fixture")
	}
	fixture := filepath.Join(repoRootDir(t), "internal", "engine", "testdata", "fake-engine.cjs")
	f := sessiontest.NewFakeEnvironment()
	env := f.Environment()
	env.Host = engine.NewProtocolRunner(t.TempDir(), func(run session.Run, workspace string) *exec.Cmd {
		return exec.Command(node, fixture, scenario, run.ID, workspace)
	})
	manual := transport.NewManual(func(session.Run) (string, error) { return "PRINT 1\n", nil })
	env.Input = manual
	profile := session.Profile{ID: "fixture", Revision: "pinned", SaveName: "NS2",
		BootstrapSHA256: "fixture", StartPort: 39380, StartArgumentCount: 5}
	c := session.New(env, profile)
	t.Cleanup(func() {
		s := c.Snapshot()
		env.Host.Cleanup(context.Background(), session.Run{ID: s.RunID})
	})
	c.ConfigureTransport(appstate.PreferredTransport("manual"), "192.0.2.1")
	a := testApp(t, c)
	a.Manual = manual
	return a, ui.NewTester(a.view, 640, 900)
}

func waitManualUI(t *testing.T, a *app, tt *ui.Tester, predicate func(session.Snapshot, transport.Prompt) bool) {
	t.Helper()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		if predicate(a.Controller.Snapshot(), a.Manual.Prompt()) {
			tt.Frame()
			return
		}
		select {
		case <-a.Controller.Changes():
		case <-a.Manual.Changes():
		case <-timer.C:
			t.Fatalf("UI did not reach expected state: %+v", a.Controller.Snapshot())
		}
	}
}

func clickManual(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err, tt.Texts())
	}
}

func saveManualBootstrap(t *testing.T, a *app, tt *ui.Tester) {
	t.Helper()
	clickManual(t, tt, "Check again")
	waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return s.Stage == session.Ready && !s.Busy })
	clickManual(t, tt, "Type the commands yourself instead")
	waitIdle(t, a, tt)
	passPrepare(t, a, tt)
	clickManual(t, tt, "Prepare bootstrap")
	for _, kind := range []string{"slot", "bootstrap", "direct"} {
		waitManualUI(t, a, tt, func(_ session.Snapshot, p transport.Prompt) bool { return p.Kind == kind })
		clickManual(t, tt, "I completed this console step")
	}
	waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return s.BootstrapPrepared && !s.Busy })
	clickManual(t, tt, "I authorize replacing this saved program if it already exists")
	clickManual(t, tt, "Submit SAVE")
	waitManualUI(t, a, tt, func(_ session.Snapshot, p transport.Prompt) bool { return p.Kind == "save" })
	clickManual(t, tt, "I completed this console step")
	waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return s.SaveSubmitted && !s.Busy })
	if a.Controller.Snapshot().BootstrapSaved {
		t.Fatal("SAVE submission was mistaken for confirmation")
	}
	clickManual(t, tt, "I saved the bootstrap")
	waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return s.Stage == session.Ready && s.BootstrapSaved })
	clickManual(t, tt, "Continue to launch")
}

func TestGuidedManualUISetupLaunchAndStructuredVerification(t *testing.T) {
	for _, scenario := range []string{"verified", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			a, tt := manualApp(t, scenario)
			saveManualBootstrap(t, a, tt)
			clickManual(t, tt, "Guided manual launch")
			for _, kind := range []string{"load", "load-confirm", "run", "remove", "start"} {
				waitManualUI(t, a, tt, func(_ session.Snapshot, p transport.Prompt) bool { return p.Kind == kind })
				if a.Controller.Snapshot().Outcome.Verified {
					t.Fatal("input was mistaken for verification")
				}
				if kind == "start" {
					if a.Manual.Prompt().Text != "V=START(39380,192,0,2,1)" {
						t.Fatal("wrong session command")
					}
					clickManual(t, tt, "Copy console text")
					if tt.Clipboard() != "V=START(39380,192,0,2,1)" {
						t.Fatal("copy changed the exact session command")
					}
					for _, event := range a.Controller.Events(0) {
						if strings.Contains(event.Message, "V=START") {
							t.Fatal("pairing leaked into public events")
						}
					}
				}
				clickManual(t, tt, "I completed this console step")
			}
			stage := session.Verified
			if scenario == "partial" {
				stage = session.Indeterminate
			}
			waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return s.Stage == stage && !s.Busy })
			if scenario == "verified" {
				clickManual(t, tt, "Finish")
				waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return s.Stage == session.Finished && !s.Busy })
				if !a.Controller.Snapshot().CleanupConfirmed || len(a.Controller.Snapshot().Processes) != 0 {
					t.Fatal("Finish lacks cleanup evidence")
				}
			} else if tt.HasText("Verified session") {
				t.Fatal("partial 4/4 result shown as verified")
			}
		})
	}
}

func TestGuidedManualUICancelBeforeHostPublication(t *testing.T) {
	a, tt := manualApp(t, "waiting")
	saveManualBootstrap(t, a, tt)
	clickManual(t, tt, "Guided manual launch")
	waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return len(s.Processes) > 0 })
	if a.Manual.Prompt().Kind != "" {
		t.Fatal("manual command shown before the host listened")
	}
	clickManual(t, tt, "Cancel")
	waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return s.Stage == session.Ready && !s.Busy })
	if s := a.Controller.Snapshot(); !s.CleanupConfirmed || s.SideEffects || len(s.Processes) != 0 {
		t.Fatal("Cancel did not confirm pre-input cleanup")
	}
}

func TestGuidedManualUIStopClearsPromptAndRequiresRestart(t *testing.T) {
	a, tt := manualApp(t, "ready-wait")
	saveManualBootstrap(t, a, tt)
	clickManual(t, tt, "Guided manual launch")
	waitManualUI(t, a, tt, func(_ session.Snapshot, p transport.Prompt) bool { return p.Kind == "load" })
	clickManual(t, tt, "Stop")
	waitManualUI(t, a, tt, func(s session.Snapshot, _ transport.Prompt) bool { return s.Stage == session.Indeterminate && !s.Busy })
	s := a.Controller.Snapshot()
	if a.Manual.Prompt().Kind != "" || !s.CleanupConfirmed || len(s.Processes) != 0 {
		t.Fatal("Stop did not release prompt and owned process")
	}
	if !tt.HasText("Restart the console before another attempt.") {
		t.Fatal(tt.Texts())
	}
}

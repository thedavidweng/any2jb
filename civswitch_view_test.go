package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestCivSwitchPanelStatesUnavailableReasonWithoutSmileBASICControls(t *testing.T) {
	a := testApp(t, session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.civSwitchView, 900, 1800)
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

// civPanelAdapter is the route adapter plus its gated persistence surface.
type civPanelAdapter struct {
	installs, sends, stages, centers atomic.Int32
}

func (a *civPanelAdapter) Prepare(context.Context) (time.Duration, error) { return 0, nil }
func (a *civPanelAdapter) Transfer(context.Context, session.Report) error { return nil }
func (a *civPanelAdapter) Probe(context.Context, string, bool) error      { return nil }
func (a *civPanelAdapter) Release(context.Context) error                  { return nil }

func (a *civPanelAdapter) InstallBootTrigger(ctx context.Context, address string, confirmed bool, report session.Report) (string, error) {
	if !confirmed {
		return "", errors.New("unconfirmed save replacement reached the adapter")
	}
	a.installs.Add(1)
	report(session.Progress{SideEffect: true})
	return "/retained/backups/one", nil
}

func (a *civPanelAdapter) SendJavaScript(ctx context.Context, address, file string, confirmed bool, report session.Report) error {
	if !confirmed {
		return errors.New("unconfirmed code execution reached the adapter")
	}
	a.sends.Add(1)
	report(session.Progress{SideEffect: true})
	return nil
}

func (a *civPanelAdapter) StageJavaScript(ctx context.Context, address, file string, confirmed bool, report session.Report) error {
	if !confirmed {
		return errors.New("unconfirmed staging reached the adapter")
	}
	a.stages.Add(1)
	return nil
}

func (a *civPanelAdapter) ControlCenter(ctx context.Context, address string, observe session.CivSwitchObserver) error {
	a.centers.Add(1)
	observe(session.CivSwitchObservation{Ready: true})
	observe(session.CivSwitchObservation{ExportPath: "/retained/save-exports/one"})
	<-ctx.Done()
	return ctx.Err()
}

func verifiedCivSwitchPanel(t *testing.T, w *civPanelAdapter) (*app, *ui.Tester) {
	t.Helper()
	env := sessiontest.NewFakeEnvironment().Environment()
	env.CivSwitch = w
	a := testApp(t, session.New(env, session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.civSwitchView, 1000, 3200)
	a.civSwitchPanel.prerequisites.ConsoleIPv4 = "192.0.2.2"
	tt.Frame()
	if err := tt.Click(locales.CivAttach()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Verified)
	return a, tt
}

func disabled(t *testing.T, tt *ui.Tester, label string) bool {
	t.Helper()
	control, ok := tt.AccessByRole(ui.RoleButton, label)
	if !ok {
		t.Fatalf("missing control %q: %v", label, tt.Texts())
	}
	return control.Disabled
}

func TestCivSwitchPanelGatesPersistenceAndCodeOnExplicitConsent(t *testing.T) {
	w := &civPanelAdapter{}
	a, tt := verifiedCivSwitchPanel(t, w)
	for _, text := range []string{locales.CivTriggerWarning(), locales.CivCodeWarning(), locales.CivControlNote(), locales.CivDependentNote()} {
		if !tt.HasText(text) {
			t.Fatalf("missing risk disclosure %q: %v", text, tt.Texts())
		}
	}
	for _, label := range []string{locales.CivTrigger(), locales.CivJSSend(), locales.CivControlStage()} {
		if !disabled(t, tt, label) {
			t.Fatalf("%q is usable without explicit confirmation", label)
		}
	}
	if disabled(t, tt, locales.CivControlStart()) {
		t.Fatal("the read-only Control Center requires code consent")
	}
	if err := tt.Click(locales.CivTriggerConsent()); err != nil {
		t.Fatal(err)
	}
	if disabled(t, tt, locales.CivTrigger()) {
		t.Fatal("the confirmed trigger install stayed unavailable")
	}
	if err := tt.Click(locales.CivTrigger()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Verified)
	if w.installs.Load() != 1 {
		t.Fatalf("installs: %d", w.installs.Load())
	}
	if !tt.HasText(locales.CivTriggerInstalled()) || !tt.HasText(locales.CivBackupSaved("/retained/backups/one")) {
		t.Fatalf("install result: %v", tt.Texts())
	}
	if err := tt.Click(locales.CivCodeConsent()); err != nil {
		t.Fatal(err)
	}
	if !disabled(t, tt, locales.CivJSSend()) {
		t.Fatal("consent alone offered to send a file the user never chose")
	}
	a.civSwitchPanel.javaScript = "/tmp/payload.js"
	tt.Frame()
	if err := tt.Click(locales.CivJSSend()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Verified)
	if w.sends.Load() != 1 || w.stages.Load() != 0 {
		t.Fatalf("after a confirmed send: %d sends, %d stagings", w.sends.Load(), w.stages.Load())
	}
}

func TestCivSwitchPanelRunsAndStopsTheReadOnlyControlCenter(t *testing.T) {
	w := &civPanelAdapter{}
	a, tt := verifiedCivSwitchPanel(t, w)
	if err := tt.Click(locales.CivControlStart()); err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); !a.controller.Snapshot().CivSwitchActions.ControlCenterLive; {
		if time.Since(start) > time.Second {
			t.Fatal("the Control Center session never became live")
		}
		time.Sleep(time.Millisecond)
	}
	tt.Frame()
	if !tt.HasText(locales.CivControlLive()) || !tt.HasText(locales.CivExportSaved("/retained/save-exports/one")) {
		t.Fatalf("live Control Center: %v", tt.Texts())
	}
	if err := tt.Click(locales.CivControlStop()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Verified)
	s := a.controller.Snapshot()
	if s.CivSwitchActions.ControlCenterLive || s.SideEffects || !s.LoaderVerified {
		t.Fatalf("after stopping the read-only session: %+v", s)
	}
	if w.centers.Load() != 1 {
		t.Fatalf("Control Center sessions: %d", w.centers.Load())
	}
}

package ui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/locales"
)

func TestCivSwitchPanelStatesUnavailableReasonWithoutSmileBASICControls(t *testing.T) {
	a := testApp(t, session.New(sessiontest.NewFakeEnvironment().Environment(), session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.civSwitchView, 900, 1800)
	for _, text := range []string{locales.CivPrerequisites(), locales.CivNoRPC(), locales.CivComposite(), appstate.CivSwitchUnavailableLocal(a.Controller)} {
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

type civPanelAdapter struct {
	installs, sends, fsHosts, hbProbes, hbLaunches atomic.Int32
}

func (a *civPanelAdapter) Availability() error                            { return nil }
func (a *civPanelAdapter) Writer() session.CivSwitchWriter                { return a }
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

func (a *civPanelAdapter) StartCivSwitchFSHost(ctx context.Context, request session.CivSwitchFSRequest, event func(session.CivSwitchFSEvent)) error {
	a.fsHosts.Add(1)
	event(session.CivSwitchFSEvent{Type: "ready", RunPath: "/retained/fs-workspace"})
	<-ctx.Done()
	return ctx.Err()
}

func (a *civPanelAdapter) CivHomebrewProbe(context.Context, string) (session.CivHomebrewState, error) {
	a.hbProbes.Add(1)
	return session.CivHomebrewState{Mode: "launcher", Launcher: true, FTP: true}, nil
}

func (a *civPanelAdapter) CivHomebrewLaunch(ctx context.Context, address string, event func(session.CivHomebrewEvent)) error {
	a.hbLaunches.Add(1)
	event(session.CivHomebrewEvent{Type: "ready"})
	<-ctx.Done()
	return ctx.Err()
}

func (a *civPanelAdapter) CivHomebrewInstallTrigger(context.Context, string, bool, func(session.CivHomebrewEvent)) (string, error) {
	return "/retained/hb-backup", nil
}

func (a *civPanelAdapter) CivHomebrewRestoreTrigger(context.Context, string, func(session.CivHomebrewEvent)) error {
	return nil
}

func (a *civPanelAdapter) CivHomebrewShutdown(context.Context, string) (session.CivHomebrewStop, error) {
	return session.CivHomebrewStop{Stopped: true}, nil
}

func (a *civPanelAdapter) CivHomebrewInstallPackage(context.Context, string, string, func(session.CivHomebrewEvent)) (session.CivHomebrewApp, error) {
	return session.CivHomebrewApp{ID: "moonlight-civswitch"}, nil
}

func verifiedCivSwitchPanel(t *testing.T, w *civPanelAdapter) (*app, *ui.Tester) {
	t.Helper()
	env := sessiontest.NewFakeEnvironment().Environment()
	env.CivSwitch = w
	a := testApp(t, session.New(env, session.Profile{ID: "test", Revision: "pinned"}))
	tt := ui.NewTester(a.civSwitchView, 1000, 3600)
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
	for _, text := range []string{locales.CivTriggerWarning(), locales.CivCodeWarning(), locales.CivDependentNote(), locales.CivFSHostIntro()} {
		if !tt.HasText(text) {
			t.Fatalf("missing risk disclosure %q: %v", text, tt.Texts())
		}
	}
	for _, label := range []string{locales.CivTrigger(), locales.CivJSSend(), locales.CivHBInstallTrigger(), locales.CivHBRestoreTrigger()} {
		if !disabled(t, tt, label) {
			t.Fatalf("%q is usable without explicit confirmation", label)
		}
	}
	if disabled(t, tt, locales.CivFSStart()) {
		t.Fatal("the CivSwitch FS host requires code consent")
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
	if w.sends.Load() != 1 {
		t.Fatalf("after a confirmed send: %d sends", w.sends.Load())
	}
}

func TestCivSwitchPanelRunsAndStopsFSHostAndCivHomebrew(t *testing.T) {
	w := &civPanelAdapter{}
	a, tt := verifiedCivSwitchPanel(t, w)
	a.civSwitchPanel.fsHostAddress = "192.0.2.10"
	tt.Frame()
	if err := tt.Click(locales.CivFSStart()); err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); a.Controller.Snapshot().CivSwitchActions.FSRunPath == ""; {
		if time.Since(start) > time.Second {
			t.Fatal("the CivSwitch FS host session never reported its run path")
		}
		time.Sleep(time.Millisecond)
	}
	tt.Frame()
	if !tt.HasText(locales.CivFSLive("/retained/fs-workspace")) {
		t.Fatalf("live FS host: %v", tt.Texts())
	}
	if err := tt.Click(locales.CivFSStop()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Verified)
	s := a.Controller.Snapshot()
	if s.CivSwitchActions.FSHostLive || s.SideEffects || !s.LoaderVerified {
		t.Fatalf("after stopping FS host: %+v", s)
	}
	if w.fsHosts.Load() != 1 {
		t.Fatalf("FS host sessions: %d", w.fsHosts.Load())
	}

	if err := tt.Click(locales.CivHBLaunch()); err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); !a.Controller.Snapshot().CivSwitchActions.CivHomebrewLive; {
		if time.Since(start) > time.Second {
			t.Fatal("CivHomebrew never became live")
		}
		time.Sleep(time.Millisecond)
	}
	tt.Frame()
	if err := tt.Click(locales.CivHBStop()); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Verified)
	if w.hbLaunches.Load() != 1 {
		t.Fatalf("CivHomebrew launches: %d", w.hbLaunches.Load())
	}
}

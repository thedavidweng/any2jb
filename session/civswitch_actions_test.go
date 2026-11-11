package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

// civWriter is the gated persistence surface of the route adapter.
type civWriter struct {
	civAdapter
	installs, sends, stages, centers int
	file                             string
	installErr                       error
	center                           func(context.Context, session.CivSwitchObserver) error
}

func (w *civWriter) InstallBootTrigger(ctx context.Context, address string, confirmedSaveReplacement bool, report session.Report) (string, error) {
	if !confirmedSaveReplacement {
		return "", errors.New("unconfirmed replacement reached the adapter")
	}
	w.installs++
	report(session.Progress{SideEffect: true})
	return "/retained/backups/one", w.installErr
}

func (w *civWriter) SendJavaScript(ctx context.Context, address, file string, confirmed bool, report session.Report) error {
	if !confirmed {
		return errors.New("unconfirmed code execution reached the adapter")
	}
	w.sends, w.file = w.sends+1, file
	report(session.Progress{SideEffect: true})
	return nil
}

func (w *civWriter) StageJavaScript(ctx context.Context, address, file string, confirmed bool, report session.Report) error {
	if !confirmed {
		return errors.New("unconfirmed staging reached the adapter")
	}
	w.stages, w.file = w.stages+1, file
	report(session.Progress{SideEffect: true})
	return nil
}

func (w *civWriter) ControlCenter(ctx context.Context, address string, observe session.CivSwitchObserver) error {
	w.centers++
	if w.center != nil {
		return w.center(ctx, observe)
	}
	observe(session.CivSwitchObservation{Ready: true})
	observe(session.CivSwitchObservation{ExportPath: "/retained/save-exports/one"})
	return nil
}

func verifiedCivSwitch(t *testing.T, w *civWriter) *session.SessionController {
	t.Helper()
	f := sessiontest.NewFakeEnvironment()
	env := f.Environment()
	env.CivSwitch = w
	c := session.New(env, session.Profile{ID: "test", Revision: "pinned"})
	if _, err := c.ConnectCivSwitch("192.0.2.2"); err != nil {
		t.Fatal(err)
	}
	if s := await(t, c, session.Verified); !s.LoaderVerified {
		t.Fatal("fixture did not verify the loader")
	}
	return c
}

func TestCivSwitchActionsNeedAVerifiedLoaderAndStagedWriter(t *testing.T) {
	w := &civWriter{}
	c := session.New(func() session.Environment {
		env := sessiontest.NewFakeEnvironment().Environment()
		env.CivSwitch = w
		return env
	}(), session.Profile{ID: "test", Revision: "pinned"})
	for name, action := range map[string]func() (session.Snapshot, error){
		"trigger":        func() (session.Snapshot, error) { return c.InstallCivSwitchTrigger(true) },
		"javascript":     func() (session.Snapshot, error) { return c.SendCivSwitchJavaScript("/tmp/p.js", true) },
		"staging":        func() (session.Snapshot, error) { return c.StageCivSwitchJavaScript("/tmp/p.js", true) },
		"control center": func() (session.Snapshot, error) { return c.StartCivSwitchControlCenter() },
	} {
		if _, err := action(); err == nil {
			t.Fatalf("%s ran without a verified loader", name)
		}
	}
	if w.installs+w.sends+w.stages+w.centers != 0 {
		t.Fatalf("the adapter was reached without a verified loader: %+v", w)
	}
	// A route adapter without the gated surface offers no dependent action.
	plain := &civAdapter{}
	env := sessiontest.NewFakeEnvironment().Environment()
	env.CivSwitch = plain
	bare := session.New(env, session.Profile{ID: "test", Revision: "pinned"})
	if _, err := bare.ConnectCivSwitch("192.0.2.2"); err != nil {
		t.Fatal(err)
	}
	await(t, bare, session.Verified)
	if _, err := bare.InstallCivSwitchTrigger(true); err == nil {
		t.Fatal("an unstaged persistence adapter accepted a trigger install")
	}
}

func TestCivSwitchWritesAndCodeNeedExplicitConfirmation(t *testing.T) {
	w := &civWriter{}
	c := verifiedCivSwitch(t, w)
	if _, err := c.InstallCivSwitchTrigger(false); err == nil {
		t.Fatal("installed the boot trigger without confirming the save replacement")
	}
	if _, err := c.SendCivSwitchJavaScript("/tmp/p.js", false); err == nil {
		t.Fatal("sent arbitrary JavaScript without confirmation")
	}
	if _, err := c.StageCivSwitchJavaScript("/tmp/p.js", false); err == nil {
		t.Fatal("staged a payload without confirmation")
	}
	if _, err := c.SendCivSwitchJavaScript("", true); err == nil {
		t.Fatal("accepted a confirmation without a chosen file")
	}
	if w.installs+w.sends+w.stages != 0 {
		t.Fatalf("an unconfirmed action reached the console: %+v", w)
	}
	if _, err := c.SendCivSwitchJavaScript("/tmp/p.js", true); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Verified)
	if w.sends != 1 || w.file != "/tmp/p.js" || !s.SideEffects {
		t.Fatalf("confirmed send: %+v %+v", w, s)
	}
	if _, err := c.StageCivSwitchJavaScript("/tmp/p.js", true); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.Verified)
	if w.stages != 1 {
		t.Fatalf("confirmed staging: %+v", w)
	}
}

func TestTriggerInstallFailureKeepsBackupAndRequiresConsoleRestart(t *testing.T) {
	w := &civWriter{installErr: errors.New("installer stopped")}
	c := verifiedCivSwitch(t, w)
	if _, err := c.InstallCivSwitchTrigger(true); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Indeterminate)
	if s.CivSwitchActions.TriggerInstalled {
		t.Fatal("an unconfirmed install was recorded as installed")
	}
	if s.CivSwitchActions.BackupPath != "/retained/backups/one" || s.Recovery != session.RestartConsole {
		t.Fatalf("after a failed install: %+v", s)
	}
	w.installErr = nil
	if _, err := c.InstallCivSwitchTrigger(true); err == nil {
		t.Fatal("retried the install from an indeterminate console state")
	}
}

func TestControlCenterRecordsReadOnlyExportAndStopsWithoutFailure(t *testing.T) {
	w := &civWriter{center: func(ctx context.Context, observe session.CivSwitchObserver) error {
		observe(session.CivSwitchObservation{Ready: true})
		observe(session.CivSwitchObservation{ExportPath: "/retained/save-exports/one"})
		<-ctx.Done()
		return ctx.Err()
	}}
	c := verifiedCivSwitch(t, w)
	if _, err := c.StartCivSwitchControlCenter(); err != nil {
		t.Fatal(err)
	}
	live := func() session.Snapshot {
		for start := time.Now(); time.Since(start) < time.Second; time.Sleep(time.Millisecond) {
			if s := c.Snapshot(); s.CivSwitchActions.ControlCenterLive && s.CivSwitchActions.ExportPath != "" {
				return s
			}
		}
		t.Fatal("the Control Center session never became live")
		return session.Snapshot{}
	}()
	if live.SideEffects {
		t.Fatal("a read-only Control Center session reported a console side effect")
	}
	if _, err := c.StopCivSwitchControlCenter(); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Verified)
	if s.CivSwitchActions.ControlCenterLive || !s.LoaderVerified || s.Stage != session.Verified {
		t.Fatalf("after a deliberate stop: %+v", s)
	}
	if s.CivSwitchActions.ExportPath != "/retained/save-exports/one" {
		t.Fatalf("the verified export was not recorded: %+v", s.CivSwitchActions)
	}
	if _, err := c.StopCivSwitchControlCenter(); err == nil {
		t.Fatal("stopped a Control Center session that was not running")
	}
}

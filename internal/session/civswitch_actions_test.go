package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

// civWriter is the gated persistence and application surface of the route
// adapter: everything that needs the verified loader plus the console address.
type civWriter struct {
	civAdapter
	installs, sends    int
	triggers, restores []string
	file               string
	installErr         error
	fs                 []session.CivSwitchFSRequest
	fsRunPath          string
	fsErr              error
	homebrewCalls      []string
	homebrewState      session.CivHomebrewState
	homebrewStop       session.CivHomebrewStop
	homebrewApp        session.CivHomebrewApp
	homebrewErr        error
}

func (w *civWriter) Writer() session.CivSwitchWriter { return w }

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

func (w *civWriter) StartCivSwitchFSHost(ctx context.Context, request session.CivSwitchFSRequest, event func(session.CivSwitchFSEvent)) error {
	w.fs = append(w.fs, request)
	if w.fsErr != nil {
		return w.fsErr
	}
	w.fsRunPath = "/support/runs/civswitch-fs-0123456789abcdef/port/engine/runs/switch2-abc123"
	event(session.CivSwitchFSEvent{Type: "held-ready", RunPath: w.fsRunPath, Message: "CivSwitch FS and RPC verified"})
	<-ctx.Done()
	return ctx.Err()
}

func (w *civWriter) CivHomebrewProbe(ctx context.Context, address string) (session.CivHomebrewState, error) {
	w.homebrewCalls = append(w.homebrewCalls, "probe "+address)
	return w.homebrewState, w.homebrewErr
}

func (w *civWriter) CivHomebrewLaunch(ctx context.Context, address string, event func(session.CivHomebrewEvent)) error {
	w.homebrewCalls = append(w.homebrewCalls, "launch "+address)
	if w.homebrewErr != nil {
		return w.homebrewErr
	}
	event(session.CivHomebrewEvent{Type: "ready", Message: "CivHomebrew is running on the Switch"})
	<-ctx.Done()
	return ctx.Err()
}

func (w *civWriter) CivHomebrewInstallTrigger(ctx context.Context, address string, confirmed bool, event func(session.CivHomebrewEvent)) (string, error) {
	if !confirmed {
		return "", errors.New("unconfirmed save replacement reached the adapter")
	}
	w.triggers = append(w.triggers, address)
	return "/support/civswitch/civhomebrew/backups/" + address, w.homebrewErr
}

func (w *civWriter) CivHomebrewRestoreTrigger(ctx context.Context, address string, event func(session.CivHomebrewEvent)) error {
	w.restores = append(w.restores, address)
	return w.homebrewErr
}

func (w *civWriter) CivHomebrewShutdown(ctx context.Context, address string) (session.CivHomebrewStop, error) {
	w.homebrewCalls = append(w.homebrewCalls, "shutdown "+address)
	return w.homebrewStop, w.homebrewErr
}

func (w *civWriter) CivHomebrewInstallPackage(ctx context.Context, address, spec string, event func(session.CivHomebrewEvent)) (session.CivHomebrewApp, error) {
	w.homebrewCalls = append(w.homebrewCalls, "install "+spec)
	return w.homebrewApp, w.homebrewErr
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
		"trigger":    func() (session.Snapshot, error) { return c.InstallCivSwitchTrigger(true) },
		"javascript": func() (session.Snapshot, error) { return c.SendCivSwitchJavaScript("/tmp/p.js", true) },
		"fs host":    func() (session.Snapshot, error) { return c.StartCivSwitchFSHost("192.0.2.3", true) },
		"homebrew":   func() (session.Snapshot, error) { return c.ProbeCivHomebrew() },
	} {
		if _, err := action(); err == nil {
			t.Fatalf("%s ran without a verified loader", name)
		}
	}
	if w.installs+w.sends+len(w.fs)+len(w.homebrewCalls) != 0 {
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
	if _, err := bare.StartCivSwitchFSHost("192.0.2.3", true); err == nil {
		t.Fatal("an adapter without the FS host accepted a supervisor start")
	}
	if _, err := bare.ProbeCivHomebrew(); err == nil {
		t.Fatal("an adapter without CivHomebrew accepted a probe")
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
	if _, err := c.SendCivSwitchJavaScript("", true); err == nil {
		t.Fatal("accepted a confirmation without a chosen file")
	}
	if w.installs+w.sends != 0 {
		t.Fatalf("an unconfirmed action reached the console: %+v", w)
	}
	if _, err := c.SendCivSwitchJavaScript("/tmp/p.js", true); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Verified)
	if w.sends != 1 || w.file != "/tmp/p.js" || !s.SideEffects {
		t.Fatalf("confirmed send: %+v %+v", w, s)
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

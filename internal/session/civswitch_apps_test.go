package session_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/session"
)

func TestCivSwitchFSHostRecordsTheHeldRunAndStopsCleanly(t *testing.T) {
	w := &civWriter{}
	c := verifiedCivSwitch(t, w)
	if _, err := c.StartCivSwitchFSHost("not an address", true); err == nil {
		t.Fatal("accepted a host address that is not on the LAN")
	}
	if len(w.fs) != 0 {
		t.Fatal("started the supervisor without a host address")
	}
	if _, err := c.StartCivSwitchFSHost("192.0.2.3", true); err != nil {
		t.Fatal(err)
	}
	held := func() session.Snapshot {
		for start := time.Now(); time.Since(start) < time.Second; time.Sleep(time.Millisecond) {
			if s := c.Snapshot(); s.CivSwitchActions.FSRunPath != "" {
				return s
			}
		}
		t.Fatal("the supervisor never reported a held run")
		return session.Snapshot{}
	}()
	if len(w.fs) != 1 || w.fs[0].SwitchIPv4 != "192.0.2.2" || w.fs[0].HostIPv4 != "192.0.2.3" || !w.fs[0].WaitForLoader {
		t.Fatalf("supervisor request: %+v", w.fs)
	}
	if !held.CivSwitchActions.FSHostLive || held.CivSwitchActions.FSRunPath != w.fsRunPath {
		t.Fatalf("held state: %+v", held.CivSwitchActions)
	}
	if _, err := c.StopCivSwitchFSHost(); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Verified)
	if s.CivSwitchActions.FSHostLive || !s.LoaderVerified {
		t.Fatalf("after a deliberate stop: %+v", s)
	}
	if s.CivSwitchActions.FSRunPath != w.fsRunPath {
		t.Fatalf("the verified held run was not retained: %+v", s.CivSwitchActions)
	}
	if _, err := c.StopCivSwitchFSHost(); err == nil {
		t.Fatal("stopped a supervisor that was not running")
	}
}

func TestCivSwitchFSHostFailureIsReportedNotSwallowed(t *testing.T) {
	w := &civWriter{fsErr: errors.New("CivSwitch FS host reported a run path outside its staged source")}
	c := verifiedCivSwitch(t, w)
	if _, err := c.StartCivSwitchFSHost("192.0.2.3", false); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Failed)
	if s.CivSwitchActions.FSHostLive {
		t.Fatal("a failed supervisor stayed live")
	}
	if s.Recovery != session.RetrySafe {
		t.Fatalf("failure recovery: %+v", s)
	}
}

func TestCivHomebrewActionsUseTheVerifiedConsole(t *testing.T) {
	w := &civWriter{
		homebrewState: session.CivHomebrewState{Mode: "launcher", Launcher: true, FTP: true},
		homebrewStop:  session.CivHomebrewStop{Blocked: true, Message: "Close the foreground app on your Switch"},
		homebrewApp:   session.CivHomebrewApp{ID: "moonlight-civswitch", Name: "Moonlight", Pinned: true},
	}
	c := verifiedCivSwitch(t, w)
	if _, err := c.ProbeCivHomebrew(); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Verified)
	if s.CivSwitchActions.CivHomebrew.Mode != "launcher" || !s.CivSwitchActions.CivHomebrew.FTP {
		t.Fatalf("probe state: %+v", s.CivSwitchActions.CivHomebrew)
	}
	if _, err := c.ShutdownCivHomebrew(); err != nil {
		t.Fatal(err)
	}
	s = await(t, c, session.Verified)
	if !strings.Contains(s.CivSwitchActions.CivHomebrew.Message, "Close the foreground app") {
		t.Fatalf("blocked shutdown: %+v", s.CivSwitchActions.CivHomebrew)
	}
	if _, err := c.InstallCivHomebrewTrigger(false); err == nil {
		t.Fatal("installed the CivHomebrew trigger without confirming the save replacement")
	}
	if len(w.triggers) != 0 {
		t.Fatal("an unconfirmed trigger install reached the adapter")
	}
	if _, err := c.InstallCivHomebrewTrigger(true); err != nil {
		t.Fatal(err)
	}
	s = await(t, c, session.Verified)
	if s.CivSwitchActions.CivHomebrewBackupPath != "/support/civswitch/civhomebrew/backups/192.0.2.2" {
		t.Fatalf("trigger backup: %+v", s.CivSwitchActions)
	}
	if _, err := c.RestoreCivHomebrewTrigger(); err != nil {
		t.Fatal(err)
	}
	await(t, c, session.Verified)
	if len(w.restores) != 1 {
		t.Fatal("restore did not reach the adapter")
	}
	if _, err := c.InstallCivHomebrewPackage(""); err == nil {
		t.Fatal("installed a package without choosing one")
	}
	if _, err := c.InstallCivHomebrewPackage("moonlight-civswitch"); err != nil {
		t.Fatal(err)
	}
	s = await(t, c, session.Verified)
	if s.CivSwitchActions.CivHomebrewPackage.ID != "moonlight-civswitch" || !s.CivSwitchActions.CivHomebrewPackage.Pinned {
		t.Fatalf("installed package: %+v", s.CivSwitchActions.CivHomebrewPackage)
	}
}

func TestCivHomebrewLaunchStopsWithoutAFailure(t *testing.T) {
	w := &civWriter{}
	c := verifiedCivSwitch(t, w)
	if _, err := c.StopCivHomebrew(); err == nil {
		t.Fatal("stopped CivHomebrew before launching it")
	}
	if _, err := c.LaunchCivHomebrew(); err != nil {
		t.Fatal(err)
	}
	live := func() session.Snapshot {
		for start := time.Now(); time.Since(start) < time.Second; time.Sleep(time.Millisecond) {
			if s := c.Snapshot(); s.CivSwitchActions.CivHomebrewLive {
				return s
			}
		}
		t.Fatal("CivHomebrew never became live")
		return session.Snapshot{}
	}()
	if live.CivSwitchActions.CivHomebrew.Message != "CivHomebrew is running on the Switch" {
		t.Fatalf("launch event: %+v", live.CivSwitchActions.CivHomebrew)
	}
	if _, err := c.StopCivHomebrew(); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Verified)
	if s.CivSwitchActions.CivHomebrewLive || !s.LoaderVerified {
		t.Fatalf("after a deliberate stop: %+v", s)
	}
	if len(w.homebrewCalls) != 1 || w.homebrewCalls[0] != "launch 192.0.2.2" {
		t.Fatalf("adapter calls: %v", w.homebrewCalls)
	}
}

func TestCivHomebrewProbeFailureLeavesTheLoaderVerified(t *testing.T) {
	w := &civWriter{homebrewErr: errors.New("the service on port 9027 is not a compatible CivHomebrew companion")}
	c := verifiedCivSwitch(t, w)
	if _, err := c.ProbeCivHomebrew(); err != nil {
		t.Fatal(err)
	}
	s := await(t, c, session.Failed)
	if !s.LoaderVerified || s.CivSwitchActions.CivHomebrew.Mode != "" {
		t.Fatalf("after a failed probe: %+v", s)
	}
}

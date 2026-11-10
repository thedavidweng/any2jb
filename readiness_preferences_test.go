package main

import (
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestLANOverrideDoesNotDiscardConfirmedBootstrap(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	p := a.controller.Snapshot().Profile
	a.controller.UseSavedBootstrap(session.BootstrapRecord{
		ProfileID: p.ID, Revision: p.Revision, SourceSHA256: p.SourceSHA256, BootstrapSHA256: p.BootstrapSHA256, SaveName: p.SaveName, Edition: "full", Confirmed: true,
	})
	a.preferences.LANIPv4 = "192.168.1.10"
	if err := a.applyPreferences(); err != nil {
		t.Fatal(err)
	}
	if !a.controller.Snapshot().BootstrapSaved {
		t.Fatal("network preference erased matching saved bootstrap confirmation")
	}
}

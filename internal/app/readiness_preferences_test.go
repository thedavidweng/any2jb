package app

import (
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

func TestLANOverrideDoesNotDiscardConfirmedBootstrap(t *testing.T) {
	a, err := Load(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.InitializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	p := a.Controller.Snapshot().Profile
	a.Controller.UseSavedBootstrap(session.BootstrapRecord{
		ProfileID: p.ID, Revision: p.Revision, SourceSHA256: p.SourceSHA256, BootstrapSHA256: p.BootstrapSHA256, SaveName: p.SaveName, Edition: "full", Confirmed: true,
	})
	a.Preferences.LANIPv4 = "192.168.1.10"
	if err := a.ApplyPreferences(); err != nil {
		t.Fatal(err)
	}
	if !a.Controller.Snapshot().BootstrapSaved {
		t.Fatal("network preference erased matching saved bootstrap confirmation")
	}
}

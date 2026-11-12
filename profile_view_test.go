package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestHostOptionsAreDisclosedAndCapabilitiesExplained(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.profileView, 720, 1000)
	if tt.HasText("Host and input preferences") {
		t.Fatal("expert options shown in onboarding")
	}
	if err := tt.Click("Developer Options"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Host and input preferences") {
		t.Fatal(tt.Texts())
	}
	a.preferences.Profile = "pepsipu"
	tt.Frame()
	if !tt.HasText("Not supported by this host. Choose a compatible host in Developer Options.") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"Retained RPC", "Demo edition", "SaveKit", "Native HBLauncher", "Modloader"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s is not disabled: %+v", label, control)
		}
	}
	if err := tt.Click("Developer Options"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Developer Options"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func TestUnappliedHostChoiceCannotLaunchPreviousHost(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	p := a.controller.Snapshot().Profile
	if _, err := a.controller.UseSavedBootstrap(session.BootstrapRecord{ProfileID: p.ID, Revision: p.Revision,
		SourceSHA256: p.SourceSHA256, BootstrapSHA256: p.BootstrapSHA256, SaveName: p.SaveName, Edition: "full", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	a.flow.stage = flowLaunch
	tt := ui.NewTester(a.view, 720, 1000)
	if _, err := a.controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	a.preferences.Profile = "izmysh"
	tt.Frame()
	start, ok := tt.AccessByRole(ui.RoleButton, "Guided manual launch")
	if !ok || !start.Disabled {
		t.Fatal("pending host choice can launch old host")
	}
}

func TestAllSixHostsCanBeSelected(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.developer = true
	tt := ui.NewTester(a.profileView, 720, 1200)
	for _, id := range []string{"pepsipu", "cellenseres", "BermTheSequel", "izmysh", "astral", "kurumuz"} {
		if err := tt.Click(a.preferences.Profile); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(id); err != nil {
			t.Fatal(err)
		}
		if a.preferences.Profile != id {
			t.Fatalf("wanted %s, got %s", id, a.preferences.Profile)
		}
	}
}

func TestIZMYSHQualificationBlockerIsVisible(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.preferences.Profile = "izmysh"
	tt := ui.NewTester(a.profileView, 720, 1000)
	if !tt.HasText("IZMYSH cannot launch yet. Its separate native qualification adapter is not implemented.") {
		t.Fatal(tt.Texts())
	}
}

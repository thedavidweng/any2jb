package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/locales"
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
	if err := tt.Click("Developer options"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Host and input preferences") {
		t.Fatal(tt.Texts())
	}
	a.Preferences.Profile = "pepsipu"
	tt.Frame()
	if !tt.HasText("Not supported by this host. Choose a compatible host under Developer options in Settings.") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"Retained RPC", "Demo edition", "SaveKit", "Native HBLauncher", "Modloader"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s is not disabled: %+v", label, control)
		}
	}
	if err := tt.Click("Developer options"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Developer options"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func TestUnappliedHostChoiceCannotLaunchPreviousHost(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.driverPanel.gate = blockedDriverGate(a.Manifest)
	if err := a.initializeSession(sessiontest.NewFakeEnvironment().Environment()); err != nil {
		t.Fatal(err)
	}
	p := a.Controller.Snapshot().Profile
	if _, err := a.Controller.UseSavedBootstrap(session.BootstrapRecord{ProfileID: p.ID, Revision: p.Revision,
		SourceSHA256: p.SourceSHA256, BootstrapSHA256: p.BootstrapSHA256, SaveName: p.SaveName, Edition: "full", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	a.flow.stage = flowLaunch
	tt := ui.NewTester(a.view, 720, 1000)
	if _, err := a.Controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	a.Preferences.Profile = "izmysh"
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
		choice, ok := tt.AccessByRole(ui.RolePopUpButton, locales.HostProfile())
		if !ok {
			t.Fatal("host profile choice missing", tt.Texts())
		}
		tt.ClickAt(choice.Bounds.X+choice.Bounds.W/2, choice.Bounds.Y+choice.Bounds.H/2)
		if err := tt.Click(id); err != nil {
			t.Fatal(err)
		}
		if a.Preferences.Profile != id {
			t.Fatalf("wanted %s, got %s", id, a.Preferences.Profile)
		}
	}
}

func TestIZMYSHQualificationBlockerIsVisible(t *testing.T) {
	a, err := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.Preferences.Profile = "izmysh"
	tt := ui.NewTester(a.profileView, 720, 1000)
	if !tt.HasText("IZMYSH cannot launch yet. Its separate native qualification adapter is not implemented.") {
		t.Fatal(tt.Texts())
	}
}

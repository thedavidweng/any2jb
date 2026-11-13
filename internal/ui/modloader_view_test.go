package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
)

const (
	installMods   = "Install mods (final action)"
	launchCustom  = "Launch custom homebrew (experimental)"
	modsInstalled = "Modloader installed. Finish this session, close SmileBASIC without restarting the console, then launch the game. Every tool stays locked until the console restarts."
)

func TestModloaderNeedsTheHostCapability(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	_, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{"mintbrew"})
	if !tt.HasText(installMods) || !tt.HasText("This host profile does not enable this tool.") {
		t.Fatal(tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, installMods); ok {
		t.Fatal("the modloader is an action on a host without its capability")
	}
}

func TestModloaderShowsItsRecordedReason(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.DistributionFor("modloader")
	if err != nil {
		t.Fatal(err)
	}
	f := sessiontest.NewFakeEnvironment()
	_, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{reasons: map[string]string{"modloader": d.Reason}}, []string{"modloader"})
	if !tt.HasText(d.Reason) {
		t.Fatal(tt.Texts())
	}
}

func TestModsFolderReachesTheToolRequest(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	a, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{"modloader"})
	mods := t.TempDir()
	file := filepath.Join(mods, "contents", "0100000000010000", "romfs", "data.bin")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.homebrewPanel.modsFolder = mods
	tt.Frame()
	if err := tt.Click(modConsent); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(installMods); err != nil {
		t.Fatal(err)
	}
	if request := awaitToolRequest(t, f); request.Tool != "modloader" || request.ModsFolder != mods || !request.CodeConsent {
		t.Fatalf("unexpected tool request: %+v", request)
	}
	if a.homebrewPanel.modsConsent {
		t.Fatal("consent was not consumed")
	}
}

const modConsent = "I confirm running mods as code on the console. The overlay stays until restart. This is the session's final action and blocks every later tool. No backup is taken because nothing is written to saves."

func TestChangingModsFolderClearsConsent(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	a, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{"modloader"})
	a.homebrewPanel.modsFolder = t.TempDir()
	tt.Frame()
	if err := tt.Click(modConsent); err != nil {
		t.Fatal(err)
	}
	a.homebrewPanel.modsFolder = t.TempDir()
	tt.Frame()
	control, _ := tt.AccessByRole(ui.RoleButton, installMods)
	if !control.Disabled || a.homebrewPanel.modsConsent {
		t.Fatal("a different folder reused consent")
	}
}

func TestInstallModsNeedsExplicitConsent(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	a, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{"modloader"})
	a.homebrewPanel.modsFolder = t.TempDir()
	tt.Frame()
	control, ok := tt.AccessByRole(ui.RoleButton, installMods)
	if !ok || !control.Disabled {
		t.Fatalf("Install mods is usable without explicit consent: %+v", control)
	}
	if err := tt.Click(installMods); err != nil {
		t.Fatal(err)
	}
	if f.ToolRequest.Tool != "" {
		t.Fatal("unconfirmed mods reached the tool")
	}
}

func TestInvalidCustomHomebrewFolderKeepsTheLease(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	a, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{"custom-homebrew"})
	if !tt.HasText("Custom homebrew (experimental)") {
		t.Fatal(tt.Texts())
	}
	a.homebrewPanel.customFolder = t.TempDir()
	tt.Frame()
	if err := tt.Click(homebrewCodeConsent); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(launchCustom); err != nil {
		t.Fatal(err)
	}
	if !a.commandRejected || f.ToolRequest.Tool != "" {
		t.Fatalf("an invalid custom homebrew folder was not refused: %+v", f.ToolRequest)
	}
	if stage := a.Controller.Snapshot().Stage; stage != session.RetainedRPC {
		t.Fatalf("the refusal dropped the retained lease: %s", stage)
	}
}

const homebrewCodeConsent = "I confirm running user-provided homebrew code on the console for this action."

func TestUserHomebrewNeedsConsentAndChangingFolderClearsIt(t *testing.T) {
	for _, tool := range []string{"custom-homebrew", "native-hblauncher"} {
		t.Run(tool, func(t *testing.T) {
			f := sessiontest.NewFakeEnvironment()
			a, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{tool})
			folder, button := &a.homebrewPanel.customFolder, launchCustom
			if tool == "native-hblauncher" {
				folder, button = &a.homebrewPanel.appFolder, "Launch native HBLauncher"
			}
			*folder = t.TempDir()
			tt.Frame()
			control, _ := tt.AccessByRole(ui.RoleButton, button)
			if !control.Disabled {
				t.Fatal("user code can launch without consent")
			}
			if err := tt.Click(homebrewCodeConsent); err != nil {
				t.Fatal(err)
			}
			control, _ = tt.AccessByRole(ui.RoleButton, button)
			if control.Disabled {
				t.Fatal("consent did not enable launch")
			}
			*folder = t.TempDir()
			tt.Frame()
			control, _ = tt.AccessByRole(ui.RoleButton, button)
			if !control.Disabled {
				t.Fatal("changed homebrew folder reused consent")
			}
		})
	}
}

func TestInstalledModloaderLocksEveryTool(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{state: retained.State{ModloaderInstalled: true}}
	_, tt := leasedHomebrewApp(t, f, adapter, []string{"rpc-status", "mintbrew", "custom-homebrew", "modloader"})
	if !tt.HasText(modsInstalled) {
		t.Fatal(tt.Texts())
	}
	if status, ok := tt.AccessByRole(ui.RoleStatus, modsInstalled); !ok {
		t.Fatalf("the reboot requirement is not a status: %+v", status)
	}
	for _, label := range []string{"Launch MintBrew", launchCustom, installMods} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s is usable after the modloader handoff: %+v", label, control)
		}
	}
}

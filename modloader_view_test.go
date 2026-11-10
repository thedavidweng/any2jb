package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
)

const (
	installMods   = "Install mods (final action)"
	launchCustom  = "Launch custom homebrew (experimental)"
	modsInstalled = "Modloader installed. Finish this session, close SmileBASIC without restarting the console, then launch the game. Every tool stays locked until the console restarts."
)

func TestModloaderNeedsTheHostCapability(t *testing.T) {
	f := session.NewFakeEnvironment()
	_, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{"mintbrew"})
	if !tt.HasText("Installing mods is the final action of this session. The mod overlay stays active until the console restarts, and no other tool can launch in the same boot. Only Kurumuz and Cellenseres hosts support it. No game files or mods are included; choose your own folder.") {
		t.Fatal(tt.Texts())
	}
	control, ok := tt.AccessByRole(ui.RoleButton, installMods)
	if !ok || !control.Disabled {
		t.Fatalf("the modloader is usable on a host without its capability: %+v", control)
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
	f := session.NewFakeEnvironment()
	_, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{reasons: map[string]string{"modloader": d.Reason}}, []string{"modloader"})
	if !tt.HasText(d.Reason) {
		t.Fatal(tt.Texts())
	}
}

func TestModsFolderReachesTheToolRequest(t *testing.T) {
	f := session.NewFakeEnvironment()
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
	if err := tt.Click(installMods); err != nil {
		t.Fatal(err)
	}
	if request := awaitToolRequest(t, f); request.Tool != "modloader" || request.ModsFolder != mods {
		t.Fatalf("unexpected tool request: %+v", request)
	}
}

func TestInvalidCustomHomebrewFolderKeepsTheLease(t *testing.T) {
	f := session.NewFakeEnvironment()
	a, tt := leasedHomebrewApp(t, f, &fakeToolAdapter{}, []string{"custom-homebrew"})
	if !tt.HasText("Custom homebrew (experimental)") {
		t.Fatal(tt.Texts())
	}
	a.homebrewPanel.customFolder = t.TempDir()
	tt.Frame()
	if err := tt.Click(launchCustom); err != nil {
		t.Fatal(err)
	}
	if !a.commandRejected || f.ToolRequest.Tool != "" {
		t.Fatalf("an invalid custom homebrew folder was not refused: %+v", f.ToolRequest)
	}
	if stage := a.controller.Snapshot().Stage; stage != session.RetainedRPC {
		t.Fatalf("the refusal dropped the retained lease: %s", stage)
	}
}

func TestInstalledModloaderLocksEveryTool(t *testing.T) {
	f := session.NewFakeEnvironment()
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

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

// launcherPreview is a prepared package preview the fake adapter returns.
func launcherPreview() *savekit.Preview {
	body := sha256.Sum256([]byte("core.lua"))
	files := []savekit.ExportFile{{Path: "/core.lua", Size: 8, SHA256: hex.EncodeToString(body[:])}}
	return &savekit.Preview{Files: files, Bytes: 8, Digest: hex.EncodeToString(body[:])}
}

// launcherApp publishes a prepared preview the way the window does: on the
// main thread, after the background preparation finished.
func launcherApp(t *testing.T, adapter *fakeToolAdapter) (*app, *ui.Tester, <-chan struct{}) {
	t.Helper()
	f := session.NewFakeEnvironment()
	a := saveKitApp(t, f, adapter)
	published := make(chan struct{}, 4)
	a.launcherPanel.update = func(apply func()) { apply(); published <- struct{}{} }
	tt := ui.NewTester(a.saveKitView, 820, 4200)
	return a, tt, published
}

// previewPackage clicks Preview and waits for the prepared result.
func previewPackage(t *testing.T, tt *ui.Tester, published <-chan struct{}) {
	t.Helper()
	if err := tt.Click("Preview launcher install"); err != nil {
		t.Fatal(err)
	}
	<-published
	tt.Frame()
}

func TestGatedLauncherInstallerStatesItsRecordedReason(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.DistributionFor("launcher-seedsow-demo")
	if err != nil {
		t.Fatal(err)
	}
	adapter := &fakeToolAdapter{reasons: map[string]string{"launcher-seedsow-demo": d.Reason}}
	a, tt, _ := launcherApp(t, adapter)
	saveKitLease(t, a, tt)
	if !tt.HasText(d.Reason) {
		t.Fatalf("missing recorded reason: %q", tt.Texts())
	}
	for _, label := range []string{"Preview launcher install", "Back up and install launcher"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s is usable for a gated package: %+v", label, control)
		}
	}
}

func TestLauncherInstallerStatesItsPersistentSideEffects(t *testing.T) {
	a, tt, _ := launcherApp(t, &fakeToolAdapter{})
	saveKitLease(t, a, tt)
	for _, text := range []string{
		"Persistent side effects: the installed save replaces this game's startup code until you restore the backup, and the launcher opens network services on your LAN whenever the game runs (Artemis HTTP on port 8080, or a Lua service on port 9026 with FTP on ports 5000 and 5001). That runtime is separate from the held RPC session and keeps running after Finish.",
		"An existing launcher whose core files are unrecognised or incomplete is left untouched: the verified backup is kept and no file is written.",
	} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q in %q", text, tt.Texts())
		}
	}
}

// A staged package installs only after its own preview and confirmation, and
// only once the account that owns the save is selected.
func TestLauncherInstallNeedsPreviewConfirmationAndAccount(t *testing.T) {
	adapter := &fakeToolAdapter{preview: launcherPreview(),
		state: retained.State{SaveKit: &retained.SaveKitState{Users: []string{
			"11111111111111111111111111111111", "22222222222222222222222222222222"}}}}
	a, tt, published := launcherApp(t, adapter)
	saveKitLease(t, a, tt)
	install, ok := tt.AccessByRole(ui.RoleButton, "Back up and install launcher")
	if !ok || !install.Disabled {
		t.Fatalf("install is usable without a preview: %+v", install)
	}
	previewPackage(t, tt, published)
	if a.launcherPanel.preview == nil {
		t.Fatal("the package was not previewed")
	}
	if install, _ := tt.AccessByRole(ui.RoleButton, "Back up and install launcher"); !install.Disabled {
		t.Fatal("install is usable without a confirmation")
	}
	if err := tt.Click("I confirm installing this launcher save to the selected game and account"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// Two accounts are offered, so none is assumed and the install waits.
	if install, _ := tt.AccessByRole(ui.RoleButton, "Back up and install launcher"); !install.Disabled {
		t.Fatal("install ran without the owning account selected")
	}
	a.launcherPanel.account = "11111111111111111111111111111111"
	tt.Frame()
	if a.launcherPanel.preview != nil || a.launcherPanel.confirm {
		t.Fatal("a changed account kept its preview or confirmation")
	}
	previewPackage(t, tt, published)
	if err := tt.Click("I confirm installing this launcher save to the selected game and account"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if install, _ := tt.AccessByRole(ui.RoleButton, "Back up and install launcher"); install.Disabled {
		t.Fatalf("a previewed and confirmed install for a selected account stayed disabled: %q", tt.Texts())
	}
}

// Changing the package invalidates the preview and its confirmation.
func TestLauncherPreviewResetsWhenTheTargetChanges(t *testing.T) {
	adapter := &fakeToolAdapter{preview: launcherPreview(),
		state: retained.State{SaveKit: &retained.SaveKitState{
			Users: []string{"11111111111111111111111111111111"}}}}
	a, tt, published := launcherApp(t, adapter)
	saveKitLease(t, a, tt)
	previewPackage(t, tt, published)
	if err := tt.Click("I confirm installing this launcher save to the selected game and account"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if install, _ := tt.AccessByRole(ui.RoleButton, "Back up and install launcher"); install.Disabled {
		t.Fatalf("a previewed and confirmed install stayed disabled: %q", tt.Texts())
	}
	a.launcherPanel.pkg = "Gravity Circuit (RCE v1.1)"
	tt.Frame()
	if a.launcherPanel.preview != nil || a.launcherPanel.confirm {
		t.Fatal("a changed package kept its preview or confirmation")
	}
	if install, _ := tt.AccessByRole(ui.RoleButton, "Back up and install launcher"); !install.Disabled {
		t.Fatal("install stayed usable for a package without its own preview")
	}
}

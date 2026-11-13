package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/savekit"
	"github.com/thedavidweng/any2jb/internal/session/sessiontest"
	"github.com/thedavidweng/any2jb/locales"
)

// launcherPreview is a prepared package preview the fake adapter returns.
func launcherPreview() *savekit.Preview {
	body := sha256.Sum256([]byte("core.lua"))
	files := []savekit.ExportFile{{Path: "/core.lua", Size: 8, SHA256: hex.EncodeToString(body[:])}}
	return &savekit.Preview{Files: files, Bytes: 8, Digest: hex.EncodeToString(body[:])}
}

// launcherApp models Window.Update: the preparing goroutine only hands its
// closure over, and the test's own goroutine applies it, so panel state is
// written on the thread that builds the view.
func launcherApp(t *testing.T, adapter *fakeToolAdapter) (*app, *ui.Tester, <-chan func()) {
	t.Helper()
	a, tt, published, _ := launcherAppWithEnv(t, adapter)
	return a, tt, published
}

func launcherAppWithEnv(t *testing.T, adapter *fakeToolAdapter) (*app, *ui.Tester, <-chan func(), *sessiontest.FakeEnvironment) {
	t.Helper()
	f := sessiontest.NewFakeEnvironment()
	a := saveKitApp(t, f, adapter)
	published := make(chan func(), 4)
	a.launcherPanel.update = func(apply func()) { published <- apply }
	tt := ui.NewTester(a.saveKitView, 820, 4200)
	return a, tt, published, f
}

// previewPackage clicks Preview, applies the prepared result on this
// goroutine, and builds the view again.
func previewPackage(t *testing.T, tt *ui.Tester, published <-chan func()) {
	t.Helper()
	if err := tt.Click("Preview launcher install"); err != nil {
		t.Fatal(err)
	}
	(<-published)()
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
	a, tt, published, f := launcherAppWithEnv(t, adapter)
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
	if err := tt.Click(locales.LauncherConfirm()); err != nil {
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
	if err := tt.Click(locales.LauncherConfirm()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if install, _ := tt.AccessByRole(ui.RoleButton, "Back up and install launcher"); install.Disabled {
		t.Fatalf("a previewed and confirmed install for a selected account stayed disabled: %q", tt.Texts())
	}
	if err := tt.Click("Back up and install launcher"); err != nil {
		t.Fatal(err)
	}
	// The gate prepares the package again; no preview folder is kept or sent.
	request := awaitToolRequest(t, f)
	saveReq, ok := request.Save.(*savekit.Request)
	if !ok || saveReq == nil || saveReq.SourceFolder != "" || saveReq.Launcher == nil {
		t.Fatalf("launcher install request: %+v", request)
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
	if err := tt.Click(locales.LauncherConfirm()); err != nil {
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

// Every installer control carries an accessible name and the refusal is a
// status region, so a screen reader reads why nothing was written.
func TestLauncherControlsAreAccessible(t *testing.T) {
	adapter := &fakeToolAdapter{state: retained.State{SaveKit: &retained.SaveKitState{
		Users: []string{"11111111111111111111111111111111"}}}}
	a, tt, _ := launcherApp(t, adapter)
	saveKitLease(t, a, tt)
	a.launcherPanel.failed = true
	tt.Frame()
	for _, info := range tt.AccessAll() {
		switch info.Role {
		case ui.RoleButton, ui.RoleCheckBox, ui.RoleTextField, ui.RolePopUpButton, ui.RoleComboBox:
			if info.Name == "" {
				t.Errorf("control without an accessible name: %+v", info)
			}
		}
	}
	status := false
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleStatus && info.Name == "The launcher package could not be prepared from its pinned, digest-verified source. No save was changed." {
			status = true
		}
	}
	if !status {
		t.Fatalf("the refusal is not a status region: %+v", tt.AccessAll())
	}
}

// A preview that finishes after the selection changed belongs to another
// package and is discarded.
func TestLauncherStalePreviewIsDiscarded(t *testing.T) {
	adapter := &fakeToolAdapter{preview: launcherPreview(),
		state: retained.State{SaveKit: &retained.SaveKitState{
			Users: []string{"11111111111111111111111111111111"}}}}
	a, tt, published := launcherApp(t, adapter)
	saveKitLease(t, a, tt)
	if err := tt.Click("Preview launcher install"); err != nil {
		t.Fatal(err)
	}
	a.launcherPanel.pkg = "Gravity Circuit (RCE v1.1)"
	tt.Frame()
	(<-published)()
	tt.Frame()
	if a.launcherPanel.preview != nil {
		t.Fatal("a preview for the previous package was shown for the new one")
	}
	if _, ok := tt.AccessByRole(ui.RoleCheckBox, locales.LauncherConfirm()); ok {
		t.Fatal("a stale preview offered its confirmation")
	}
}

func TestLauncherInstallIsDisabledAfterModloaderHandoff(t *testing.T) {
	adapter := &fakeToolAdapter{preview: launcherPreview(),
		state: retained.State{SaveKit: &retained.SaveKitState{
			Users: []string{"11111111111111111111111111111111"}}}}
	a, tt, published := launcherApp(t, adapter)
	saveKitLease(t, a, tt)
	previewPackage(t, tt, published)
	if err := tt.Click(locales.LauncherConfirm()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	adapter.state.ModloaderInstalled = true
	tt.Frame()
	if install, _ := tt.AccessByRole(ui.RoleButton, "Back up and install launcher"); !install.Disabled {
		t.Fatal("a launcher install stayed usable after the modloader handoff")
	}
}

func TestLauncherAccountClearsWhenTheListDisappears(t *testing.T) {
	adapter := &fakeToolAdapter{state: retained.State{SaveKit: &retained.SaveKitState{
		Users: []string{"11111111111111111111111111111111"}}}}
	a, tt, _ := launcherApp(t, adapter)
	saveKitLease(t, a, tt)
	if a.launcherPanel.account == "" {
		t.Fatal("the only account was not selected")
	}
	adapter.state.SaveKit = nil
	tt.Frame()
	if a.launcherPanel.account != "" {
		t.Fatalf("a hidden account choice still targets the install: %q", a.launcherPanel.account)
	}
}

func TestLauncherLabelsHaveNoDefault(t *testing.T) {
	if label, ok := launcherLabel("launcher-unrecorded"); ok || label != "" {
		t.Fatalf("an unrecorded launcher got a label: %q", label)
	}
	m := testManifest(t)
	for _, id := range retained.LauncherIDs(m) {
		if _, ok := launcherLabel(id); !ok {
			t.Fatalf("manifest launcher %s has no label", id)
		}
	}
}

func TestLauncherConfirmationWarnsThatMatchingFilesAreReplaced(t *testing.T) {
	for locale, phrase := range map[string]string{
		"en":    "Existing files with the same names will be replaced.",
		"zh-CN": "同名的现有文件将被替换。",
	} {
		if confirmation := locales.In(locale).LauncherConfirm(); !strings.Contains(confirmation, phrase) {
			t.Errorf("%s launcher confirmation omits the replacement warning: %q", locale, confirmation)
		}
	}
}

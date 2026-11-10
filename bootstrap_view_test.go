package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/session"
)

func TestExistingBootstrapRequiresDeliberateIdentityConfirmation(t *testing.T) {
	a, _ := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	a.initializeSession(session.NewFakeEnvironment().Environment())
	tt := ui.NewTester(a.view, 640, 900)
	if err := tt.Click("Use saved bootstrap"); err != nil {
		t.Fatal(err)
	}
	if a.controller.Snapshot().BootstrapSaved {
		t.Fatal("opening confirmation trusted a console save")
	}
	if !slices.Contains(tt.Texts(), "I confirm this exact saved bootstrap") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("I confirm this exact saved bootstrap"); err != nil {
		t.Fatal(err)
	}
	if !a.controller.Snapshot().BootstrapSaved {
		t.Fatal("explicit confirmation did not enable reuse")
	}
}

func TestBootstrapSaveRequiresExplicitOverwritePermission(t *testing.T) {
	a, _ := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	a.initializeSession(session.NewFakeEnvironment().Environment())
	a.controller.Inspect()
	waitBootstrap(t, a.controller)
	a.controller.PrepareBootstrap()
	waitBootstrap(t, a.controller)
	tt := ui.NewTester(a.view, 640, 900)
	save, ok := tt.AccessByRole(ui.RoleButton, "Submit SAVE")
	if !ok || !save.Disabled {
		t.Fatal("SAVE offered without explicit overwrite permission")
	}
	clickManual(t, tt, "I authorize replacing this saved program if it already exists")
	save, ok = tt.AccessByRole(ui.RoleButton, "Submit SAVE")
	if !ok || save.Disabled {
		t.Fatal("explicit permission did not enable SAVE")
	}
	clickManual(t, tt, "Submit SAVE")
	waitBootstrap(t, a.controller)
	if !a.controller.Snapshot().SaveSubmitted || a.controller.Snapshot().BootstrapSaved {
		t.Fatal("submission must not assume the console accepted its save prompt")
	}
}

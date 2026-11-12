package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func TestExistingBootstrapRequiresDeliberateIdentityConfirmation(t *testing.T) {
	a, _ := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	a.initializeSession(sessiontest.NewFakeEnvironment().Environment())
	tt := ui.NewTester(a.view, 640, 900)
	clickManual(t, tt, "Check readiness")
	uiStage(t, a, tt, session.Ready)
	clickManual(t, tt, "Continue to route and setup")
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
	a.initializeSession(sessiontest.NewFakeEnvironment().Environment())
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

// The confirmation must name the save, host and edition the user is vouching
// for, in the catalog's order, and the recorded identity must match it.
func TestBootstrapIdentityConfirmationNamesTheConfirmedIdentity(t *testing.T) {
	a, _ := loadApp(filepath.Join(t.TempDir(), "preferences.json"))
	a.initializeSession(sessiontest.NewFakeEnvironment().Environment())
	tt := ui.NewTester(a.view, 640, 900)
	clickManual(t, tt, "Check readiness")
	uiStage(t, a, tt, session.Ready)
	clickManual(t, tt, "Continue to route and setup")
	if err := tt.Click("Use saved bootstrap"); err != nil {
		t.Fatal(err)
	}
	const want = "Confirm the console already contains NS2 for kurumuz (full), revision "
	if !slices.ContainsFunc(tt.Texts(), func(text string) bool { return strings.HasPrefix(text, want) }) {
		t.Fatalf("confirmation does not name NS2 for kurumuz (full): %q", tt.Texts())
	}
	if err := tt.Click("I confirm this exact saved bootstrap"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(a.bootstrapPath())
	if err != nil {
		t.Fatal(err)
	}
	var record session.BootstrapRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.SaveName != "NS2" || record.ProfileID != "kurumuz" || record.Edition != "full" || !record.Confirmed {
		t.Fatalf("stored identity %+v", record)
	}
}

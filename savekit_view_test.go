package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/session/sessiontest"
)

func saveKitApp(t *testing.T, f *sessiontest.FakeEnvironment, adapter *fakeToolAdapter) *app {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := &app{manifest: m, tools: adapter,
		controller: session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned",
			RetainsRPC: true, Tools: []string{"savekit-read", "savekit"}})}
	a.saveKit.open = true
	return a
}

func saveKitLease(t *testing.T, a *app, tt *ui.Tester) {
	t.Helper()
	if _, err := a.controller.Inspect(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.Ready)
	if _, err := a.controller.Start(); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
}

func TestSaveKitControlsNeedAHeldLease(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{}
	a := saveKitApp(t, f, adapter)
	tt := ui.NewTester(a.saveKitView, 720, 1400)
	if !tt.HasText("SaveKit needs a verified session with a live retained RPC lease on a SaveKit-capable host.") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"List console accounts", "List installed titles", "Scan all save archives",
		"Inspect save files", "Export verified backup", "Read save timestamps",
		"Preview save write", "Preview restore", "Back up and write save", "Back up and restore save",
		"Compare live save with backup", "Back up and delete file"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s is usable without a retained lease: %+v", label, control)
		}
	}
}

func TestSaveKitUnavailableStatesTheRecordedReason(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.DistributionFor("savekit")
	if err != nil {
		t.Fatal(err)
	}
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{reasons: map[string]string{"savekit-read": d.Reason, "savekit": d.Reason}}
	a := saveKitApp(t, f, adapter)
	tt := ui.NewTester(a.saveKitView, 720, 1400)
	saveKitLease(t, a, tt)
	if !tt.HasText(d.Reason) {
		t.Fatalf("missing recorded reason: %q", tt.Texts())
	}
	for _, label := range []string{"List console accounts", "Back up and write save",
		"Back up and restore save", "Back up and delete file"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("an unstaged SaveKit action is usable: %+v", control)
		}
	}
}

func TestSaveKitListsSelectArchiveAndExport(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{state: retained.State{SaveKit: &retained.SaveKitState{
		Users:  []string{"11111111111111111111111111111111", "22222222222222222222222222222222"},
		Titles: []string{"010012900d38a000"},
		Inventory: []savekit.Entry{
			{Archive: savekit.Archive{Space: 1, Type: 1}, TitleID: "010012900d38a000",
				UID: "11111111111111111111111111111111", Size: "10"},
			{Archive: savekit.Archive{Space: 4, Type: 3}, TitleID: "0100000000001000",
				UID: "22222222222222222222222222222222", Size: "20"},
		},
	}}}
	a := saveKitApp(t, f, adapter)
	tt := ui.NewTester(a.saveKitView, 720, 1400)
	saveKitLease(t, a, tt)
	if err := tt.Click("Scan all save archives"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	if !slices.Contains(f.Trace(), "tool.run-with savekit-read") {
		t.Fatalf("the inventory action did not reach the adapter: %v", f.Trace())
	}
	// Selecting an archive fills its title, account and archive fields.
	a.saveKit.archive = archiveLabel(adapter.state.SaveKit.Inventory[1])
	tt.Frame()
	if !tt.HasText("Selected archive: title 0100000000001000 · account 2222222222222222 · space 4 type 3 rank 0 index 0") {
		t.Fatal(tt.Texts())
	}
	backup, ok := tt.AccessByRole(ui.RoleButton, "Export verified backup")
	if !ok || backup.Disabled {
		t.Fatalf("a selected archive did not enable the verified export: %+v", backup)
	}
	if err := tt.Click("Export verified backup"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	// A verified export keeps its folder and offers to open it.
	adapter.state.SaveKit.Path = "/support/savekit-exports/0100000000001000-x"
	adapter.state.Export = adapter.state.SaveKit.Path
	tt.Frame()
	if !tt.HasText("Verified save export kept at /support/savekit-exports/0100000000001000-x") {
		t.Fatal(tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Open export folder"); !ok {
		t.Fatal("the export folder cannot be opened")
	}
	// A non-default archive selection explains why writes stay disabled.
	if !tt.HasText("Writes and deletes target the default account archive (space 1, type 1, rank 0, index 0); the selected archive differs.") {
		t.Fatal(tt.Texts())
	}
	if control, ok := tt.AccessByRole(ui.RoleButton, "Preview save write"); !ok || !control.Disabled {
		t.Fatalf("a write preview ran against a non-default archive: %+v", control)
	}
}

func TestSaveKitManualTitleEntry(t *testing.T) {
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{state: retained.State{SaveKit: &retained.SaveKitState{
		Users: []string{"11111111111111111111111111111111"},
	}}}
	a := saveKitApp(t, f, adapter)
	tt := ui.NewTester(a.saveKitView, 720, 1400)
	saveKitLease(t, a, tt)
	a.saveKit.account = "11111111111111111111111111111111"
	tt.Frame()
	inspect, ok := tt.AccessByRole(ui.RoleButton, "Inspect save files")
	if !ok || !inspect.Disabled {
		t.Fatalf("inspect ran without a title: %+v", inspect)
	}
	if err := tt.Click("Title ID (16 hexadecimal digits)"); err != nil {
		t.Fatal(err)
	}
	tt.Type("010012900d38a000")
	tt.Frame()
	inspect, ok = tt.AccessByRole(ui.RoleButton, "Inspect save files")
	if !ok || inspect.Disabled {
		t.Fatalf("a manual title did not enable inspect: %+v", inspect)
	}
	if err := tt.Click("Inspect save files"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	if !slices.Contains(f.Trace(), "tool.run-with savekit-read") {
		t.Fatalf("inspect did not reach the adapter: %v", f.Trace())
	}
}

// leasedSaveKit enters the panel with a lease, a manual target and a source
// folder ready for the gated write flow.
func leasedSaveKit(t *testing.T, files map[string]string) (*app, *fakeToolAdapter, *ui.Tester, *sessiontest.FakeEnvironment) {
	t.Helper()
	f := sessiontest.NewFakeEnvironment()
	adapter := &fakeToolAdapter{state: retained.State{SaveKit: &retained.SaveKitState{
		Users: []string{"11111111111111111111111111111111"},
	}}}
	a := saveKitApp(t, f, adapter)
	tt := ui.NewTester(a.saveKitView, 720, 1400)
	saveKitLease(t, a, tt)
	a.saveKit.account = "11111111111111111111111111111111"
	a.saveKit.titleID = "010012900d38a000"
	source := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.saveKit.sourceFolder = source
	tt.Frame()
	return a, adapter, tt, f
}

func TestSaveKitConfirmationCannotMoveToAnotherTarget(t *testing.T) {
	a, _, tt, _ := leasedSaveKit(t, map[string]string{"extra.bin": "extra\n"})
	if err := tt.Click("Preview save write"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("I confirm this exact target and file preview"); err != nil {
		t.Fatal(err)
	}
	a.saveKit.titleID = "0100000000001000"
	tt.Frame()
	write, ok := tt.AccessByRole(ui.RoleButton, "Back up and write save")
	if !ok || !write.Disabled {
		t.Fatalf("confirmation moved to another title: %+v", write)
	}
}

func TestSaveKitDeleteConfirmationCannotMoveToAnotherFile(t *testing.T) {
	a, _, tt, _ := leasedSaveKit(t, map[string]string{"extra.bin": "extra\n"})
	a.saveKit.deleteName = "first.bin"
	tt.Frame()
	if err := tt.Click("I confirm this exact target and file preview"); err != nil {
		t.Fatal(err)
	}
	a.saveKit.deleteName = "second.bin"
	tt.Frame()
	remove, ok := tt.AccessByRole(ui.RoleButton, "Back up and delete file")
	if !ok || !remove.Disabled {
		t.Fatalf("confirmation moved to another file: %+v", remove)
	}
}

func TestSaveKitWriteEditsNeedNewConsent(t *testing.T) {
	for _, change := range []string{"source", "replace"} {
		t.Run(change, func(t *testing.T) {
			a, _, tt, _ := leasedSaveKit(t, map[string]string{"extra.bin": "extra\n"})
			if err := tt.Click("Preview save write"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("I confirm this exact target and file preview"); err != nil {
				t.Fatal(err)
			}
			if change == "source" {
				a.saveKit.sourceFolder = t.TempDir()
			} else {
				a.saveKit.allowReplace = true
			}
			tt.Frame()
			write, ok := tt.AccessByRole(ui.RoleButton, "Back up and write save")
			if !ok || !write.Disabled {
				t.Fatalf("write edit kept its previous consent: %+v", write)
			}
		})
	}
}

func TestSaveKitRestoreListsFilesBeforeConsent(t *testing.T) {
	a, _, tt, _ := leasedSaveKit(t, map[string]string{"extra.bin": "extra\n"})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "save.bin"), []byte("save"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"programId":"010012900d38a000","uid":"11111111111111111111111111111111","space":1,"type":1,"rank":0,"index":0,"files":1,"bytes":4,"partial":false,"failed":[],"entries":[{"path":"/save.bin","size":4,"sha256":"%x"}]}`, sha256.Sum256([]byte("save")))
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	a.saveKit.restoreFolder = dir
	tt.Frame()
	if err := tt.Click("Preview restore"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("/save.bin  4") {
		t.Fatalf("restore hides the affected file: %v", tt.Texts())
	}
	if !tt.HasText("Target: title 010012900d38a000 · account 11111111111111111111111111111111 · space 1 type 1 rank 0 index 0") {
		t.Fatal("the target does not identify the full account")
	}
	if err := tt.Click("I confirm this exact target and file preview"); err != nil {
		t.Fatal(err)
	}
	a.saveKit.restoreFolder = t.TempDir()
	tt.Frame()
	restore, ok := tt.AccessByRole(ui.RoleButton, "Back up and restore save")
	if !ok || !restore.Disabled {
		t.Fatalf("a changed restore source kept its consent: %+v", restore)
	}
}

func TestSaveKitWriteNeedsPreviewAndConfirmation(t *testing.T) {
	a, adapter, tt, f := leasedSaveKit(t, map[string]string{"extra.bin": "extra\n"})
	write, ok := tt.AccessByRole(ui.RoleButton, "Back up and write save")
	if !ok || !write.Disabled {
		t.Fatalf("an unpreviewed write is usable: %+v", write)
	}
	if err := tt.Click("Preview save write"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("1 file(s), 6 bytes") {
		t.Fatal(tt.Texts())
	}
	if !tt.HasText("/extra.bin  6") {
		t.Fatal(tt.Texts())
	}
	if !tt.HasText("Target: title 010012900d38a000 · account 11111111111111111111111111111111 · space 1 type 1 rank 0 index 0") {
		t.Fatal(tt.Texts())
	}
	write, ok = tt.AccessByRole(ui.RoleButton, "Back up and write save")
	if !ok || !write.Disabled {
		t.Fatalf("an unconfirmed write is usable: %+v", write)
	}
	if err := tt.Click("I confirm this exact target and file preview"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	write, ok = tt.AccessByRole(ui.RoleButton, "Back up and write save")
	if !ok || write.Disabled {
		t.Fatalf("a confirmed write stayed disabled: %+v", write)
	}
	if err := tt.Click("Back up and write save"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	if !slices.Contains(f.Trace(), "tool.run-with savekit") {
		t.Fatalf("the write did not reach the adapter: %v", f.Trace())
	}
	// The confirmation and preview reset: the next write needs its own.
	if a.saveKit.confirmWrite || a.saveKit.writePreview != nil {
		t.Fatal("the confirmation survived its operation")
	}
	write, ok = tt.AccessByRole(ui.RoleButton, "Back up and write save")
	if !ok || !write.Disabled {
		t.Fatalf("a second write did not need a new confirmation: %+v", write)
	}
	// A verified outcome states the counts and keeps the backup accessible.
	adapter.state.SaveKit.Write = &retained.SaveKitWrite{Action: "write", Verified: true,
		Files: 1, Backup: "/support/savekit-exports/010012900d38a000-a"}
	tt.Frame()
	if !tt.HasText("Write verified: 1 file(s) written, 0 replaced. The pre-operation backup is kept at /support/savekit-exports/010012900d38a000-a") {
		t.Fatal(tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleStatus, "Write verified: 1 file(s) written, 0 replaced. The pre-operation backup is kept at /support/savekit-exports/010012900d38a000-a"); !ok {
		t.Fatal("the write outcome is not an accessible status")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Open backup folder"); !ok {
		t.Fatal("the recovery backup cannot be opened")
	}
}

func TestSaveKitFailedOperationBlocksAndExplains(t *testing.T) {
	a, adapter, tt, _ := leasedSaveKit(t, map[string]string{"extra.bin": "extra\n"})
	// The manager latches a failed mutation in SaveKitBlocked, independently of
	// the outcome it displays, until the lease is reset.
	adapter.state.SaveKitBlocked = "post-write verification failed"
	adapter.state.SaveKit.Write = &retained.SaveKitWrite{Action: "write",
		Failure: "post-write verification failed", Backup: "/support/savekit-exports/010012900d38a000-b"}
	tt.Frame()
	if !tt.HasText("The save operation failed. The verified backup is kept at /support/savekit-exports/010012900d38a000-b. Finish this session and restart the console before any further save operation; on a new session you can restore the backup from this panel.") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"Preview save write", "Back up and delete file", "Compare live save with backup"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s ran after a failed save operation: %+v", label, control)
		}
	}
	// Read-only inspection still works after a failed operation.
	if control, ok := tt.AccessByRole(ui.RoleButton, "Inspect save files"); !ok || control.Disabled {
		t.Fatalf("read-only inspection was blocked: %+v", control)
	}
	_ = a
}

func TestSaveKitCompareReportsMatchAndDifference(t *testing.T) {
	a, adapter, tt, f := leasedSaveKit(t, map[string]string{"extra.bin": "extra\n"})
	a.saveKit.compareFolder = "/support/savekit-exports/010012900d38a000-a"
	tt.Frame()
	if err := tt.Click("Compare live save with backup"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	if !slices.Contains(f.Trace(), "tool.run-with savekit") {
		t.Fatalf("the compare did not reach the adapter: %v", f.Trace())
	}
	adapter.state.SaveKit.Write = &retained.SaveKitWrite{Action: "compare", Verified: true,
		Files: 1, Backup: "/support/savekit-exports/010012900d38a000-c"}
	tt.Frame()
	if !tt.HasText("The live save matches the export (1 file(s)). The comparison export is kept at /support/savekit-exports/010012900d38a000-c") {
		t.Fatal(tt.Texts())
	}
	adapter.state.SaveKit.Write = &retained.SaveKitWrite{Action: "compare",
		Backup: "/support/savekit-exports/010012900d38a000-d"}
	tt.Frame()
	if !tt.HasText("The live save differs from the export. The comparison export is kept at /support/savekit-exports/010012900d38a000-d") {
		t.Fatal(tt.Texts())
	}
	// A failed compare never blocks a mutation.
	if control, ok := tt.AccessByRole(ui.RoleButton, "Preview save write"); !ok || control.Disabled {
		t.Fatalf("a failed compare blocked the write gate: %+v", control)
	}
}

func TestSaveKitDeleteNeedsTheFileNameAndConfirmation(t *testing.T) {
	a, _, tt, f := leasedSaveKit(t, map[string]string{"extra.bin": "extra\n"})
	delete, ok := tt.AccessByRole(ui.RoleButton, "Back up and delete file")
	if !ok || !delete.Disabled {
		t.Fatalf("a delete without a file name is usable: %+v", delete)
	}
	a.saveKit.deleteName = "savegame.bin"
	tt.Frame()
	if err := tt.Click("I confirm this exact target and file preview"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	delete, ok = tt.AccessByRole(ui.RoleButton, "Back up and delete file")
	if !ok || delete.Disabled {
		t.Fatalf("a confirmed delete stayed disabled: %+v", delete)
	}
	if err := tt.Click("Back up and delete file"); err != nil {
		t.Fatal(err)
	}
	uiStage(t, a, tt, session.RetainedRPC)
	if !slices.Contains(f.Trace(), "tool.run-with savekit") {
		t.Fatalf("the delete did not reach the adapter: %v", f.Trace())
	}
	// The checkbox label is shared; confirming delete must not confirm write.
	if a.saveKit.confirmWrite || a.saveKit.confirmRestore {
		t.Fatal("one confirmation covered another operation")
	}
	if a.saveKit.confirmDelete {
		t.Fatal("the delete confirmation survived its operation")
	}
}

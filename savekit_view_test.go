package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

func saveKitApp(t *testing.T, f *session.FakeEnvironment, adapter *fakeToolAdapter) *app {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := &app{manifest: m, tools: adapter,
		controller: session.New(f.Environment(), session.Profile{ID: "test", Revision: "pinned",
			RetainsRPC: true, Tools: []string{"savekit-read"}})}
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

func TestSaveKitControlsNeedAHeldLeaseAndShowNoWritePath(t *testing.T) {
	f := session.NewFakeEnvironment()
	adapter := &fakeToolAdapter{}
	a := saveKitApp(t, f, adapter)
	tt := ui.NewTester(a.saveKitView, 720, 1400)
	if !tt.HasText("Nothing in this panel writes to the console: there is no write, restore or delete control.") {
		t.Fatal(tt.Texts())
	}
	if !tt.HasText("SaveKit needs a verified session with a live retained RPC lease on a SaveKit-capable host.") {
		t.Fatal(tt.Texts())
	}
	for _, label := range []string{"List console accounts", "List installed titles", "Scan all save archives",
		"Inspect save files", "Export verified backup", "Read save timestamps"} {
		control, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok || !control.Disabled {
			t.Fatalf("%s is usable without a retained lease: %+v", label, control)
		}
	}
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleButton {
			lower := strings.ToLower(info.Name)
			for _, write := range []string{"restore", "delete", "write save", "replace"} {
				if strings.Contains(lower, write) {
					t.Fatalf("a write control is reachable: %q", info.Name)
				}
			}
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
	f := session.NewFakeEnvironment()
	adapter := &fakeToolAdapter{reasons: map[string]string{"savekit-read": d.Reason}}
	a := saveKitApp(t, f, adapter)
	tt := ui.NewTester(a.saveKitView, 720, 1400)
	saveKitLease(t, a, tt)
	if !tt.HasText(d.Reason) {
		t.Fatalf("missing recorded reason: %q", tt.Texts())
	}
	control, ok := tt.AccessByRole(ui.RoleButton, "List console accounts")
	if !ok || !control.Disabled {
		t.Fatalf("an unstaged SaveKit action is usable: %+v", control)
	}
}

func TestSaveKitListsSelectArchiveAndExport(t *testing.T) {
	f := session.NewFakeEnvironment()
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
}

func TestSaveKitManualTitleEntry(t *testing.T) {
	f := session.NewFakeEnvironment()
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

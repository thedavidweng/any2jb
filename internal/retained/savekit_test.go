package retained_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/savekit"
	"github.com/thedavidweng/any2jb/internal/session"
)

// savekitManager builds a manager whose SaveKit module is the structured
// fixture; the held-session fixture supplies the verified lease on loopback.
func savekitManager(t *testing.T, support string, runner retained.Workspaces, scenario string, staged ...string) *retained.Manager {
	t.Helper()
	node := nodePath(t)
	fixture, err := filepath.Abs("testdata/fake-tool.cjs")
	if err != nil {
		t.Fatal(err)
	}
	module, err := filepath.Abs("../savekit/testdata/fake-savekit-host.cjs")
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	options := retained.Options{
		Support: support, Node: node, Host: fixture, Manifest: m,
		Runs: runner, Staged: staged, EngineStaged: true, ToolRoot: filepath.Join(support, "tools"),
		SaveKitModule: module, SaveKitScenario: scenario,
	}
	return retained.NewProtocolManager(options, func(run session.Run, request session.ToolRequest, workspace string) *exec.Cmd {
		return exec.Command(node, fixture, options.ToolRoot, request.Tool, workspace,
			request.ConsoleIPv4, filepath.Join(support, "verified-exports", "system-code-"+run.ID), scenario)
	})
}

func savekitRequest(action string) session.ToolRequest {
	return session.ToolRequest{Tool: "savekit-read", Save: &savekit.Request{Action: action}}
}

func TestSaveKitReadRequiresAHeldLease(t *testing.T) {
	without := savekitManager(t, t.TempDir(), nil, "ok", "savekit")
	err := without.RunWith(context.Background(), testRun(), savekitRequest("accounts"), func(session.Progress) {})
	if err == nil {
		t.Fatal("a read-only save operation ran without a retained lease")
	}
}

func TestSaveKitReadListsAndExportsWithAHeldLease(t *testing.T) {
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit")
	if err := m.RunWith(context.Background(), run, savekitRequest("accounts"), func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if users := m.State().SaveKit.Users; len(users) != 2 {
		t.Fatalf("accounts: %+v", m.State().SaveKit)
	}
	if err := m.RunWith(context.Background(), run, savekitRequest("inventory"), func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	inventory := m.State().SaveKit.Inventory
	if len(inventory) != 2 {
		t.Fatalf("inventory: %+v", m.State().SaveKit)
	}
	entry := inventory[1]
	target := session.ToolRequest{Tool: "savekit-read", Save: &savekit.Request{
		Action:  "inspect",
		TitleID: entry.TitleID, UID: entry.UID, Archive: &entry.Archive,
	}}
	if err := m.RunWith(context.Background(), run, target, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.State().SaveKit.Output, "/savegame.bin") {
		t.Fatalf("inspect: %+v", m.State().SaveKit)
	}
	backup := session.ToolRequest{Tool: "savekit-read", Save: &savekit.Request{
		Action:  "backup",
		TitleID: "010012900d38a000", UID: "11111111111111111111111111111111",
	}}
	if err := m.RunWith(context.Background(), run, backup, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	export := m.State().SaveKit.Path
	if !strings.HasPrefix(export, filepath.Join(support, "savekit-exports")+string(filepath.Separator)) {
		t.Fatalf("the export left its chosen folder: %q", export)
	}
	if err := savekit.VerifyExport(export); err != nil {
		t.Fatal(err)
	}
}

func TestSaveKitReadSharesTheSingleOwner(t *testing.T) {
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit", "mintbrew", "homebrew-runtime")
	request := session.ToolRequest{Tool: "mintbrew", ConsoleIPv4: "192.0.2.7"}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	err := m.RunWith(context.Background(), run, savekitRequest("accounts"), func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "mintbrew") {
		t.Fatalf("a save operation ran while another tool owned the lease: %v", err)
	}
	if err := m.Stop(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	slow := savekitManager(t, support, runner, "slow", "savekit", "mintbrew", "homebrew-runtime")
	done := make(chan error, 1)
	go func() {
		done <- slow.RunWith(context.Background(), run, savekitRequest("accounts"), func(session.Progress) {})
	}()
	for slow.State().Tool != "savekit-read" {
		select {
		case err := <-done:
			t.Fatalf("save operation ended early: %v", err)
		default:
		}
	}
	err = slow.RunWith(context.Background(), run, request, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "savekit-read") {
		t.Fatalf("a tool ran while a save operation owned the lease: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSaveKitReadShowsTheRecordedReasonWhenUnstaged(t *testing.T) {
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok")
	err := m.RunWith(context.Background(), run, savekitRequest("accounts"), func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "SaveKit") {
		t.Fatalf("an unstaged save operation did not state the recorded reason: %v", err)
	}
}

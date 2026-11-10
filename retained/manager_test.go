package retained_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
)

func testRun() session.Run {
	return session.Run{ID: "fixture-run", LANIPv4: "192.0.2.1",
		Profile: session.Profile{ID: "kurumuz", StartPort: 39380, StartArgumentCount: 5, RetainsRPC: true,
			Tools: []string{"rpc-status", "system-code-dump", "ouncecake-webdav", "ouncecake-ftp", "sd-browser"}}}
}

// heldSession runs the structured engine fixture until its retained lease is
// verified. No console or exploit is involved.
func heldSession(t *testing.T) (*engine.HostRunner, session.Run, string) {
	t.Helper()
	node := nodePath(t)
	fixture, err := filepath.Abs("../engine/testdata/fake-engine.cjs")
	if err != nil {
		t.Fatal(err)
	}
	support := t.TempDir()
	runner := engine.NewProtocolRunner(support, func(run session.Run, workspace string) *exec.Cmd {
		return exec.Command(node, fixture, "held-verified", run.ID, workspace)
	})
	run := testRun()
	t.Cleanup(func() { runner.Cleanup(context.Background(), run) })
	host, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runner.Run(context.Background(), run, host, func(session.Progress) {})
	if err != nil || !outcome.Valid(true) {
		t.Fatalf("held fixture: %+v %v", outcome, err)
	}
	return runner, run, support
}

func nodePath(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the structured tool fixture")
	}
	return node
}

func manager(t *testing.T, support string, runner retained.Workspaces, scenario string, staged ...string) *retained.Manager {
	t.Helper()
	node := nodePath(t)
	fixture, err := filepath.Abs("testdata/fake-tool.cjs")
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	options := retained.Options{Support: support, Node: node, Host: fixture, Manifest: m,
		Runs: runner, Staged: staged, EngineStaged: true, ToolRoot: filepath.Join(support, "tools")}
	return retained.NewProtocolManager(options, func(run session.Run, request session.ToolRequest, workspace string) *exec.Cmd {
		return exec.Command(node, fixture, options.ToolRoot, request.Tool, workspace,
			request.ConsoleIPv4, filepath.Join(support, "verified-exports", "system-code-"+run.ID), scenario)
	})
}

func TestUnavailableToolsStateTheRecordedReason(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	unstaged := retained.New(retained.Options{Support: t.TempDir(), Node: "node", Host: "toolhost.cjs", Manifest: m})
	for tool, want := range map[string]string{
		"ouncecake-webdav": "ouncecake", "ouncecake-ftp": "ouncecake", "sd-browser": "sd-browser",
	} {
		d, err := m.DistributionFor(want)
		if err != nil {
			t.Fatal(err)
		}
		if reason := unstaged.Unavailable(tool, "kurumuz"); reason != d.Reason {
			t.Fatalf("%s must show the recorded reason, got %q", tool, reason)
		}
	}
	host, err := m.DistributionFor("kurumuz")
	if err != nil {
		t.Fatal(err)
	}
	if reason := unstaged.Unavailable("system-code-dump", "kurumuz"); reason != host.Reason {
		t.Fatalf("the capture must show the host's reason, got %q", reason)
	}
	if reason := unstaged.Unavailable("savekit", "kurumuz"); reason == "" {
		t.Fatal("a write tool was offered by the read-only adapter")
	}
	if reason := unstaged.Unavailable("rpc-status", "kurumuz"); reason != "" {
		t.Fatalf("status needs no staged component: %q", reason)
	}
	packaged := retained.New(retained.Options{Support: t.TempDir(), Manifest: m})
	if reason := packaged.Unavailable("rpc-status", "kurumuz"); reason == "" {
		t.Fatal("tools ran without a staged tool host")
	}
}

func TestToolRequiresAnOwnedVerifiedLease(t *testing.T) {
	support := t.TempDir()
	without := manager(t, support, nil, "ok")
	if err := without.Run(context.Background(), testRun(), "rpc-status", func(session.Progress) {}); err == nil {
		t.Fatal("a tool ran without any retained lease")
	}
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "ok")
	if err := m.Run(context.Background(), run, "rpc-status", func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Finish(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background(), run, "rpc-status", func(session.Progress) {}); err == nil {
		t.Fatal("a tool ran after the retained lease closed")
	}
}

func TestOneOwnerAtATime(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "ok", "sd-browser", "ouncecake")
	request := session.ToolRequest{Tool: "sd-browser", ConsoleIPv4: "192.0.2.7"}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if endpoint := m.State().Endpoint; endpoint != "ftp://192.0.2.7:2122/sd/" {
		t.Fatalf("unexpected endpoint %q", endpoint)
	}
	err := m.RunWith(context.Background(), run, session.ToolRequest{Tool: "ouncecake-webdav",
		ConsoleIPv4: "192.0.2.7"}, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "sd-browser") {
		t.Fatalf("a second owner was not refused with the current owner: %v", err)
	}
	if err := m.Stop(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if state := m.State(); state.Endpoint != "" || state.Tool != "" {
		t.Fatalf("stopped tool kept its endpoint: %+v", state)
	}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func TestStopDrainsProgressAfterRunning(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "progress-flood", "sd-browser")
	err := m.RunWith(context.Background(), run, session.ToolRequest{
		Tool: "sd-browser", ConsoleIPv4: "192.0.2.7"}, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.Stop(context.Background(), run) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("progress after running prevented a clean stop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop blocked behind unread progress events")
	}
}

func TestCancellationDrainsProgressBeforeRunning(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "pre-result-flood", "sd-browser")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- m.RunWith(ctx, run, session.ToolRequest{Tool: "sd-browser", ConsoleIPv4: "192.0.2.7"},
			func(session.Progress) { cancel() })
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled startup reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled startup blocked behind unread progress")
	}
}

func TestExportsStayInsideTheirStatedBoundary(t *testing.T) {
	runner, run, support := heldSession(t)
	exposed := manager(t, support, runner, "exposed", "sd-browser")
	err := exposed.RunWith(context.Background(), run, session.ToolRequest{Tool: "sd-browser",
		ConsoleIPv4: "192.0.2.7"}, func(session.Progress) {})
	if err == nil {
		t.Fatal("an endpoint outside the stated boundary was accepted")
	}
	outside := manager(t, support, runner, "outside", "sd-browser")
	if err := outside.Run(context.Background(), run, "system-code-dump", func(session.Progress) {}); err == nil {
		t.Fatal("a capture written into the private workspace was accepted")
	}
	ok := manager(t, support, runner, "ok")
	if err := ok.Run(context.Background(), run, "system-code-dump", func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	export := ok.State().Export
	if !strings.HasPrefix(export, filepath.Join(support, "verified-exports")) {
		t.Fatalf("capture kept inside a temporary workspace: %q", export)
	}
	if _, err := os.Stat(filepath.Join(export, "firmware-dump.json")); err != nil {
		t.Fatal(err)
	}
}

func TestOunceCakeExportsOnlyAnOfferedGame(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "ok", "ouncecake")
	request := session.ToolRequest{Tool: "ouncecake-ftp", ConsoleIPv4: "192.0.2.7"}
	done := make(chan error, 1)
	go func() { done <- m.RunWith(context.Background(), run, request, func(session.Progress) {}) }()
	for m.State().Games == nil {
		if len(done) > 0 {
			t.Fatal(<-done)
		}
	}
	if err := m.Select("ffffffffffffffff"); err == nil {
		t.Fatal("a game outside the offered catalog was selected")
	}
	if err := m.Select("0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if endpoint := m.State().Endpoint; endpoint != "ftp://127.0.0.1:2222/" {
		t.Fatalf("the FTP export left this Mac: %q", endpoint)
	}
	if err := m.Stop(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func TestAttachRevalidatesAndDetachLeavesTheHostRunning(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, nil, "ok")
	found, err := m.Discover(context.Background())
	if err != nil || len(found) != 1 || found[0].ID == "" {
		t.Fatalf("discovery did not list the verified session: %v %v", found, err)
	}
	if _, err := m.Attach(context.Background(), run, "run-does-not-exist"); err == nil {
		t.Fatal("attached to an unverified session")
	}
	outcome, err := m.Attach(context.Background(), run, found[0].ID)
	if err != nil || !outcome.Valid(true) {
		t.Fatalf("attach refused a verified session: %+v %v", outcome, err)
	}
	// An attached manager owns no run of its own, yet tools work on the lease.
	if err := m.Run(context.Background(), run, "rpc-status", func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if err := m.Detach(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.VerifyHeld(context.Background(), mustWorkspace(t, runner, run)); err != nil {
		t.Fatalf("detach stopped the original host: %v", err)
	}
	if err := m.Run(context.Background(), run, "rpc-status", func(session.Progress) {}); err == nil {
		t.Fatal("a tool ran after detaching")
	}
}

func TestLostAttachmentIsReportedNotRetried(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, nil, "ok")
	found, err := m.Discover(context.Background())
	if err != nil || len(found) == 0 {
		t.Fatalf("discovery: %v %v", found, err)
	}
	if _, err := m.Attach(context.Background(), run, found[0].ID); err != nil {
		t.Fatal(err)
	}
	lost := make(chan error, 1)
	go func() { lost <- m.Watch(context.Background(), run) }()
	if err := runner.Cleanup(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := <-lost; err == nil {
		t.Fatal("a lost attachment was reported as healthy")
	}
	if !m.State().Lost {
		t.Fatal("the lost lease was not recorded")
	}
	if err := m.Run(context.Background(), run, "rpc-status", func(session.Progress) {}); err == nil {
		t.Fatal("a tool was retried on a lost session")
	}
}

func mustWorkspace(t *testing.T, runner *engine.HostRunner, run session.Run) string {
	t.Helper()
	workspace, err := runner.Workspace(run)
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

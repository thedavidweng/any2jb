package retained_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
)

// homebrewStaged lists every component the structured fixture pretends to have
// verified; distribution policy still gates each launcher by its own record.
var homebrewStaged = []string{"mintbrew", "mintbrew-native", "homebrew-runtime",
	"izlauncher", "izlauncher-blitbun", "izlauncher-shell", "izlauncher-vacootube"}

func TestHomebrewToolsStateTheirRecordedReason(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	unstaged := retained.New(retained.Options{Support: t.TempDir(), Node: "node", Host: "toolhost.cjs", Manifest: m})
	for tool, want := range map[string]string{
		"mintbrew": "mintbrew", "izlauncher": "izlauncher",
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
	staged := retained.New(retained.Options{Support: t.TempDir(), Node: "node", Host: "toolhost.cjs",
		Manifest: m, Staged: homebrewStaged})
	if reason := staged.Unavailable("native-hblauncher", "kurumuz"); reason != host.Reason {
		t.Fatalf("the native launcher needs a developer engine stage, got %q", reason)
	}
	// A partially staged IZlauncher names the first missing component.
	partial := retained.New(retained.Options{Support: t.TempDir(), Node: "node", Host: "toolhost.cjs",
		Manifest: m, Staged: []string{"izlauncher"}})
	d, err := m.DistributionFor("izlauncher-blitbun")
	if err != nil {
		t.Fatal(err)
	}
	if reason := partial.Unavailable("izlauncher", "kurumuz"); reason != d.Reason {
		t.Fatalf("a missing IZlauncher module must name its component, got %q", reason)
	}
}

func TestHomebrewLaunchReportsUnknownProfileBeforeStartingChild(t *testing.T) {
	runner, run, support := heldSession(t)
	mf, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	host, err := filepath.Abs("testdata/fake-tool.cjs")
	if err != nil {
		t.Fatal(err)
	}
	m := retained.New(retained.Options{Support: support, Runs: runner, Manifest: mf,
		Node: nodePath(t), Host: host, Staged: homebrewStaged})
	defer m.Stop(context.Background(), run)
	run.Profile.ID = "missing-profile"
	err = m.Run(context.Background(), run, "mintbrew", func(session.Progress) {
		t.Error("a child started despite an invalid profile")
	})
	if err == nil || !strings.Contains(err.Error(), "missing-profile") {
		t.Fatalf("manifest profile failure was hidden: %v", err)
	}
}

func TestHomebrewLaunchRefusesASecondOwner(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "ok", homebrewStaged...)
	request := session.ToolRequest{Tool: "mintbrew", ConsoleIPv4: "192.0.2.7"}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	err := m.RunWith(context.Background(), run, session.ToolRequest{Tool: "izlauncher",
		ConsoleIPv4: "192.0.2.7"}, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "mintbrew") {
		t.Fatalf("a second homebrew launch was not refused with the current owner: %v", err)
	}
	if err := m.Stop(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	// A proven cleanup removes the attempt marker and releases the workspace.
	if _, err := os.Stat(filepath.Join(mustWorkspace(t, runner, run), ".mac2jb-hblauncher-attempt.json")); !os.IsNotExist(err) {
		t.Fatalf("a proven cleanup kept the attempt marker: %v", err)
	}
	if err := m.RunWith(context.Background(), run, session.ToolRequest{Tool: "izlauncher",
		ConsoleIPv4: "192.0.2.7"}, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func TestUnprovenHomebrewCleanupBlocksReentry(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "hb-unproven", homebrewStaged...)
	request := session.ToolRequest{Tool: "mintbrew", ConsoleIPv4: "192.0.2.7"}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background(), run); err == nil {
		t.Fatal("an unproven cleanup was accepted")
	}
	if !m.State().CleanupBlocked {
		t.Fatal("an unproven cleanup did not block the workspace")
	}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err == nil ||
		!strings.Contains(err.Error(), "restart") {
		t.Fatalf("re-entry after an unproven cleanup must advise a console restart: %v", err)
	}
	// Read-only status does not own homebrew state and still runs.
	if err := m.Run(context.Background(), run, "rpc-status", func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
}

func TestHomebrewStopThenSwitch(t *testing.T) {
	for _, scenario := range []string{"ok", "progress-flood"} {
		t.Run(scenario, func(t *testing.T) {
			runner, run, support := heldSession(t)
			m := manager(t, support, runner, scenario, homebrewStaged...)
			for _, tool := range []string{"mintbrew", "native-hblauncher"} {
				if err := m.Run(context.Background(), run, tool, func(session.Progress) {}); err != nil {
					t.Fatalf("launch %s after verified cleanup: %v", tool, err)
				}
				done := make(chan error, 1)
				go func() { done <- m.Stop(context.Background(), run) }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("stop %s: %v", tool, err)
					}
				case <-time.After(3 * time.Second):
					t.Fatalf("stop %s blocked behind progress events", tool)
				}
				if state := m.State(); state.CleanupBlocked || state.Tool != "" {
					t.Fatalf("verified cleanup did not release %s: %+v", tool, state)
				}
			}
		})
	}
}

func TestRetrySafeHomebrewFailureKeepsReentryOpen(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "hb-fail-retry", homebrewStaged...)
	request := session.ToolRequest{Tool: "mintbrew", ConsoleIPv4: "192.0.2.7"}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err == nil {
		t.Fatal("a failing launch reported success")
	}
	if m.State().CleanupBlocked {
		t.Fatal("a retry-safe failure blocked re-entry")
	}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err == nil {
		t.Fatal("the retried failing launch reported success")
	}
}

func TestBlockedHomebrewFailureRequiresAConsoleRestart(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "hb-fail-blocked", homebrewStaged...)
	request := session.ToolRequest{Tool: "mintbrew", ConsoleIPv4: "192.0.2.7"}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err == nil {
		t.Fatal("a failing launch reported success")
	}
	if !m.State().CleanupBlocked {
		t.Fatal("a blocked failure left re-entry open")
	}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err == nil {
		t.Fatal("a blocked workspace accepted another homebrew launch")
	}
}

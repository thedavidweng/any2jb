package retained_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
)

func TestModloaderAndCustomHomebrewStateTheirRecordedReason(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	unstaged := retained.New(retained.Options{Support: t.TempDir(), Node: "node", Host: "toolhost.cjs", Manifest: m})
	for tool, component := range map[string]string{"modloader": "modloader", "custom-homebrew": "mintbrew"} {
		d, err := m.DistributionFor(component)
		if err != nil {
			t.Fatal(err)
		}
		if reason := unstaged.Unavailable(tool, "kurumuz"); reason != d.Reason {
			t.Fatalf("%s must show the recorded reason, got %q", tool, reason)
		}
	}
}

func TestModloaderCompletionLocksToolsUntilReboot(t *testing.T) {
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", append([]string{"modloader", "savekit"}, homebrewStaged...)...)
	request := session.ToolRequest{Tool: "modloader", ModsFolder: t.TempDir(), CodeConsent: true}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if state := m.State(); !state.ModloaderInstalled || state.Tool != "" {
		t.Fatalf("a verified modloader handoff was not recorded: %+v", state)
	}
	for _, tool := range []string{"rpc-status", "mintbrew", "modloader", "savekit", "savekit-read"} {
		err := m.RunWith(context.Background(), run, session.ToolRequest{Tool: tool, CodeConsent: true}, func(session.Progress) {})
		if err == nil || !strings.Contains(err.Error(), "restart") {
			t.Fatalf("%s ran while the modloader overlay is active: %v", tool, err)
		}
	}
}

func TestModloaderRefusesMissingConsentAtRetainedBoundary(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "ok", "modloader")
	err := m.RunWith(context.Background(), run, session.ToolRequest{Tool: "modloader", ModsFolder: t.TempDir()}, func(session.Progress) {
		t.Error("unconfirmed mods started a tool process")
	})
	if err == nil || !strings.Contains(err.Error(), "Confirm") {
		t.Fatalf("missing consent was not refused: %v", err)
	}
	if state := m.State(); state.ModloaderInstalled || state.Tool != "" {
		t.Fatalf("unconfirmed mods changed state: %+v", state)
	}
}

func TestRetainedRunnerRefusesUnconfirmedUserHomebrew(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "ok", homebrewStaged...)
	for _, request := range []session.ToolRequest{
		{Tool: "custom-homebrew", CustomHomebrewFolder: t.TempDir()},
		{Tool: "native-hblauncher", AppFolder: t.TempDir()},
	} {
		err := m.RunWith(context.Background(), run, request, func(session.Progress) {
			t.Error("unconfirmed homebrew started a tool process")
		})
		if err == nil || !strings.Contains(err.Error(), "Confirm") {
			t.Fatalf("missing consent was not refused: %v", err)
		}
	}
}

func TestModloaderFailureBeforeTransferKeepsTheLease(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "mod-fail-retry", "modloader")
	request := session.ToolRequest{Tool: "modloader", ModsFolder: t.TempDir(), CodeConsent: true}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err == nil {
		t.Fatal("a refused mod folder reported success")
	}
	if state := m.State(); state.ModloaderInstalled || state.CleanupBlocked {
		t.Fatalf("a retry-safe refusal locked the session: %+v", state)
	}
	if err := m.Run(context.Background(), run, "rpc-status", func(session.Progress) {}); err != nil {
		t.Fatalf("the retained lease did not survive a refused folder: %v", err)
	}
}

func TestUnverifiedModloaderHandoffRequiresARestart(t *testing.T) {
	runner, run, support := heldSession(t)
	m := manager(t, support, runner, "mod-fail-blocked", append([]string{"modloader"}, homebrewStaged...)...)
	request := session.ToolRequest{Tool: "modloader", ModsFolder: t.TempDir(), CodeConsent: true}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err == nil {
		t.Fatal("an unverified handoff reported success")
	}
	if !m.State().CleanupBlocked {
		t.Fatal("an unverified handoff left the console usable")
	}
	for _, tool := range []string{"modloader", "mintbrew"} {
		if err := m.RunWith(context.Background(), run, session.ToolRequest{Tool: tool}, func(session.Progress) {}); err == nil {
			t.Fatalf("%s ran after an unverified modloader handoff", tool)
		}
	}
}

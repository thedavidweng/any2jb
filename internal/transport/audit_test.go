package transport_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/diagnostics"
	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/transport"
)

func TestUSBLogContainsOnlyObservedCountersAndSurvivesSetupCleanup(t *testing.T) {
	const text = "PRINT \"DO_NOT_LOG_ME\"\n"
	sum := sha256.Sum256([]byte(text))
	support := t.TempDir()
	options := transport.DarwinOptions{Runtime: engine.Options{Support: support}, Controller: "usb-drd0", Timing: transport.DefaultTiming(),
		Bootstrap: func(session.Run) (string, error) { return text, nil }, Confirm: func(context.Context, session.Run, transport.ConsoleStep) error { return nil }}
	options.Timing.GapMS = 1
	d := transport.NewHelperDarwin(options, helperFactory(t, filepath.Join(t.TempDir(), "reports")))
	run := session.Run{ID: "usb-audit", Edition: "full", Profile: session.Profile{SaveName: "NS2", BootstrapSHA256: hex.EncodeToString(sum[:])}}
	if err := d.Send(context.Background(), run, session.Bootstrap, session.PreparedHost{}, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	store := diagnostics.Store{Support: support}
	logs, workspace, err := store.Refresh(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(workspace, "usb.log")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("USB log is not private")
	}
	if !strings.Contains(logs.USB, "enumeration_ready") || !strings.Contains(logs.USB, "typing_line") || !strings.Contains(logs.USB, "neutral_release_confirmed") {
		t.Fatalf("missing real helper observations: %q", logs.USB)
	}
	if strings.Contains(logs.USB, "DO_NOT_LOG_ME") || strings.Contains(logs.USB, "PRINT") {
		t.Fatal("bootstrap text leaked to diagnostics")
	}
	if err := d.Send(context.Background(), run, session.Save, session.PreparedHost{}, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	logs, again, err := store.Refresh(run.ID)
	if err != nil || again != workspace || strings.Count(logs.USB, "typing_line") != 1 || !strings.Contains(logs.USB, "command_complete") {
		t.Fatal("setup steps did not share one append-only USB log")
	}
	if err := d.Release(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatal("input workspace was not removed after cleanup")
	}
	retained, err := store.Read(run.ID)
	if err != nil || retained.USB != logs.USB {
		t.Fatal("USB observations did not survive teardown")
	}
	if interrupted, err := store.Interrupted(); err != nil || interrupted {
		t.Fatal("a clean USB lease was treated as an interrupted run")
	}
}

func TestLaunchWritesUSBLogInTheMatchingHostWorkspaceWithoutLoggingPairing(t *testing.T) {
	support := t.TempDir()
	workspace, err := engine.NewWorkspace(support)
	if err != nil {
		t.Fatal(err)
	}
	const command = "V=START(39380,192,0,2,1,123,456,789,101)"
	run := session.Run{ID: "host-usb", Edition: "full", LANIPv4: "192.0.2.1", Profile: session.Profile{SaveName: "NS2", StartPort: 39380, StartArgumentCount: 9}}
	if err := os.WriteFile(filepath.Join(workspace, "diagnostics-id"), []byte(run.ID), 0600); err != nil {
		t.Fatal(err)
	}
	options := transport.DarwinOptions{Runtime: engine.Options{Support: support}, Controller: "usb-drd0", Timing: transport.DefaultTiming(),
		Confirm: func(context.Context, session.Run, transport.ConsoleStep) error { return nil }}
	options.Timing.GapMS = 1
	options.Timing.LoadMS = 0
	d := transport.NewHelperDarwin(options, helperFactory(t, filepath.Join(t.TempDir(), "reports")))
	host := session.PreparedHost{Listening: true, PairingValid: true, Opaque: engine.PairingDetails{RunID: run.ID, Workspace: workspace, Command: command}}
	if err := d.Send(context.Background(), run, session.Launch, host, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	logs, path, err := (diagnostics.Store{Support: support}).Refresh(run.ID)
	if err != nil || path != workspace || !strings.Contains(logs.USB, "command_complete") {
		t.Fatal("launch did not write the current host's USB log")
	}
	if strings.Contains(logs.USB, "START(") || strings.Contains(logs.USB, "192.0.2.1") || strings.Contains(logs.USB, "123,456") {
		t.Fatal("pairing leaked to USB diagnostics")
	}
	if err := d.Release(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatal("input cleanup removed the host-owned workspace")
	}
}

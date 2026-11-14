package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/diagnostics"
	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/session"
)

func TestRunnerRetainsRedactedEvidenceAndIntentionalBackups(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("fixture requires Node")
	}
	fixture, err := filepath.Abs("testdata/fake-engine.cjs")
	if err != nil {
		t.Fatal(err)
	}
	support := t.TempDir()
	run := testRun()
	runner := engine.NewProtocolRunner(support, func(run session.Run, workspace string) *exec.Cmd {
		return exec.Command(node, fixture, "ready-wait", run.ID, workspace)
	})
	host, err := runner.Prepare(context.Background(), run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Cleanup(context.Background(), run)
	pairing, err := engine.Pairing(host)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(pairing.Workspace, "backups", "save.bin")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("private save"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pairing.Workspace, "run.log"), []byte("pipeline failed\npairingToken=private-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runner.Cleanup(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(support, "past-runs", filepath.Base(pairing.Workspace), "backups", "save.bin"))
	if err != nil || string(data) != "private save" {
		t.Fatal(string(data), err)
	}
	logs, err := (diagnostics.Store{Support: support}).Read(run.ID)
	if err != nil || !strings.Contains(logs.Pipeline, "pipeline failed") || strings.Contains(logs.Pipeline, "private-token") {
		t.Fatal(logs, err)
	}
}

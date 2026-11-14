package diagnostics

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStaleLeaseReleaseRequiresDeadMatchingOwner(t *testing.T) {
	s := Store{Support: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(s.Support, "runs", "run-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Support, "session.lock"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordOwner("old"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseInterrupted(context.Background()); err == nil {
		t.Fatal("released a live owner")
	}
	cmd := exec.Command("/usr/bin/true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	owner, _ := json.Marshal(Owner{RunID: "wrong", PID: cmd.Process.Pid})
	if err := os.WriteFile(filepath.Join(s.Support, "session-owner.json"), owner, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseInterrupted(context.Background()); err == nil {
		t.Fatal("released mismatching lease")
	}
	owner, _ = json.Marshal(Owner{RunID: "old", PID: cmd.Process.Pid})
	if err := os.WriteFile(filepath.Join(s.Support, "session-owner.json"), owner, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Support, "session.lock")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if interrupted, err := s.Interrupted(); err != nil || interrupted {
		t.Fatal(interrupted, err)
	}
	if _, err := os.Stat(filepath.Join(s.Support, "past-runs", "run-old")); err != nil {
		t.Fatal(err)
	}
}

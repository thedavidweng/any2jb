package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanedOrphanStillRequiresConsoleRestartAndRetainsLogs(t *testing.T) {
	s := Store{Support: t.TempDir()}
	past := filepath.Join(s.Support, "past-runs", "run-old")
	if err := os.MkdirAll(past, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"session-interrupted": "old", "past-runs/run-old/diagnostics-id": "old", "past-runs/run-old/run.log": "pipeline failed"} {
		if err := os.WriteFile(filepath.Join(s.Support, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if interrupted, err := s.Interrupted(); err != nil || !interrupted {
		t.Fatal(interrupted, err)
	}
	logs, folder, err := s.Refresh("")
	if err != nil || logs.Pipeline != "pipeline failed" || folder != past {
		t.Fatal(logs, folder, err)
	}
	if err := s.ReleaseInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if interrupted, err := s.Interrupted(); err != nil || interrupted {
		t.Fatal(interrupted, err)
	}
}

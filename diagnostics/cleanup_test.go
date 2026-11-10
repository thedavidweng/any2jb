package diagnostics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupPreviewsOnlyTemporaryFilesAndPreservesRetainedData(t *testing.T) {
	s := Store{Support: t.TempDir()}
	for _, name := range []string{"runs/run-old/run.log", "runs/run-old/connect.txt", "runs/run-old/switch2/acquisition.log", "runs/run-old/backups/save.bin", "runs/run-old/savekit-exports/export.bin", "runs/run-old/verified-exports/game.bin", "runtime/node", "support-bundles/report.zip"} {
		p := filepath.Join(s.Support, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := s.PreviewCleanup()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 {
		t.Fatal(plan)
	}
	if err := s.Cleanup(plan); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runs/run-old/backups/save.bin", "runs/run-old/savekit-exports/export.bin", "runs/run-old/verified-exports/game.bin", "runtime/node", "support-bundles/report.zip"} {
		if _, err := os.Stat(filepath.Join(s.Support, name)); err != nil {
			t.Fatal(name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.Support, "session.lock"), []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewCleanup(); err == nil {
		t.Fatal("active/stale lease must block cleanup")
	}
	if err := s.Cleanup(plan); err == nil {
		t.Fatal("lease created after preview must block cleanup")
	}
}

func TestInterruptedArtifactsAreNotAdopted(t *testing.T) {
	s := Store{Support: t.TempDir()}
	interrupted, err := s.Interrupted()
	if err != nil || interrupted {
		t.Fatal(interrupted, err)
	}
	if err := os.MkdirAll(filepath.Join(s.Support, "runs", "run-old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Support, "runs", "run-old", "connection.json"), []byte(`{"verified":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	interrupted, err = s.Interrupted()
	if err != nil || !interrupted {
		t.Fatal(interrupted, err)
	}
}

package engine_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/session"
)

func TestPinnedDeveloperHostListening(t *testing.T) {
	source, assets := os.Getenv("ANY2JB_REFERENCE_ROOT"), os.Getenv("ANY2JB_RUNTIME_ASSETS")
	if source == "" || assets == "" {
		t.Skip("set ANY2JB_REFERENCE_ROOT and ANY2JB_RUNTIME_ASSETS for verified developer Node/engine listening check")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	support := t.TempDir()
	t.Cleanup(func() {
		filepath.WalkDir(support, func(name string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				return os.Chmod(name, 0o700)
			}
			return err
		})
	})
	options := engine.Options{Assets: assets, Support: support, Version: "listening-fixture", Manifest: m,
		DeveloperSourceRoot: source, DeveloperProfileID: "pepsipu"}
	profile, err := m.Profile("pepsipu")
	if err != nil {
		t.Fatal(err)
	}
	run := session.Run{ID: "developer-listening-check", Profile: profile.SessionProfile(), LANIPv4: "127.0.0.1"}
	runner := engine.NewHostRunner(options)
	defer runner.Cleanup(context.Background(), run)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	host, err := runner.Prepare(ctx, run, func(session.Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	details, err := engine.Pairing(host)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(details.Workspace) != filepath.Join(support, "runs") {
		t.Fatal("workspace is not owned")
	}
	stage := filepath.Join(support, "runtime/developer", options.Version, profile.ID)
	if _, err := os.Stat(filepath.Join(stage, profile.SourceDirectory, "runs")); !os.IsNotExist(err) {
		t.Fatal("upstream source tree was used for run output")
	}
	// No console connects. This proves listening/START and cancellation only.
	if err := runner.Cleanup(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

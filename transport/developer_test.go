package transport_test

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

func TestPinnedDeveloperHelpersAreIntegrityCheckedWithoutUSB(t *testing.T) {
	source, assets := os.Getenv("ANY2JB_REFERENCE_ROOT"), os.Getenv("ANY2JB_RUNTIME_ASSETS")
	if source == "" || assets == "" {
		t.Skip("explicit pinned source and runtime assets required; no USB operations")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("native helper build requires Darwin arm64")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	support := t.TempDir()
	t.Cleanup(func() {
		if err := filepath.WalkDir(support, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return os.Chmod(name, 0700)
			}
			return nil
		}); err != nil {
			t.Error(err)
		}
	})
	h, err := transport.BuildDeveloperHelpers(context.Background(), source, support, m, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	path, digest, err := h.CompositeHelper()
	if err != nil || !filepath.IsAbs(path) || len(digest) != 64 {
		t.Fatal("shared composite helper provenance missing", err)
	}
	p, err := m.Profile(m.Default)
	if err != nil {
		t.Fatal(err)
	}
	options := transport.DarwinOptions{Helpers: h, Controller: "usb-drd0", Timing: transport.DefaultTiming(),
		Runtime: engine.Options{Assets: assets, Support: support, Version: "test", Manifest: m, DeveloperSourceRoot: source, DeveloperProfileID: p.ID}}
	d := transport.NewDarwin(options)
	run := session.Run{ID: "check-only", Profile: p.SessionProfile(), Edition: "full"}
	if err := d.Check(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	run.Edition = "unknown"
	if err := d.Check(context.Background(), run); err == nil {
		t.Fatal("unrecognized edition authorized")
	}
	run.Edition = "demo"
	if p.SupportsDemo {
		if err := d.Check(context.Background(), run); err != nil {
			t.Fatal(err)
		}
	}
	run.Edition = "full"
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Check(context.Background(), run); err == nil {
		t.Fatal("removed helpers remained authorized")
	}
	if _, _, err := h.CompositeHelper(); err == nil {
		t.Fatal("removed composite helper remained authorized")
	}
	// This test never calls Send/OpenKeyboard. Compiling and integrity checks
	// cannot initialize a gadget or send keys.
}

package engine_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/engine"
	"github.com/thedavidweng/any2jb/internal/manifest"
)

func TestBundledNodeRuntime(t *testing.T) {
	assets := os.Getenv("ANY2JB_RUNTIME_ASSETS")
	if assets == "" {
		t.Skip("set ANY2JB_RUNTIME_ASSETS to prepared resources")
	}
	o := fixture(t)
	o.Assets = assets
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	o.Manifest = m
	root, err := engine.Stage(o)
	if err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(root, "runtime/node")
	output, err := exec.Command(node, "--version").CombinedOutput()
	if err != nil || string(output) != "v24.22.1\n" {
		t.Fatalf("bundled Node version: %q %v", output, err)
	}
	output, err = exec.Command(node, filepath.Join(root, "runtime/bridge.cjs"), filepath.Join(root, "profiles.json"), "pepsipu").CombinedOutput()
	if err != nil || string(output) == "" {
		t.Fatalf("bundled profile rejected: %q %v", output, err)
	}
	output, err = exec.Command(node, filepath.Join(root, "runtime/bridge.cjs"), filepath.Join(root, "profiles.json"), "izmysh").CombinedOutput()
	if err == nil {
		t.Fatal("blocked profile accepted")
	}
	if string(output) == "" {
		t.Fatal("bridge did not report blocked profile")
	}
}

func TestCorruptCachedNodeNeverExtracted(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	cache, assets := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(cache, filepath.Base(m.Runtime.NodeArchive)), []byte("not Node"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := engine.PrepareNode(cache, assets, m); err == nil {
		t.Fatal("corrupt archive accepted")
	}
	entries, err := os.ReadDir(assets)
	if err != nil || len(entries) != 0 {
		t.Fatal("corrupt archive extracted")
	}
}

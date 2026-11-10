package engine_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/engine"
	"github.com/thedavidweng/any2jb/manifest"
)

func fixture(t *testing.T) engine.Options {
	t.Helper()
	root := t.TempDir()
	m := &manifest.Manifest{Runtime: manifest.Runtime{
		Platform: "darwin-arm64", NodeVersion: "24.22.1",
		Node: "node", Bridge: "bridge", Helpers: []string{"sender", "commands", "keyboard", "civswitch-hid"},
	}}
	// A minimal Mach-O header suffices for static architecture inspection.
	header := make([]byte, 32)
	binary.LittleEndian.PutUint32(header, 0xfeedfacf)
	binary.LittleEndian.PutUint32(header[4:], 0x100000c)
	for _, id := range []string{"node", "bridge", "sender", "commands", "keyboard", "civswitch-hid"} {
		data := header
		if id == "bridge" {
			data = []byte("structured bridge fixture")
		}
		name := "runtime/" + id
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o700); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		m.Components = append(m.Components, manifest.Component{ID: id, Path: name, SHA256: hex.EncodeToString(digest[:]),
			Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true}})
	}
	support := t.TempDir()
	t.Cleanup(func() {
		filepath.WalkDir(support, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				return os.Chmod(p, 0o700)
			}
			return err
		})
	})
	return engine.Options{Assets: root, Support: support, Version: "0.1.0", Manifest: m}
}

func TestRejectInvalidPayload(t *testing.T) {
	for _, fault := range []string{"tamper", "missing helper", "architecture", "node version", "blocked", "extra file", "symlink"} {
		t.Run(fault, func(t *testing.T) {
			o := fixture(t)
			node := filepath.Join(o.Assets, "runtime/node")
			switch fault {
			case "tamper":
				os.WriteFile(node, []byte("tampered"), 0o700)
			case "missing helper":
				os.Remove(filepath.Join(o.Assets, "runtime/sender"))
			case "architecture":
				data, _ := os.ReadFile(node)
				binary.LittleEndian.PutUint32(data[4:], 0x1000007)
				os.WriteFile(node, data, 0o700)
				digest := sha256.Sum256(data)
				o.Manifest.Components[0].SHA256 = hex.EncodeToString(digest[:])
			case "node version":
				o.Manifest.Runtime.NodeVersion = "26.13.0"
			case "blocked":
				o.Manifest.Components[0].Distribution.Decision = "blocked"
			case "extra file":
				os.WriteFile(filepath.Join(filepath.Dir(node), "unexpected.cjs"), []byte("extra"), 0o600)
			case "symlink":
				os.Remove(node)
				os.Symlink("/bin/sh", node)
			}
			if _, err := engine.Stage(o); err == nil {
				t.Fatalf("%s accepted", fault)
			}
			if _, err := os.Stat(filepath.Join(o.Support, "runtime", o.Version)); !os.IsNotExist(err) {
				t.Fatal("invalid runtime activated")
			}
		})
	}
}

func TestManualRuntimeDoesNotRequireUSB(t *testing.T) {
	o := fixture(t)
	for i := range o.Manifest.Components {
		c := &o.Manifest.Components[i]
		if c.ID == "node" || c.ID == "bridge" {
			continue
		}
		c.Distribution.Bundled = false
		if err := os.Remove(filepath.Join(o.Assets, c.Path)); err != nil {
			t.Fatal(err)
		}
	}
	root, err := engine.Stage(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.VerifyUSB(root, o.Manifest); err == nil {
		t.Fatal("unbundled USB helpers accepted")
	}
}

func TestTamperedStageDiscardedNotRepairedSilently(t *testing.T) {
	o := fixture(t)
	root, err := engine.Stage(o)
	if err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(root, "runtime/node")
	if err := os.Chmod(node, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(node, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Stage(o); err == nil {
		t.Fatal("tampered runtime accepted")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("bad stage not discarded")
	}
}

func TestWritableStageRejected(t *testing.T) {
	o := fixture(t)
	root, err := engine.Stage(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Stage(o); err == nil {
		t.Fatal("writable runtime accepted")
	}
}

func TestStageOnceReadOnlyAndWorkspaceIsolation(t *testing.T) {
	o := fixture(t)
	first, err := engine.Stage(o)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(first, "runtime/node"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o500 {
		t.Fatalf("runtime mode %o", info.Mode().Perm())
	}
	// A second activation reuses the verified tree, not the source payload.
	if err := os.RemoveAll(o.Assets); err != nil {
		t.Fatal(err)
	}
	second, err := engine.Stage(o)
	if err != nil || second != first {
		t.Fatalf("reuse: %q %v", second, err)
	}
	a, err := engine.NewWorkspace(o.Support)
	if err != nil {
		t.Fatal(err)
	}
	b, err := engine.NewWorkspace(o.Support)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || filepath.Dir(a) != filepath.Join(o.Support, "runs") {
		t.Fatal("workspace reused")
	}
	if err := os.WriteFile(filepath.Join(a, "pairing.json"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b, "pairing.json")); !os.IsNotExist(err) {
		t.Fatal("pairing state shared")
	}
	for _, p := range []string{a, b} {
		i, err := os.Stat(p)
		if err != nil || i.Mode().Perm() != 0o700 {
			t.Fatalf("workspace permissions: %v", err)
		}
	}
}

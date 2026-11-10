package manifest_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/manifest"
)

func TestPackageRejectsWrongArchitectureEvenWithMatchingDigest(t *testing.T) {
	for _, cpu := range []uint32{0x100000c, 0x1000007} {
		root := t.TempDir()
		data := make([]byte, 32)
		binary.LittleEndian.PutUint32(data, 0xfeedfacf)
		binary.LittleEndian.PutUint32(data[4:], cpu)
		if err := os.WriteFile(filepath.Join(root, "node"), data, 0o700); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		m := &manifest.Manifest{
			Runtime: manifest.Runtime{Platform: "darwin-arm64", Node: "node"},
			Components: []manifest.Component{{ID: "node", Path: "node",
				SHA256:       hex.EncodeToString(digest[:]),
				Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true}}},
		}
		err := m.CheckPackage(root)
		if (err == nil) != (cpu == 0x100000c) {
			t.Fatalf("CPU %x package result: %v", cpu, err)
		}
		if cpu == 0x100000c {
			if err := os.Chmod(filepath.Join(root, "node"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := m.CheckPackage(root); err == nil {
				t.Fatal("missing executable permissions accepted")
			}
		}
	}
}

func TestPackageGate(t *testing.T) {
	for _, decision := range []string{"may-ship", "excluded", "blocked", ""} {
		t.Run(decision, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "helper"), []byte("hello"), 0o600); err != nil {
				t.Fatal(err)
			}
			m := &manifest.Manifest{Components: []manifest.Component{{
				ID: "helper", Path: "helper",
				SHA256:       "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
				Distribution: manifest.Distribution{Decision: decision, Bundled: true, Reason: "fixture"},
			}}}
			err := m.CheckPackage(root)
			if (err == nil) != (decision == "may-ship") {
				t.Fatalf("decision %q: %v", decision, err)
			}
			m.Components[0].Distribution.Bundled = false
			if err := m.CheckPackage(root); err == nil {
				t.Fatal("unbundled component accepted")
			}
		})
	}
}

func TestPlatformOverlayPreservesHostDistributionGate(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "darwin-arm64/vendor/blocked/run.cjs")
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{
		Profiles: []manifest.Profile{{ID: "blocked", SourceDirectory: "vendor/blocked", Distribution: manifest.Distribution{Decision: "blocked"}}},
		Components: []manifest.Component{{ID: "misclassified-host", Path: "vendor/blocked/run.cjs", ResourcePlatform: "darwin-arm64",
			SHA256:       "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true}}},
	}
	if err := m.CheckPackage(root); err == nil {
		t.Fatal("overlay bypassed blocked host decision")
	}
	m.Profiles[0].Distribution = manifest.Distribution{Decision: "may-ship", Bundled: true}
	if err := m.CheckPackage(root); err != nil {
		t.Fatal(err)
	}
}

func TestDistributionAvailability(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pepsipu", "kurumuz", "cellenseres", "BermTheSequel", "izmysh", "astral", "browser-research"} {
		gate, err := m.DistributionFor(id)
		if err != nil {
			t.Fatal(err)
		}
		if gate.Available() || gate.Bundled || gate.Reason == "" {
			t.Fatalf("%s must be unavailable with a reason: %+v", id, gate)
		}
	}
	if _, err := m.DistributionFor("unknown"); err == nil {
		t.Fatal("unknown capability accepted")
	}
}

func TestPackageRejectsUnknownChangedAndLinkedResources(t *testing.T) {
	m := &manifest.Manifest{
		Profiles: []manifest.Profile{{ID: "blocked-host", SourceDirectory: "vendor/host", Distribution: manifest.Distribution{Decision: "blocked"}}},
		Components: []manifest.Component{{
			ID: "icon", Path: "icon.png",
			SHA256:       "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true},
		}},
	}
	for _, name := range []string{"unrecorded.cjs", "icon.png", "vendor/host/run.cjs", "renamed-probe.js"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "icon.png"), []byte("hello"), 0o600); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("unapproved bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := m.CheckPackage(root); err == nil {
				t.Fatal("unapproved resource accepted")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Symlink("../outside", filepath.Join(root, "icon.png")); err != nil {
			t.Fatal(err)
		}
		if err := m.CheckPackage(root); err == nil {
			t.Fatal("symlink accepted")
		}
	})
}

func TestCanonicalResourcesPassDistributionGate(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	// Ordinary tests stay offline. A source checkout has no generated Node
	// binary until prepare-runtime runs; the build gate must reject that state.
	if _, err := os.Stat("../resources/darwin-arm64/runtime/node"); os.IsNotExist(err) {
		if err := m.CheckPackage("../resources"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing generated Node accepted: %v", err)
		}
		for i := range m.Components {
			if m.Components[i].ID == "node-darwin-arm64" {
				m.Components[i].Distribution.Bundled = false
			}
		}
	}
	if err := m.CheckPackage("../resources"); err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Profiles {
		if p.Upstream == "" || p.Revision == "" || p.SourceSHA256 == "" ||
			p.LicenseStatus == "" || p.Distribution.Notice == "" || p.Distribution.Reason == "" {
			t.Errorf("incomplete license inventory for %s", p.ID)
		}
	}
	for _, c := range m.Components {
		if c.Upstream == "" || c.Revision == "" || c.SHA256 == "" ||
			c.LicenseStatus == "" || c.Distribution.Notice == "" || c.Distribution.Reason == "" {
			t.Errorf("incomplete license inventory for %s", c.ID)
		}
	}
}

func TestPackageRequiresBundledNotices(t *testing.T) {
	m := &manifest.Manifest{Components: []manifest.Component{{
		ID: "notice", Path: "LICENSE",
		SHA256:       "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true},
	}}}
	if err := m.CheckPackage(t.TempDir()); err == nil || !strings.Contains(err.Error(), "notice") {
		t.Fatalf("missing bundled notice accepted or unidentified: %v", err)
	}
}

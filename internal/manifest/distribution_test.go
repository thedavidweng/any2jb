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

	"github.com/thedavidweng/any2jb/internal/manifest"
)

func elfHeader(machine uint16) []byte {
	data := make([]byte, 64)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	binary.LittleEndian.PutUint16(data[18:], machine)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint16(data[52:], 64)
	return data
}

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
				Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true, Notice: "fixture NOTICE"}}},
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
	for _, tc := range []struct {
		platform string
		machine  uint16
		valid    bool
	}{
		{"linux-amd64", 62, true},
		{"linux-amd64", 183, false},
		{"linux-arm64", 183, true},
		{"linux-arm64", 62, false},
	} {
		root := t.TempDir()
		data := elfHeader(tc.machine)
		target := filepath.Join(root, tc.platform, "runtime/node")
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o700); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		m := &manifest.Manifest{
			Runtime: manifest.Runtime{Targets: map[string]manifest.RuntimeTarget{tc.platform: {Node: "node-" + tc.platform}}},
			Components: []manifest.Component{{
				ID: "node-" + tc.platform, Path: "runtime/node", ResourcePlatform: tc.platform,
				SHA256:       hex.EncodeToString(digest[:]),
				Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true, Notice: "fixture NOTICE"},
			}},
		}
		err := m.CheckPackage(root)
		if (err == nil) != tc.valid {
			t.Fatalf("%s machine %d package result: %v", tc.platform, tc.machine, err)
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
				Distribution: manifest.Distribution{Decision: decision, Bundled: true, Reason: "fixture", Notice: "fixture"},
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
	for _, platform := range []string{"darwin-arm64", "linux-amd64", "linux-arm64"} {
		root := t.TempDir()
		name := filepath.Join(root, platform, "vendor/blocked/run.cjs")
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		m := &manifest.Manifest{
			Profiles: []manifest.Profile{{ID: "blocked", SourceDirectory: "vendor/blocked", Distribution: manifest.Distribution{Decision: "blocked"}}},
			Components: []manifest.Component{{ID: "misclassified-host", Path: "vendor/blocked/run.cjs", ResourcePlatform: platform,
				SHA256:       "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
				Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true, Notice: "fixture"}}},
		}
		if err := m.CheckPackage(root); err == nil {
			t.Fatalf("%s overlay bypassed blocked host decision", platform)
		}
		m.Profiles[0].Distribution = manifest.Distribution{Decision: "may-ship", Bundled: true}
		if err := m.CheckPackage(root); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDistributionAvailability(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pepsipu", "kurumuz", "cellenseres", "BermTheSequel", "astral"} {
		gate, err := m.DistributionFor(id)
		if err != nil {
			t.Fatal(err)
		}
		if !gate.Available() || !gate.Bundled || gate.Notice == "" || gate.Reason == "" {
			t.Fatalf("%s must be bundled with notice and reason: %+v", id, gate)
		}
	}
	for _, id := range []string{"izmysh", "browser-research"} {
		gate, err := m.DistributionFor(id)
		if err != nil {
			t.Fatal(err)
		}
		if gate.Available() || gate.Bundled || gate.Reason == "" {
			t.Fatalf("%s must be unavailable with a reason: %+v", id, gate)
		}
	}
	for _, id := range []string{"mintbrew", "mintbrew-native", "izlauncher", "izlauncher-blitbun", "izlauncher-shell", "izlauncher-vacootube", "homebrew-runtime"} {
		gate, err := m.DistributionFor(id)
		if err != nil {
			t.Fatal(err)
		}
		if gate.Decision != "may-ship" || gate.Bundled || gate.Available() || gate.Reason == "" {
			t.Fatalf("%s must be on-demand (may-ship, bundled=false): %+v", id, gate)
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
	// Ordinary tests stay offline. A source checkout has no generated runtime or
	// upstream vendor files until prepare-runtime runs; the build gate must
	// reject that unpopulated state while still verifying all committed files.
	committed := map[string]bool{
		"placeholder-icon": true,
		"mygo-notice":      true,
		"engine-bridge":    true,
		"tool-host":        true,
	}
	missingGenerated := false
	for _, p := range m.Profiles {
		if !p.Distribution.Bundled {
			continue
		}
		if _, err := os.Stat(filepath.Join("../../resources", filepath.FromSlash(p.Bootstrap))); os.IsNotExist(err) {
			missingGenerated = true
		}
	}
	hasGenerated := func(c manifest.Component) bool {
		for _, p := range []string{c.Path, c.PackagedGzip} {
			if p == "" {
				continue
			}
			rel := p
			if c.ResourcePlatform != "" {
				rel = c.ResourcePlatform + "/" + rel
			}
			if _, err := os.Stat(filepath.Join("../../resources", filepath.FromSlash(rel))); err == nil {
				return true
			}
		}
		return false
	}
	for _, c := range m.Components {
		if !c.Distribution.Bundled || committed[c.ID] {
			continue
		}
		if !hasGenerated(c) {
			missingGenerated = true
		}
	}
	if missingGenerated {
		if err := m.CheckPackage("../../resources"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing generated resources accepted: %v", err)
		}
		unmarked := *m
		unmarked.Profiles = append([]manifest.Profile(nil), m.Profiles...)
		unmarked.Components = append([]manifest.Component(nil), m.Components...)
		for i := range unmarked.Profiles {
			if !unmarked.Profiles[i].Distribution.Bundled {
				continue
			}
			if _, err := os.Stat(filepath.Join("../../resources", filepath.FromSlash(unmarked.Profiles[i].Bootstrap))); os.IsNotExist(err) {
				unmarked.Profiles[i].Distribution.Bundled = false
			}
		}
		for i := range unmarked.Components {
			c := &unmarked.Components[i]
			if !c.Distribution.Bundled || committed[c.ID] {
				continue
			}
			if !hasGenerated(*c) {
				c.Distribution.Bundled = false
			}
		}
		if err := unmarked.CheckPackage("../../resources"); err != nil {
			t.Fatal(err)
		}
	} else if err := m.CheckPackage("../../resources"); err != nil {
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
	const hello = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "helper"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Components: []manifest.Component{{
		ID: "helper", Path: "helper", SHA256: hello,
		Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true, Reason: "fixture"},
	}}}
	err := m.CheckPackage(root)
	want := "bundled component helper has no third-party notice"
	if err == nil || err.Error() != want {
		t.Fatalf("bundled component without a notice: %v, want %q", err, want)
	}
	m.Components[0].Distribution.Notice = "Original fixture; no upstream code copied."
	if err := m.CheckPackage(root); err != nil {
		t.Fatal(err)
	}
	// A notice that names a packaged file must ship that file.
	m.Components[0].Distribution.Notice = "licenses/helper.txt"
	m.Components = append(m.Components, manifest.Component{
		ID: "helper-notice", Path: "licenses/helper.txt", SHA256: hello,
		Distribution: manifest.Distribution{Decision: "may-ship", Notice: "licenses/helper.txt"},
	})
	err = m.CheckPackage(root)
	want = "bundled component helper names notice file licenses/helper.txt, which is not bundled"
	if err == nil || err.Error() != want {
		t.Fatalf("missing bundled notice file: %v, want %q", err, want)
	}
}

func TestMergedPackageResolvesPackagedPathsAndRejectsExcluded(t *testing.T) {
	const hello = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	root := t.TempDir()
	for _, name := range []string{"runtime/helper.cjs", "AppIcon.icns", "en.lproj/InfoPlist.strings"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := &manifest.Manifest{Components: []manifest.Component{{
		ID: "helper", Path: "runtime/helper.cjs", ResourcePlatform: "darwin-arm64", SHA256: hello,
		Distribution: manifest.Distribution{Decision: "may-ship", Bundled: true, Notice: "fixture"},
	}}}
	if err := m.CheckPackageMerged(root); err != nil {
		t.Fatalf("assembled resources rejected: %v", err)
	}
	if err := m.CheckPackage(root); err == nil {
		t.Fatal("the source gate accepted a merged layout")
	}
	if err := os.WriteFile(filepath.Join(root, "runtime", "blocked.cjs"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.Components = append(m.Components, manifest.Component{ID: "blocked", Path: "runtime/blocked.cjs",
		ResourcePlatform: "darwin-arm64", SHA256: hello,
		Distribution: manifest.Distribution{Decision: "excluded", Reason: "fixture"}})
	err := m.CheckPackageMerged(root)
	if err == nil || !strings.Contains(err.Error(), "package contains blocked: excluded") {
		t.Fatalf("excluded component in the assembled app: %v", err)
	}
}

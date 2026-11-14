package manifest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

func TestPinnedProfiles(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Default != "kurumuz" || len(m.Profiles) != 5 {
		t.Fatalf("unexpected default or profile count: %s, %d", m.Default, len(m.Profiles))
	}
	for _, want := range []struct {
		id                            string
		arity, port                   int
		save                          string
		held, demo, savekit, hb, mods bool
	}{
		{"pepsipu", 9, 39380, "NS2", false, false, false, false, false},
		{"kurumuz", 5, 39380, "NS2", true, true, true, true, true},
		{"cellenseres", 5, 39380, "NS2", true, true, true, true, true},
		{"BermTheSequel", 5, 39380, "NS2", true, true, false, false, false},
		{"astral", 5, 39380, "NS2", true, true, true, true, false},
	} {
		t.Run(want.id, func(t *testing.T) {
			p, err := m.Profile(want.id)
			if err != nil {
				t.Fatal(err)
			}
			if p.StartArgumentCount != want.arity || p.StartPort != want.port || p.SaveName != want.save ||
				p.SupportsHeldRPC != want.held || p.SupportsDemo != want.demo || p.SupportsSaveKit != want.savekit ||
				p.SupportsNativeHBLauncher != want.hb || p.SupportsModloader != want.mods {
				t.Fatalf("profile contract differs from pinned mac2jb: %+v", p)
			}
		})
	}
}

func TestProfileIntegrity(t *testing.T) {
	root, m := profileFixture(t)
	if err := m.VerifyProfile(root, "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyProfile(root, "unknown"); err == nil {
		t.Fatal("unknown profile accepted")
	} else {
		var unknown *manifest.UnknownProfileError
		if !errors.As(err, &unknown) {
			t.Fatalf("expected unknown profile error, got %v", err)
		}
	}
}

func TestProfileInventoryRejections(t *testing.T) {
	for _, name := range []string{"host/run.cjs", "bootstrap.bas", "host/SHA256SUMS", "host/unrecorded.cjs"} {
		t.Run(name, func(t *testing.T) {
			root, m := profileFixture(t)
			if err := os.WriteFile(filepath.Join(root, name), []byte("modified"), 0o600); err != nil {
				t.Fatal(err)
			}
			var integrity *manifest.IntegrityError
			if err := m.VerifyProfile(root, "fixture"); !errors.As(err, &integrity) || integrity.Component != "fixture" {
				t.Fatalf("modified inventory accepted or unnamed error: %v", err)
			}
		})
	}
	t.Run("missing engine", func(t *testing.T) {
		root, m := profileFixture(t)
		if err := os.Remove(filepath.Join(root, "host/run.cjs")); err != nil {
			t.Fatal(err)
		}
		if err := m.VerifyProfile(root, "fixture"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing engine accepted: %v", err)
		}
	})
	t.Run("source identity", func(t *testing.T) {
		root, m := profileFixture(t)
		m.Profiles[0].SourceSHA256 = ""
		if err := m.VerifyProfile(root, "fixture"); err == nil {
			t.Fatal("missing source identity accepted")
		}
	})
}

func profileFixture(t *testing.T) (string, *manifest.Manifest) {
	t.Helper()
	root := t.TempDir()
	const hello = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	sums := hello + "  bootstrap.bas\n" + hello + "  run.cjs\n" + hello + "  README.md\n"
	sumDigest := sha256.Sum256([]byte(sums))
	sourceDigest := sha256.Sum256([]byte(`[["bootstrap.bas","` + hello + `"],["run.cjs","` + hello + `"]]`))
	m := &manifest.Manifest{Profiles: []manifest.Profile{{
		ID: "fixture", SourceDirectory: "host", DistributionManifest: "host/SHA256SUMS",
		DistributionManifestSHA256: hex.EncodeToString(sumDigest[:]),
		SourceSHA256:               hex.EncodeToString(sourceDigest[:]),
		Bootstrap:                  "bootstrap.bas", BootstrapSHA256: hello,
	}}}
	if err := os.Mkdir(filepath.Join(root, "host"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"host/bootstrap.bas", "host/run.cjs", "host/README.md", "bootstrap.bas"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "host/SHA256SUMS"), []byte(sums), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, m
}

func TestComponentPathRejections(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../helper", "/helper", `host\helper`, "host//helper"} {
		t.Run(name, func(t *testing.T) {
			m := &manifest.Manifest{Components: []manifest.Component{{ID: "helper", Path: name}}}
			if err := m.Verify(root, "helper"); err == nil {
				t.Fatal("invalid path accepted")
			}
		})
	}
	if err := os.WriteFile(filepath.Join(root, "original"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("original", filepath.Join(root, "helper")); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Components: []manifest.Component{{
		ID: "helper", Path: "helper", SHA256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
	}}}
	if err := m.Verify(root, "helper"); err == nil {
		t.Fatal("symlink accepted despite matching digest")
	}
	m.Components[0].Path = "original"
	m.Components[0].SHA256 = ""
	if err := m.Verify(root, "helper"); err == nil {
		t.Fatal("artifact without digest accepted")
	}
}

func TestComponentIntegrity(t *testing.T) {
	root := t.TempDir()
	m := &manifest.Manifest{Components: []manifest.Component{{
		ID: "helper", Path: "helper", SHA256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
	}}}
	if err := os.WriteFile(filepath.Join(root, "helper"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(root, "helper"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "helper"), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mismatch *manifest.IntegrityError
	if err := m.Verify(root, "helper"); !errors.As(err, &mismatch) || mismatch.Component != "helper" {
		t.Fatalf("expected named integrity error, got %v", err)
	}
	if err := os.Remove(filepath.Join(root, "helper")); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(root, "helper"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing component accepted: %v", err)
	}
	if err := m.Verify(root, "unrecorded"); err == nil {
		t.Fatal("unrecorded component accepted")
	}
}

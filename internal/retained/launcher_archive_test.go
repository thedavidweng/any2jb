package retained_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/savekit"
)

func TestLauncherChoicesAndVersionRefusalFollowManifest(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.ToolComponent("launcher-gravity-circuit")
	if err != nil {
		t.Fatal(err)
	}
	c.ID = "launcher-fixture"
	c.Launcher.Versions = []string{"2.0", "3.0"}
	m.Components = append(m.Components, c)
	if !slices.Contains(retained.LauncherIDs(m), c.ID) {
		t.Fatal("manifest launcher omitted from choices")
	}
	err = retained.ValidateLauncher(retained.LauncherRequest{ID: c.ID}, c, savekit.Request{TitleID: c.Launcher.TitleID})
	if err == nil || !strings.Contains(err.Error(), "launcher-fixture requires version 2.0 or 3.0") {
		t.Fatalf("version refusal ignored component metadata: %v", err)
	}
}

func TestLauncherRestrictions(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range retained.LauncherIDs(m) {
		c, err := m.ToolComponent(id)
		if err != nil {
			t.Fatal(err)
		}
		target := savekit.Request{TitleID: c.Launcher.TitleID}
		r := retained.LauncherRequest{ID: id}
		if id == "launcher-balatro" || id == "launcher-balatro-showcase" {
			r.PhysicalCompatibility = true
		}
		if id == "launcher-gravity-circuit" {
			r.Version = "1.2.1"
		}
		if err := retained.ValidateLauncher(r, c, target); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		target.TitleID = "ffffffffffffffff"
		if err := retained.ValidateLauncher(r, c, target); err == nil {
			t.Fatalf("%s accepted wrong game/edition", id)
		}
		d, _ := m.DistributionFor(id)
		if !d.Available() || !d.Bundled || d.Reason == "" {
			t.Fatalf("%s is not marked bundled and available with a reason", id)
		}
	}
	c, _ := m.ToolComponent("launcher-balatro")
	if err := retained.ValidateLauncher(retained.LauncherRequest{ID: c.ID}, c, savekit.Request{TitleID: c.Launcher.TitleID}); err == nil {
		t.Fatal("accepted digital/native Switch 2 Balatro")
	}
	c, _ = m.ToolComponent("launcher-gravity-circuit")
	if err := retained.ValidateLauncher(retained.LauncherRequest{ID: c.ID, Version: "1.2.0"}, c, savekit.Request{TitleID: c.Launcher.TitleID}); err == nil {
		t.Fatal("accepted unsupported Gravity Circuit version")
	}
}

func TestFullGamePatchDistinguishesExecutionFailures(t *testing.T) {
	root, m := packageFixture(t, map[string]string{"core.lua": "fixture"})
	c := m.Components[0]
	c.Launcher.FullGame = true
	backup := t.TempDir()
	if err := os.WriteFile(filepath.Join(backup, "save.bin"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	exportManifest := fmt.Sprintf(`{"programId":"%s","uid":"11111111111111111111111111111111","space":1,"type":1,"files":1,"bytes":4,"entries":[{"path":"/save.bin","size":4,"sha256":"%x"}]}`, c.Launcher.TitleID, sha256.Sum256([]byte("data")))
	if err := os.WriteFile(filepath.Join(backup, "manifest.json"), []byte(exportManifest), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, node, message string }{
		{"missing interpreter", filepath.Join(t.TempDir(), "missing-node"), "could not start"},
		{"helper failed", "/usr/bin/false", "patch helper failed"},
		{"cancelled", "/usr/bin/false", "interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.name == "cancelled" {
				cancel()
			}
			_, err := retained.PrepareLauncherArchive(ctx, m, c, root, filepath.Join(t.TempDir(), "save"), tc.node, backup)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("execution failure reported as save incompatibility: %v", err)
			}
			if tc.name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation cause lost: %v", err)
			}
		})
	}
}

func packageFixture(t *testing.T, files map[string]string) (string, *manifest.Manifest) {
	t.Helper()
	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "save.zip"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range files {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "save.zip"))
	if err != nil {
		t.Fatal(err)
	}
	return root, &manifest.Manifest{Components: []manifest.Component{{ID: "launcher-nora2-demo", Path: "save.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(data)),
		Source:   &manifest.ToolSource{Format: "selected-files", Directory: "."},
		Launcher: &manifest.LauncherSave{TitleID: "010012900d38a000", Files: len(files), Core: []string{"core.lua", "inject.lua"}}}}}
}

func TestPrepareVerifiesPackageBeforeExtracting(t *testing.T) {
	root, m := packageFixture(t, map[string]string{"core.lua": "pinned", "inject.lua": "inject"})
	c, _ := m.ToolComponent("launcher-nora2-demo")
	dir := filepath.Join(t.TempDir(), "save")
	p, err := retained.PrepareLauncherArchive(context.Background(), m, c, root, dir, "", "")
	if err != nil || len(p.Files) != 2 {
		t.Fatalf("preview: %+v %v", p, err)
	}
	if err := os.WriteFile(filepath.Join(root, "save.zip"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(t.TempDir(), "save")
	if _, err := retained.PrepareLauncherArchive(context.Background(), m, c, root, dir, "", ""); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("extracted before verification")
	}
}

func TestPrepareRefusesEscapingArchive(t *testing.T) {
	root, m := packageFixture(t, map[string]string{"../escaped": "bad"})
	c, _ := m.ToolComponent("launcher-nora2-demo")
	if _, err := retained.PrepareLauncherArchive(context.Background(), m, c, root, filepath.Join(t.TempDir(), "save"), "", ""); err == nil {
		t.Fatal("unsafe archive accepted")
	}
}

func TestUnknownLauncherCoreIsLeftUntouched(t *testing.T) {
	c := manifest.Component{Launcher: &manifest.LauncherSave{Core: []string{"core.lua", "inject.lua"}}}
	p := savekit.Preview{Files: []savekit.ExportFile{{Path: "/core.lua", SHA256: strings.Repeat("a", 64)}, {Path: "/inject.lua", SHA256: strings.Repeat("b", 64)}}}
	for _, files := range [][]savekit.ExportFile{
		{{Path: "/core.lua", SHA256: strings.Repeat("c", 64)}, {Path: "/inject.lua", SHA256: strings.Repeat("b", 64)}},
		{{Path: "/core.lua", SHA256: strings.Repeat("a", 64)}},
	} {
		if err := retained.CheckExistingLauncher(c, savekit.Export{Files: files}, p); err == nil {
			t.Fatal("unknown or partial launcher accepted")
		}
	}
	if err := retained.CheckExistingLauncher(c, savekit.Export{Files: p.Files}, p); err != nil {
		t.Fatal(err)
	}
	if err := retained.CheckExistingLauncher(c, savekit.Export{}, p); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedLauncherPackagesPrepare(t *testing.T) {
	root := os.Getenv("ANY2JB_REFERENCE_ROOT")
	if root == "" {
		t.Skip("set ANY2JB_REFERENCE_ROOT to the pinned mac2jb checkout")
	}
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range retained.LauncherIDs(m) {
		c, err := m.ToolComponent(id)
		if err != nil {
			t.Fatal(err)
		}
		if c.Launcher.FullGame {
			continue
		}
		t.Run(id, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "save")
			preview, err := retained.PrepareLauncherArchive(context.Background(), m, c, root, dest, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Files) != c.Launcher.Files {
				t.Fatalf("previewed %d files, recorded %d", len(preview.Files), c.Launcher.Files)
			}
			present := map[string]bool{}
			for _, file := range preview.Files {
				present[strings.TrimPrefix(file.Path, "/")] = true
			}
			for _, name := range c.Launcher.Core {
				if !present[name] {
					t.Errorf("the package is missing its core file %s", name)
				}
			}
		})
	}
}

package launcher_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
)

func TestFullGamePatchDistinguishesExecutionFailures(t *testing.T) {
	root, m := packageFixture(t, map[string]string{"core.lua": "fixture"})
	c := m.Components[0]
	c.Launcher.FullGame = true
	backup := t.TempDir()
	if err := os.WriteFile(filepath.Join(backup, "save.bin"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"programId":"%s","uid":"11111111111111111111111111111111","space":1,"type":1,"files":1,"bytes":4,"entries":[{"path":"/save.bin","size":4,"sha256":"%x"}]}`, c.Launcher.TitleID, sha256.Sum256([]byte("data")))
	if err := os.WriteFile(filepath.Join(backup, "manifest.json"), []byte(manifest), 0600); err != nil {
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
			_, err := launcher.Prepare(ctx, m, c, root, filepath.Join(t.TempDir(), "save"), tc.node, backup)
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
	p, err := launcher.Prepare(context.Background(), m, c, root, dir, "", "")
	if err != nil || len(p.Files) != 2 {
		t.Fatalf("preview: %+v %v", p, err)
	}
	if err := os.WriteFile(filepath.Join(root, "save.zip"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(t.TempDir(), "save")
	if _, err := launcher.Prepare(context.Background(), m, c, root, dir, "", ""); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("extracted before verification")
	}
}

func TestPrepareRefusesEscapingArchive(t *testing.T) {
	root, m := packageFixture(t, map[string]string{"../escaped": "bad"})
	c, _ := m.ToolComponent("launcher-nora2-demo")
	if _, err := launcher.Prepare(context.Background(), m, c, root, filepath.Join(t.TempDir(), "save"), "", ""); err == nil {
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
		if err := launcher.CheckExisting(c, savekit.Export{Files: files}, p); err == nil {
			t.Fatal("unknown or partial launcher accepted")
		}
	}
	if err := launcher.CheckExisting(c, savekit.Export{Files: p.Files}, p); err != nil {
		t.Fatal(err)
	}
	if err := launcher.CheckExisting(c, savekit.Export{}, p); err != nil {
		t.Fatal(err)
	}
}

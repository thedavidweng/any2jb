package retained_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

func launcherManager(t *testing.T, support string, runner retained.Workspaces, name string) *retained.Manager {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tools", "launcher-nora2-demo")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(root, "save.zip"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("fixture launcher\n")); err != nil {
		t.Fatal(err)
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
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Components {
		if m.Components[i].ID == "launcher-nora2-demo" {
			c := &m.Components[i]
			c.Path = "save.zip"
			c.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
			c.Source = &manifest.ToolSource{Format: "selected-files", Directory: "."}
			c.Launcher.Files = 1
			c.Launcher.Core = []string{name}
		}
	}
	module, err := filepath.Abs("../savekit/testdata/fake-savekit-host.cjs")
	if err != nil {
		t.Fatal(err)
	}
	host, err := filepath.Abs("testdata/fake-tool.cjs")
	if err != nil {
		t.Fatal(err)
	}
	return retained.New(retained.Options{Support: support, Node: nodePath(t), Host: host, Runs: runner, Manifest: m,
		ToolRoot: filepath.Dir(filepath.Dir(root)), Staged: []string{"savekit", "launcher-nora2-demo", "modloader"},
		SaveKitModule: module, SaveKitScenario: "ok"})
}

func TestLauncherInstallRefusedAfterModloaderHandoff(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := launcherManager(t, support, runner, "core.lua")
	r := launcher.Request{ID: "launcher-nora2-demo"}
	folder, preview, err := m.PrepareLauncher(context.Background(), r, savekit.Request{TitleID: writeTitle, UID: writeUID})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(folder))
	if err := m.RunWith(context.Background(), run, session.ToolRequest{Tool: "modloader", CodeConsent: true}, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	req := writeRequest(t, folder, preview, true)
	req.SaveKit.SourceFolder = ""
	req.Launcher = &r
	err = m.RunWith(context.Background(), run, req, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("launcher ran after modloader handoff: %v", err)
	}
	if ops := loggedOps(t, log); len(ops) != 0 {
		t.Fatalf("launcher touched the save after handoff: %v", ops)
	}
}

func TestLauncherInstallUsesTheSaveKitGate(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := launcherManager(t, support, runner, "core.lua")
	r := launcher.Request{ID: "launcher-nora2-demo"}
	target := savekit.Request{TitleID: writeTitle, UID: writeUID}
	folder, preview, err := m.PrepareLauncher(context.Background(), r, target)
	if err != nil {
		t.Fatal(err)
	}
	req := writeRequest(t, folder, preview, true)
	req.SaveKit.SourceFolder = ""
	req.Launcher = &r
	if err := m.RunWith(context.Background(), run, req, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[accounts backup write backup]" {
		t.Fatal(ops)
	}
	if out := m.State().SaveKit.Write; out == nil || !out.Verified || out.Backup == "" {
		t.Fatalf("%+v", out)
	}
}

func TestLauncherPreviewRemovesPreparedSave(t *testing.T) {
	support := t.TempDir()
	m := launcherManager(t, support, nil, "core.lua")
	preview, err := m.PreviewLauncher(context.Background(), launcher.Request{ID: "launcher-nora2-demo"},
		savekit.Request{TitleID: writeTitle, UID: writeUID})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Files) != 1 || preview.Files[0].Path != "/core.lua" {
		t.Fatalf("preview lost its file record: %+v", preview)
	}
	entries, err := os.ReadDir(filepath.Join(support, "launcher-previews"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("preview retained a prepared save: %v %v", entries, err)
	}
}

func TestLauncherUnknownCoreNeverWrites(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := launcherManager(t, support, runner, "savegame.bin")
	r := launcher.Request{ID: "launcher-nora2-demo"}
	folder, preview, err := m.PrepareLauncher(context.Background(), r, savekit.Request{TitleID: writeTitle, UID: writeUID})
	if err != nil {
		t.Fatal(err)
	}
	req := writeRequest(t, folder, preview, true)
	req.SaveKit.SourceFolder = ""
	req.Launcher = &r
	err = m.RunWith(context.Background(), run, req, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "unrecognised") ||
		!strings.Contains(err.Error(), "no file was written") {
		t.Fatalf("an unrecognised existing launcher was overwritten: %v", err)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[accounts backup]" {
		t.Fatal(ops)
	}
	// The refusal precedes any console change, so it records no outcome and
	// leaves the next, corrected attempt possible.
	if state := m.State().SaveKit; state != nil && state.Write != nil {
		t.Fatalf("a refusal before any change recorded an outcome: %+v", state.Write)
	}
}

func TestPackagedLauncherEntriesAreGated(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	manager := retained.New(retained.Options{Manifest: m})
	for _, id := range launcher.IDs(m) {
		d, _ := m.DistributionFor(id)
		if got := manager.Unavailable(id, "kurumuz"); got != d.Reason {
			t.Fatalf("%s: %q", id, got)
		}
	}
}

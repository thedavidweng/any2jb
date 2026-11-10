package manifest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/manifest"
)

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// toolFixture writes a pinned tool tree and the manifest that records it.
func toolFixture(t *testing.T, format string) (string, *manifest.Manifest) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"engine/server.cjs": "// server\n", "LICENSE": "GPL v2 text\n"}
	for name, body := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "adapter.cjs"), []byte("// adapter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var inventory []byte
	switch format {
	case "sha256sums":
		var lines []string
		for name, body := range files {
			lines = append(lines, digestOf([]byte(body))+"  "+name)
		}
		inventory = []byte(strings.Join(lines, "\n") + "\n")
	default:
		recorded := map[string]string{}
		for name, body := range files {
			recorded[name] = digestOf([]byte(body))
		}
		encoded, err := json.Marshal(map[string]any{"version": "0.1.0", "files": recorded})
		if err != nil {
			t.Fatal(err)
		}
		inventory = encoded
	}
	if err := os.WriteFile(filepath.Join(root, "INVENTORY"), inventory, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Components: []manifest.Component{{
		ID: "fixture-tool", Path: "INVENTORY", SHA256: digestOf(inventory),
		Source: &manifest.ToolSource{Directory: ".", Format: format, Files: len(files),
			Extra: map[string]string{"adapter.cjs": digestOf([]byte("// adapter\n"))}},
		Distribution: manifest.Distribution{Decision: "blocked", Reason: "fixture"},
	}}}
	return root, m
}

func TestToolSourceVerificationCoversEveryRecordedFile(t *testing.T) {
	for _, format := range []string{"sha256sums", "source-manifest"} {
		t.Run(format, func(t *testing.T) {
			root, m := toolFixture(t, format)
			names, err := m.ToolSourceFiles(root, "fixture-tool")
			if err != nil {
				t.Fatal(err)
			}
			if len(names) != 4 {
				t.Fatalf("staging inventory must list the inventory, sources and adapters: %v", names)
			}
			if err := os.WriteFile(filepath.Join(root, "engine/server.cjs"), []byte("// changed\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := m.VerifyToolSource(root, "fixture-tool"); err == nil {
				t.Fatal("changed tool source accepted")
			}
		})
	}
}

func TestToolSourceRefusesUnrecordedAndMissingFiles(t *testing.T) {
	root, m := toolFixture(t, "sha256sums")
	if err := os.WriteFile(filepath.Join(root, "engine/extra.cjs"), []byte("// extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyToolSource(root, "fixture-tool"); err == nil {
		t.Fatal("unrecorded executable module accepted")
	}
	root, m = toolFixture(t, "sha256sums")
	if err := os.Remove(filepath.Join(root, "adapter.cjs")); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyToolSource(root, "fixture-tool"); err == nil {
		t.Fatal("missing recorded adapter accepted")
	}
	root, m = toolFixture(t, "sha256sums")
	m.Components[0].Source.Files = 1
	if err := m.VerifyToolSource(root, "fixture-tool"); err == nil {
		t.Fatal("inventory size mismatch accepted")
	}
	if err := m.VerifyToolSource(root, "unknown-tool"); err == nil {
		t.Fatal("unknown tool verified")
	}
}

// selectedFilesFixture records only named files of a tree that contains more,
// for tools whose read-only stage must exclude their write entrypoints.
func selectedFilesFixture(t *testing.T) (string, *manifest.Manifest) {
	t.Helper()
	root := t.TempDir()
	recorded := map[string]string{
		"tools/list-apps.cjs":      "// list\n",
		"tools/save-inventory.cjs": "// inventory\n",
	}
	extra := map[string]string{}
	for name, body := range recorded {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		extra[name] = digestOf([]byte(body))
	}
	if err := os.WriteFile(filepath.Join(root, "NOTICE"), []byte("notice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The write entrypoint exists upstream but is never recorded or staged.
	if err := os.WriteFile(filepath.Join(root, "tools", "save-write-stage.cjs"), []byte("// write\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Components: []manifest.Component{{
		ID: "savekit", Path: "NOTICE", SHA256: digestOf([]byte("notice\n")),
		Source: &manifest.ToolSource{Directory: "tools", Format: "selected-files",
			Files: len(recorded), Extra: extra},
		Distribution: manifest.Distribution{Decision: "blocked", Reason: "fixture"},
	}}}
	return root, m
}

func TestSelectedFilesStageOnlyTheRecordedEntrypoints(t *testing.T) {
	root, m := selectedFilesFixture(t)
	names, err := m.ToolSourceFiles(root, "savekit")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("staging must list the notice and the read-only entrypoints only: %v", names)
	}
	for _, name := range names {
		if strings.Contains(name, "save-write-stage") {
			t.Fatalf("a write entrypoint entered the read-only stage: %v", names)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "list-apps.cjs"), []byte("// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyToolSource(root, "savekit"); err == nil {
		t.Fatal("a changed read-only entrypoint was accepted")
	}
	root, m = selectedFilesFixture(t)
	if err := os.Remove(filepath.Join(root, "tools", "save-inventory.cjs")); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyToolSource(root, "savekit"); err == nil {
		t.Fatal("a missing read-only entrypoint was accepted")
	}
}
func TestCanonicalToolSourcesAreRecordedAndBlocked(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ouncecake", "sd-browser", "savekit", "mintbrew", "mintbrew-native",
		"izlauncher", "izlauncher-blitbun", "izlauncher-shell", "izlauncher-vacootube", "homebrew-runtime", "modloader",
		"launcher-seedsow-demo", "launcher-seedsow-full", "launcher-nora2-demo", "launcher-nora1-demo",
		"launcher-hamidashi-demo", "launcher-lua-editor", "launcher-lua-editor-pro",
		"launcher-balatro", "launcher-balatro-showcase", "launcher-gravity-circuit"} {
		c, err := m.ToolComponent(id)
		if err != nil {
			t.Fatal(err)
		}
		if c.Distribution.Available() || c.Distribution.Bundled {
			t.Fatalf("%s is packaged without a resolved grant", id)
		}
		if c.Revision == "" || c.Upstream == "" || c.SHA256 == "" || c.Source.Files == 0 {
			t.Fatalf("%s is not pinned: %+v", id, c)
		}
		d, err := m.DistributionFor(id)
		if err != nil || d.Reason == "" || d.Notice == "" {
			t.Fatalf("%s has no user-facing reason: %+v %v", id, d, err)
		}
	}
	host, err := m.DistributionFor("tool-host")
	if err != nil || !host.Available() {
		t.Fatalf("the original tool host must be packaged: %+v %v", host, err)
	}
	if m.Runtime.ToolHost != "tool-host" {
		t.Fatalf("runtime does not pin the tool host: %q", m.Runtime.ToolHost)
	}
}

package retained_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/retained"
)

// sharedStageSource writes two embedded components that stage into one shared
// destination, each owning only its recorded directory.
func sharedStageSource(t *testing.T) (string, *manifest.Manifest) {
	t.Helper()
	root := t.TempDir()
	write := func(name, body string) string {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		return hex.EncodeToString(sum[:])
	}
	digestA := write("part-a/a.cjs", "// a\n")
	digestB := write("part-b/b.cjs", "// b\n")
	component := func(id, directory, anchor, digest string) manifest.Component {
		return manifest.Component{ID: id, Path: anchor, SHA256: digest,
			Source: &manifest.ToolSource{Directory: directory, Format: "embedded", Files: 1,
				Embedded: map[string]string{anchor: digest}, StageAs: "shared"},
			Distribution: manifest.Distribution{Decision: "blocked", Reason: "fixture"}}
	}
	return root, &manifest.Manifest{Components: []manifest.Component{
		component("part-a", "part-a", "part-a/a.cjs", digestA),
		component("part-b", "part-b", "part-b/b.cjs", digestB),
	}}
}

func TestSharedStageComponentsKeepEachOtherIntact(t *testing.T) {
	source, m := sharedStageSource(t)
	support := t.TempDir()
	options := retained.StageOptions{Support: support, Version: "0.1.0", SourceRoot: source,
		Manifest: m, Components: []string{"part-a", "part-b"}}
	root, staged, err := retained.StageTools(options)
	if err != nil || len(staged) != 2 {
		t.Fatalf("shared stage failed: %v %v", staged, err)
	}
	shared := filepath.Join(root, "tools", "shared")
	for _, name := range []string{"part-a/a.cjs", "part-b/b.cjs"} {
		if _, err := os.Stat(filepath.Join(shared, filepath.FromSlash(name))); err != nil {
			t.Fatalf("shared stage missed %s: %v", name, err)
		}
	}
	// A failed re-stage of one component must not wipe the other's files.
	if err := os.WriteFile(filepath.Join(source, "part-a/a.cjs"), []byte("// changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(shared, "part-a/a.cjs")); err != nil {
		t.Fatal(err)
	}
	_, staged, err = retained.StageTools(options)
	if err == nil || len(staged) != 1 || staged[0] != "part-b" {
		t.Fatalf("a changed source was staged: %v %v", staged, err)
	}
	if _, err := os.Stat(filepath.Join(shared, "part-b/b.cjs")); err != nil {
		t.Fatal("a failed shared re-stage removed the other component")
	}
}

// runtimeFixture writes a prepared runtime tree and its manifest document.
func runtimeFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"bin/glslangValidator": "binary\n", "python/bin/python3": "python\n"}
	type entry struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	document := map[string]any{"lockSha256": "lock-digest", "fontAdapterSha256": "adapter-digest"}
	var entries []entry
	for name, body := range files {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		entries = append(entries, entry{Path: name, SHA256: hex.EncodeToString(sum[:])})
	}
	document["files"] = entries
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, "lock-digest", "adapter-digest"
}

func TestHomebrewRuntimeVerification(t *testing.T) {
	dir, lock, adapter := runtimeFixture(t)
	if err := retained.VerifyHomebrewRuntime(dir, lock, adapter); err != nil {
		t.Fatal(err)
	}
	if err := retained.VerifyHomebrewRuntime(dir, "other-lock", adapter); err == nil {
		t.Fatal("a runtime prepared from different pinned inputs was accepted")
	}
	if err := retained.VerifyHomebrewRuntime(dir, lock, "other-adapter"); err == nil {
		t.Fatal("a runtime prepared with a different font adapter was accepted")
	}
	target := filepath.Join(dir, "bin/glslangValidator")
	if err := os.WriteFile(target, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := retained.VerifyHomebrewRuntime(dir, lock, adapter); err == nil {
		t.Fatal("a changed runtime file was accepted")
	}
	dir, lock, adapter = runtimeFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := retained.VerifyHomebrewRuntime(dir, lock, adapter); err == nil {
		t.Fatal("an unrecorded runtime file was accepted")
	}
	dir, lock, adapter = runtimeFixture(t)
	if err := os.Remove(filepath.Join(dir, "python/bin/python3")); err != nil {
		t.Fatal(err)
	}
	if err := retained.VerifyHomebrewRuntime(dir, lock, adapter); err == nil {
		t.Fatal("a missing runtime file was accepted")
	}
	dir, lock, adapter = runtimeFixture(t)
	if err := os.Symlink("bin/glslangValidator", filepath.Join(dir, "bin/link")); err != nil {
		t.Fatal(err)
	}
	if err := retained.VerifyHomebrewRuntime(dir, lock, adapter); err == nil {
		t.Fatal("a symlink inside the runtime was accepted")
	}
	if err := retained.VerifyHomebrewRuntime(filepath.Join(dir, "missing"), lock, adapter); err == nil {
		t.Fatal("a missing runtime was accepted")
	}
}

func TestHomebrewRuntimeStagingRequiresDeveloperInputs(t *testing.T) {
	if root, err := retained.StageHomebrewRuntime(retained.StageOptions{}, "", nil); root != "" || err != nil {
		t.Fatalf("packaged startup prepared a homebrew runtime: %q %v", root, err)
	}
}

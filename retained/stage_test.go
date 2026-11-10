package retained_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/retained"
)

// toolSource writes a pinned tool tree and the manifest recording it.
func toolSource(t *testing.T) (string, *manifest.Manifest) {
	t.Helper()
	root := t.TempDir()
	body := []byte("// pinned read-only module\n")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if err := os.MkdirAll(filepath.Join(root, "vendor/tool/engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor/tool/engine/server.cjs"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	inventory := []byte(digest + "  engine/server.cjs\n")
	if err := os.MkdirAll(filepath.Join(root, "packaging"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packaging/tool-SHA256SUMS"), inventory, 0o600); err != nil {
		t.Fatal(err)
	}
	inventorySum := sha256.Sum256(inventory)
	return root, &manifest.Manifest{Components: []manifest.Component{{
		ID: "pinned-tool", Path: "packaging/tool-SHA256SUMS", SHA256: hex.EncodeToString(inventorySum[:]),
		Source:       &manifest.ToolSource{Directory: "vendor/tool", Format: "sha256sums", Files: 1},
		Distribution: manifest.Distribution{Decision: "blocked", Reason: "fixture source is not redistributable"},
	}}}
}

func TestDeveloperStagingVerifiesTheCopyAndPackagedStartupStagesNothing(t *testing.T) {
	source, m := toolSource(t)
	support := t.TempDir()
	options := retained.StageOptions{Support: support, Version: "0.1.0", SourceRoot: source,
		Manifest: m, Components: []string{"pinned-tool"}}
	root, staged, err := retained.StageTools(options)
	if err != nil || len(staged) != 1 {
		t.Fatalf("staging a verified tool source failed: %v %v", staged, err)
	}
	if err := m.VerifyToolSource(filepath.Join(root, "tools/pinned-tool"), "pinned-tool"); err != nil {
		t.Fatal(err)
	}
	record, err := os.ReadFile(filepath.Join(root, "stage.json"))
	if err != nil || string(record) != `{"source":"developer-staged","redistributable":false}` {
		t.Fatalf("staged tools are not marked developer-staged: %q %v", record, err)
	}
	staged2, err := os.ReadFile(filepath.Join(root, "tools/pinned-tool/vendor/tool/engine/server.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "vendor/tool/engine/server.cjs"), append(staged2, '!'), 0o600); err != nil {
		t.Fatal(err)
	}
	// A changed pinned source must not be adopted, and the stale stage must go.
	if err := os.RemoveAll(filepath.Join(root, "tools/pinned-tool/vendor/tool/engine/server.cjs")); err != nil {
		t.Fatal(err)
	}
	_, staged, err = retained.StageTools(options)
	if err == nil || len(staged) != 0 {
		t.Fatalf("a changed tool source was staged: %v %v", staged, err)
	}
	if _, err := os.Stat(filepath.Join(root, "tools/pinned-tool")); !os.IsNotExist(err) {
		t.Fatal("a failed stage was left in place")
	}
	packagedRoot, packagedStaged, err := retained.StageTools(retained.StageOptions{Support: support,
		Version: "0.1.0", Manifest: m, Components: []string{"pinned-tool"}})
	if err != nil || packagedRoot != "" || packagedStaged != nil {
		t.Fatalf("packaged startup staged tool sources: %q %v %v", packagedRoot, packagedStaged, err)
	}
}

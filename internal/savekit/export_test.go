package savekit_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/savekit"
)

func exportFixture(t *testing.T, mutate func(dir string, manifest map[string]any)) string {
	t.Helper()
	dir := t.TempDir()
	body := []byte("save data\n")
	sum := sha256.Sum256(body)
	if err := os.WriteFile(filepath.Join(dir, "savegame.bin"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"programId": "010012900d38a000",
		"uid":       "11111111111111111111111111111111",
		"space":     1, "type": 1, "rank": 0, "index": 0,
		"files": 1, "bytes": len(body), "partial": false, "failed": []any{},
		"entries": []any{map[string]any{
			"path": "/savegame.bin", "size": len(body),
			"sha256": hex.EncodeToString(sum[:]),
		}},
	}
	if mutate != nil {
		mutate(dir, manifest)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestVerifyExportChecksManifestIntegrity(t *testing.T) {
	if err := savekit.VerifyExport(exportFixture(t, nil)); err != nil {
		t.Fatal(err)
	}
	tampered := exportFixture(t, func(dir string, _ map[string]any) {
		if err := os.WriteFile(filepath.Join(dir, "savegame.bin"), []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if err := savekit.VerifyExport(tampered); err == nil {
		t.Fatal("a changed save file passed verification")
	}
	partial := exportFixture(t, func(_ string, m map[string]any) { m["partial"] = true })
	if err := savekit.VerifyExport(partial); err == nil {
		t.Fatal("a partial export passed verification")
	}
	extra := exportFixture(t, func(dir string, _ map[string]any) {
		if err := os.WriteFile(filepath.Join(dir, "unrecorded.bin"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if err := savekit.VerifyExport(extra); err == nil {
		t.Fatal("an unrecorded file in the export passed verification")
	}
	escaped := exportFixture(t, func(_ string, m map[string]any) {
		m["entries"] = []any{map[string]any{"path": "/../outside", "size": 1, "sha256": "00"}}
	})
	if err := savekit.VerifyExport(escaped); err == nil {
		t.Fatal("an escaping export path passed verification")
	}
}

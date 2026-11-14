package savekit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/savekit"
)

func TestPreviewWriteListsAndBindsTheSourceFiles(t *testing.T) {
	source, digest := writeSource(t, map[string]string{
		"savegame.bin": "new save\n",
		"slot/one.bin": "slot one\n",
	})
	preview, err := savekit.PreviewWrite(source)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Digest != digest {
		t.Fatalf("preview digest %q, want %q", preview.Digest, digest)
	}
	if len(preview.Files) != 2 || preview.Files[0].Path != "/savegame.bin" ||
		preview.Files[1].Path != "/slot/one.bin" {
		t.Fatalf("preview files: %+v", preview.Files)
	}
	if preview.Files[0].SHA256 != digestOf("new save\n") {
		t.Fatalf("preview hash: %+v", preview.Files[0])
	}
	if preview.Bytes != int64(len("new save\n")+len("slot one\n")) {
		t.Fatalf("preview bytes: %d", preview.Bytes)
	}
}

func TestPreviewWriteRefusesUnsafeSources(t *testing.T) {
	if _, err := savekit.PreviewWrite("relative/folder"); err == nil {
		t.Fatal("a relative source folder was accepted")
	}
	if _, err := savekit.PreviewWrite(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing source folder was accepted")
	}
	empty := t.TempDir()
	if _, err := savekit.PreviewWrite(empty); err == nil {
		t.Fatal("a folder without save files was accepted")
	}
	linked, _ := writeSource(t, map[string]string{"real.bin": "data\n"})
	if err := os.Symlink(filepath.Join(linked, "real.bin"), filepath.Join(linked, "link.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := savekit.PreviewWrite(linked); err == nil {
		t.Fatal("a source with a symbolic link was accepted")
	}
	manifestOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(manifestOnly, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestOnly, ".DS_Store"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := savekit.PreviewWrite(manifestOnly); err == nil {
		t.Fatal("a folder with only skipped files was accepted")
	}
	unsafe, _ := writeSource(t, map[string]string{"ok.bin": "data\n"})
	if err := os.WriteFile(filepath.Join(unsafe, "bad\nname.bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := savekit.PreviewWrite(unsafe); err == nil {
		t.Fatal("a source with an unsafe file name was accepted")
	}
}

func TestPreviewRestoreReadsAVerifiedBackup(t *testing.T) {
	backup := exportFixture(t, nil)
	preview, err := savekit.PreviewRestore(backup)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TitleID != "010012900d38a000" || preview.UID != "11111111111111111111111111111111" {
		t.Fatalf("restore identity: %+v", preview)
	}
	if preview.Archive.Space != 1 || preview.Archive.Type != 1 {
		t.Fatalf("restore archive: %+v", preview.Archive)
	}
	if len(preview.Files) != 1 || preview.Files[0].Path != "/savegame.bin" {
		t.Fatalf("restore files: %+v", preview.Files)
	}
	tampered := exportFixture(t, func(dir string, _ map[string]any) {
		if err := os.WriteFile(filepath.Join(dir, "savegame.bin"), []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := savekit.PreviewRestore(tampered); err == nil {
		t.Fatal("an unverified backup was accepted as a restore source")
	}
}

func TestPreviewRestoreRejectsDeclaredSizeBeforeReadingFiles(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"files":1,"bytes":67108865,"entries":[{"path":"/missing.bin","size":67108865,"sha256":""}]}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := savekit.PreviewRestore(dir)
	if err == nil || !strings.Contains(err.Error(), "larger than 64 MiB") {
		t.Fatalf("restore read a file before applying declared size limits: %v", err)
	}
}

func TestPreviewDeleteBindsTheFileName(t *testing.T) {
	preview := savekit.PreviewDelete("savegame.bin")
	if preview.Digest != digestOf("delete\n/savegame.bin\n") {
		t.Fatalf("delete digest %q", preview.Digest)
	}
	if len(preview.Files) != 1 || preview.Files[0].Path != "/savegame.bin" {
		t.Fatalf("delete files: %+v", preview.Files)
	}
}

func exportOf(t *testing.T, files map[string]string) savekit.Export {
	t.Helper()
	entries := make([]savekit.ExportFile, 0, len(files))
	for name, body := range files {
		entries = append(entries, savekit.ExportFile{
			Path: "/" + name, Size: int64(len(body)),
			SHA256: digestOf(body),
		})
	}
	return savekit.Export{
		TitleID: mutTitle, UID: mutUID,
		Archive: savekit.Archive{Space: 1, Type: 1}, Files: entries,
	}
}

func TestCompareWrittenAcceptsMatchingBytesAndExtraLiveFiles(t *testing.T) {
	preview, err := savekit.PreviewWrite(writeSource2(t, map[string]string{"savegame.bin": "new\n"}))
	if err != nil {
		t.Fatal(err)
	}
	readback := exportOf(t, map[string]string{"savegame.bin": "new\n", "other.bin": "untouched\n"})
	if err := savekit.CompareWritten(readback, preview); err != nil {
		t.Fatal(err)
	}
	mismatched := exportOf(t, map[string]string{"savegame.bin": "old\n", "other.bin": "untouched\n"})
	if err := savekit.CompareWritten(mismatched, preview); err == nil {
		t.Fatal("a read-back with the old bytes passed verification")
	}
	missing := exportOf(t, map[string]string{"other.bin": "untouched\n"})
	if err := savekit.CompareWritten(missing, preview); err == nil {
		t.Fatal("a read-back missing the written file passed verification")
	}
}

func TestCompareDeletedRequiresExactlyTheFileRemoved(t *testing.T) {
	backup := exportOf(t, map[string]string{"savegame.bin": "save\n", "other.bin": "other\n"})
	readback := exportOf(t, map[string]string{"other.bin": "other\n"})
	if err := savekit.CompareDeleted(readback, backup, "savegame.bin"); err != nil {
		t.Fatal(err)
	}
	stillThere := exportOf(t, map[string]string{"savegame.bin": "save\n", "other.bin": "other\n"})
	if err := savekit.CompareDeleted(stillThere, backup, "savegame.bin"); err == nil {
		t.Fatal("a read-back still holding the file passed verification")
	}
	changed := exportOf(t, map[string]string{"other.bin": "changed\n"})
	if err := savekit.CompareDeleted(changed, backup, "savegame.bin"); err == nil {
		t.Fatal("a read-back with another file changed passed verification")
	}
	extra := exportOf(t, map[string]string{"other.bin": "other\n", "new.bin": "new\n"})
	if err := savekit.CompareDeleted(extra, backup, "savegame.bin"); err == nil {
		t.Fatal("a read-back with an extra file passed verification")
	}
}

func TestCompareExportsMatchesExactly(t *testing.T) {
	expected := exportOf(t, map[string]string{"savegame.bin": "save\n"})
	if err := savekit.CompareExports(exportOf(t, map[string]string{"savegame.bin": "save\n"}), expected); err != nil {
		t.Fatal(err)
	}
	if err := savekit.CompareExports(exportOf(t, map[string]string{"savegame.bin": "diff\n"}), expected); err == nil {
		t.Fatal("a differing live save matched")
	}
	if err := savekit.CompareExports(exportOf(t, map[string]string{"savegame.bin": "save\n", "x.bin": "x\n"}), expected); err == nil {
		t.Fatal("a live save with an extra file matched")
	}
}

func writeSource2(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, _ := writeSource(t, files)
	return dir
}

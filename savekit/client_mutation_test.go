package savekit_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/savekit"
)

// mutationBackup takes the fixture's backup the way the manager does before a
// write: a fresh export folder the mutation then re-verifies.
func mutationBackup(t *testing.T, c savekit.Client, run string) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "backup")
	_, err := c.Run(context.Background(), run, destination, savekit.Request{Action: "backup",
		TitleID: "010012900d38a000", UID: "11111111111111111111111111111111"})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func TestClientWriteReportsTheCommittedFiles(t *testing.T) {
	run := t.TempDir()
	c := client(t, "ok")
	backup := mutationBackup(t, c, run)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "extra.bin"), []byte("extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := savekit.PreviewWrite(source)
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Run(context.Background(), run, backup, savekit.Request{Action: "write",
		TitleID: "010012900d38a000", UID: "11111111111111111111111111111111",
		SourceFolder: source, Confirm: true, PreviewDigest: preview.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 1 || result.Replaced != 0 {
		t.Fatalf("write result: %+v", result)
	}
	// The fixture's live save changed: a fresh export holds the written file.
	readback := mutationBackup(t, c, run)
	export, err := savekit.ReadExport(readback)
	if err != nil {
		t.Fatal(err)
	}
	if err := savekit.CompareWritten(export, preview); err != nil {
		t.Fatal(err)
	}
}

func TestClientMutationFailuresAreReportedNotRetried(t *testing.T) {
	run := t.TempDir()
	c := client(t, "write-unverified")
	backup := mutationBackup(t, client(t, "ok"), run)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "extra.bin"), []byte("extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := savekit.PreviewWrite(source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Run(context.Background(), run, backup, savekit.Request{Action: "write",
		TitleID: "010012900d38a000", UID: "11111111111111111111111111111111",
		SourceFolder: source, Confirm: true, PreviewDigest: preview.Digest})
	if err == nil {
		t.Fatal("an unverified write reported success")
	}
	// A host echoing a different action than requested is protocol drift.
	c = client(t, "write-action")
	_, err = c.Run(context.Background(), run, "", savekit.Request{Action: "delete",
		TitleID: "010012900d38a000", UID: "11111111111111111111111111111111",
		FileName: "savegame.bin", Confirm: true,
		PreviewDigest: savekit.PreviewDelete("savegame.bin").Digest})
	if err == nil {
		t.Fatal("a host reporting another action was accepted")
	}
}

package savekit_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/savekit"
)

const (
	mutTitle = "010012900d38a000"
	mutUID   = "11111111111111111111111111111111"
)

func digestOf(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// writeSource builds a local folder of save files and returns it with the
// digest a preview of it must produce.
func writeSource(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	var canonical strings.Builder
	paths := []string{}
	for name := range files {
		paths = append(paths, name)
	}
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[j] < paths[i] {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
	for _, name := range paths {
		body := files[name]
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&canonical, "/%s\t%d\t%s\n", name, len(body), digestOf(body))
	}
	return dir, digestOf(canonical.String())
}

func TestWriteRequestsNeedTheFullGate(t *testing.T) {
	source, digest := writeSource(t, map[string]string{"savegame.bin": "new save\n"})
	valid := savekit.Request{Action: "write", TitleID: mutTitle, UID: mutUID,
		SourceFolder: source, Confirm: true, PreviewDigest: digest}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]savekit.Request{
		"unconfirmed":      {Action: "write", TitleID: mutTitle, UID: mutUID, SourceFolder: source, PreviewDigest: digest},
		"unpreviewed":      {Action: "write", TitleID: mutTitle, UID: mutUID, SourceFolder: source, Confirm: true},
		"relative source":  {Action: "write", TitleID: mutTitle, UID: mutUID, SourceFolder: "saves", Confirm: true, PreviewDigest: digest},
		"bad title":        {Action: "write", TitleID: "zz", UID: mutUID, SourceFolder: source, Confirm: true, PreviewDigest: digest},
		"explicit archive": {Action: "write", TitleID: mutTitle, UID: mutUID, SourceFolder: source, Confirm: true, PreviewDigest: digest, Archive: &savekit.Archive{Space: 1, Type: 1}},
		"file name set":    {Action: "write", TitleID: mutTitle, UID: mutUID, SourceFolder: source, Confirm: true, PreviewDigest: digest, FileName: "x"},
	}
	for name, request := range cases {
		if err := request.Validate(); err == nil {
			t.Fatalf("%s: an incompletely gated write was accepted", name)
		}
	}
}

func TestLauncherWriteCanBeValidatedBeforeItsSourceIsPrepared(t *testing.T) {
	request := savekit.Request{Action: "write", TitleID: mutTitle, UID: mutUID,
		Confirm: true, PreviewDigest: digestOf("launcher preview\n")}
	if err := request.ValidateLauncherWrite(); err != nil {
		t.Fatalf("launcher write without a view-owned source folder was rejected: %v", err)
	}
	if err := request.Validate(); err == nil {
		t.Fatal("an ordinary write without a source folder was accepted")
	}
	request.SourceFolder = "relative"
	if err := request.ValidateLauncherWrite(); err == nil {
		t.Fatal("launcher validation accepted a caller-provided source folder")
	}
}

func TestRestoreRequestsNeedAVerifiedBackupAndACompatibleArchive(t *testing.T) {
	source, digest := writeSource(t, map[string]string{"savegame.bin": "old save\n"})
	valid := savekit.Request{Action: "restore", TitleID: mutTitle, UID: mutUID,
		SourceFolder: source, Confirm: true, PreviewDigest: digest}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, archive := range []savekit.Archive{
		{Space: 1, Type: 3}, // Device saves restore like Account saves
		{Space: 4, Type: 1}, // SD user space
	} {
		request := valid
		request.Archive = &archive
		if err := request.Validate(); err != nil {
			t.Fatalf("archive %+v: %v", archive, err)
		}
	}
	for _, archive := range []savekit.Archive{
		{Space: 1, Type: 2},              // Bcat saves never restore
		{Space: 1, Type: 1, Rank: 1},     // a ranked archive
		{Space: 1, Type: 1, Index: 2},    // an indexed archive
		{Space: 1, Type: 1, StaticID: 9}, // a static archive
		{Space: 1, Type: 9},              // out of range
	} {
		request := valid
		request.Archive = &archive
		if err := request.Validate(); err == nil {
			t.Fatalf("archive %+v was accepted for restore", archive)
		}
	}
	unconfirmed := valid
	unconfirmed.Confirm = false
	if err := unconfirmed.Validate(); err == nil {
		t.Fatal("an unconfirmed restore was accepted")
	}
}

func TestDeleteRequestsNeedOneSafeRootFileAndConfirmation(t *testing.T) {
	valid := savekit.Request{Action: "delete", TitleID: mutTitle, UID: mutUID,
		FileName: "savegame.bin", Confirm: true, PreviewDigest: digestOf("delete\n/savegame.bin\n")}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "bad\nname", strings.Repeat("n", 0x300)} {
		request := valid
		request.FileName = name
		if err := request.Validate(); err == nil {
			t.Fatalf("delete of %q was accepted", name)
		}
	}
	unconfirmed := valid
	unconfirmed.Confirm = false
	if err := unconfirmed.Validate(); err == nil {
		t.Fatal("an unconfirmed delete was accepted")
	}
	withArchive := valid
	withArchive.Archive = &savekit.Archive{Space: 1, Type: 1}
	if err := withArchive.Validate(); err == nil {
		t.Fatal("a delete with an explicit archive was accepted")
	}
}

func TestCompareRequestsStayReadOnly(t *testing.T) {
	valid := savekit.Request{Action: "verify", TitleID: mutTitle, UID: mutUID,
		ExpectedFolder: t.TempDir()}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	valid.Archive = &savekit.Archive{Space: 4, Type: 3}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	mutationFields := valid
	mutationFields.SourceFolder = t.TempDir()
	if err := mutationFields.Validate(); err == nil {
		t.Fatal("a compare with a write source was accepted")
	}
	relative := valid
	relative.Archive = nil
	relative.ExpectedFolder = "exports/a"
	if err := relative.Validate(); err == nil {
		t.Fatal("a compare against a relative folder was accepted")
	}
}

func TestReadRequestsCarryNoMutationFields(t *testing.T) {
	request := savekit.Request{Action: "backup", TitleID: mutTitle, UID: mutUID, Confirm: true}
	if err := request.Validate(); err == nil {
		t.Fatal("a read carrying a confirmation was accepted")
	}
	request = savekit.Request{Action: "inspect", TitleID: mutTitle, UID: mutUID, FileName: "x"}
	if err := request.Validate(); err == nil {
		t.Fatal("a read carrying a delete target was accepted")
	}
}

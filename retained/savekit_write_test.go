package retained_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

const (
	writeTitle = "010012900d38a000"
	writeUID   = "11111111111111111111111111111111"
	otherUID   = "22222222222222222222222222222222"
)

// fixtureLog points the structured SaveKit fixture at a fresh operation log
// and returns its path.
func fixtureLog(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "ops.log")
	t.Setenv("ANY2JB_SAVEKIT_FIXTURE_LOG", log)
	return log
}

func loggedOps(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	var ops []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var op struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal([]byte(line), &op); err != nil {
			t.Fatal(err)
		}
		ops = append(ops, op.Action)
	}
	return ops
}

func writeSourceFolder(t *testing.T, files map[string]string) (string, savekit.Preview) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := savekit.PreviewWrite(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, preview
}

// exportFolder builds a verified export folder with the fixture identity.
func exportFolder(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	type entry struct {
		Path   string `json:"path"`
		Size   int    `json:"size"`
		SHA256 string `json:"sha256"`
	}
	var entries []entry
	var bytes int
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		entries = append(entries, entry{Path: "/" + name, Size: len(body),
			SHA256: hex.EncodeToString(sum[:])})
		bytes += len(body)
	}
	manifest := map[string]any{
		"archive":   "space=User type=Account rank=0 index=0 uid=1111111111111111",
		"programId": writeTitle, "uid": writeUID,
		"space": 1, "type": 1, "rank": 0, "index": 0,
		"files": len(entries), "bytes": bytes, "partial": false, "failed": []any{},
		"entries": entries,
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

func writeRequest(t *testing.T, source string, preview savekit.Preview, allowReplace bool) session.ToolRequest {
	t.Helper()
	return session.ToolRequest{Tool: "savekit", SaveKit: &savekit.Request{
		Action: "write", TitleID: writeTitle, UID: writeUID,
		SourceFolder: source, AllowReplace: allowReplace,
		Confirm: true, PreviewDigest: preview.Digest}}
}

func TestSaveKitWriteRunsTheFullGate(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit")
	source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
	if err := m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	outcome := m.State().SaveKit.Write
	if outcome == nil || !outcome.Verified || outcome.Files != 1 || outcome.Replaced != 0 {
		t.Fatalf("write outcome: %+v", outcome)
	}
	if !strings.HasPrefix(outcome.Backup, filepath.Join(support, "savekit-exports")+string(filepath.Separator)) {
		t.Fatalf("the backup left its chosen folder: %q", outcome.Backup)
	}
	if err := savekit.VerifyExport(outcome.Backup); err != nil {
		t.Fatal(err)
	}
	ops := loggedOps(t, log)
	if fmt.Sprint(ops) != "[backup write backup]" {
		t.Fatalf("operation order: %v", ops)
	}
}

func TestSaveKitFailureBlockSurvivesOtherOperations(t *testing.T) {
	for _, next := range []string{"verify", "rpc-status"} {
		t.Run(next, func(t *testing.T) {
			runner, run, support := heldSession(t)
			m := savekitManager(t, support, runner, "write-unverified", "savekit")
			source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
			request := writeRequest(t, source, preview, false)
			if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err == nil {
				t.Fatal("fixture mutation did not fail")
			}
			other := session.ToolRequest{Tool: next}
			if next == "verify" {
				other = session.ToolRequest{Tool: "savekit", SaveKit: &savekit.Request{
					Action: "verify", TitleID: writeTitle, UID: writeUID,
					ExpectedFolder: exportFolder(t, map[string]string{"savegame.bin": "save data\n"})}}
			}
			if err := m.RunWith(context.Background(), run, other, func(session.Progress) {}); err != nil {
				t.Fatal(err)
			}
			err := m.RunWith(context.Background(), run, request, func(session.Progress) {})
			if err == nil || !strings.Contains(err.Error(), "restart the console before another save operation") {
				t.Fatalf("failure latch was cleared by %s: %v", next, err)
			}
		})
	}
}

func TestSaveKitWriteAbortsWhenTheBackupFails(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "backup-fails", "savekit")
	source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
	err := m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("the write continued after its backup failed: %v", err)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[backup]" {
		t.Fatalf("a mutation ran after the backup failed: %v", ops)
	}
	outcome := m.State().SaveKit.Write
	if outcome == nil || outcome.Failure == "" {
		t.Fatalf("the failed operation left no recovery state: %+v", outcome)
	}
	// A failed save operation blocks further mutations until the console
	// restarts; nothing is retried automatically.
	err = m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "restart the console") {
		t.Fatalf("a mutation ran after a failed save operation: %v", err)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[backup]" {
		t.Fatalf("the failed operation was retried: %v", ops)
	}
}

func TestSaveKitWriteAbortsOnTargetValidationFailure(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit")
	source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
	request := writeRequest(t, source, preview, false)
	request.SaveKit.UID = otherUID
	err := m.RunWith(context.Background(), run, request, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("the write continued with a mismatched account: %v", err)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[backup]" {
		t.Fatalf("a mutation ran after target validation failed: %v", ops)
	}
	// Target validation fails before any console change, so a corrected
	// target may run.
	if err := m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[backup backup write backup]" {
		t.Fatalf("operation order: %v", ops)
	}
}

func TestSaveKitWriteRefusesAnUnpreviewedSource(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit")
	source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
	request := writeRequest(t, source, preview, false)
	request.SaveKit.PreviewDigest = strings.Repeat("0", 64)
	err := m.RunWith(context.Background(), run, request, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "preview") {
		t.Fatalf("an unpreviewed write ran: %v", err)
	}
	if ops := loggedOps(t, log); len(ops) != 0 {
		t.Fatalf("a host call ran before preview validation: %v", ops)
	}
}

func TestSaveKitWriteRequiresReplacementConsent(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit")
	source, preview := writeSourceFolder(t, map[string]string{"savegame.bin": "replaced\n"})
	err := m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("a replacing write ran without consent: %v", err)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[backup]" {
		t.Fatalf("a mutation ran without replacement consent: %v", ops)
	}
	if err := m.RunWith(context.Background(), run, writeRequest(t, source, preview, true),
		func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if outcome := m.State().SaveKit.Write; outcome == nil || !outcome.Verified || outcome.Replaced != 1 {
		t.Fatalf("write outcome: %+v", outcome)
	}
}

func TestSaveKitWriteReportsFailedVerificationAndNeverRetries(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "verify-mismatch", "savekit")
	source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
	err := m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "read-back") {
		t.Fatalf("an unverified write reported success: %v", err)
	}
	outcome := m.State().SaveKit.Write
	if outcome == nil || outcome.Verified || outcome.Failure == "" {
		t.Fatalf("the failed verification was not reported: %+v", outcome)
	}
	if err := savekit.VerifyExport(outcome.Backup); err != nil {
		t.Fatalf("the recovery backup is not accessible and verified: %v", err)
	}
	writes := 0
	for _, op := range loggedOps(t, log) {
		if op == "write" {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("the failed write was retried: %v", loggedOps(t, log))
	}
	if err := m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {}); err == nil {
		t.Fatal("a mutation ran after a failed save operation")
	}
	if n := loggedOps(t, log); len(n) != 3 {
		t.Fatalf("anything ran after the failure: %v", n)
	}
}

func TestSaveKitDeleteRunsTheFullGate(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit")
	preview := savekit.PreviewDelete("savegame.bin")
	request := session.ToolRequest{Tool: "savekit", SaveKit: &savekit.Request{
		Action: "delete", TitleID: writeTitle, UID: writeUID,
		FileName: "savegame.bin", Confirm: true, PreviewDigest: preview.Digest}}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	outcome := m.State().SaveKit.Write
	if outcome == nil || !outcome.Verified || outcome.Files != 1 {
		t.Fatalf("delete outcome: %+v", outcome)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[backup delete backup]" {
		t.Fatalf("operation order: %v", ops)
	}
	// The live save is now empty; a missing delete target fails validation.
	request.SaveKit.FileName = "missing.bin"
	request.SaveKit.PreviewDigest = savekit.PreviewDelete("missing.bin").Digest
	err := m.RunWith(context.Background(), run, request, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "not in the live save") {
		t.Fatalf("deleting an absent file was not refused: %v", err)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[backup delete backup backup]" {
		t.Fatalf("a delete ran after validation failed: %v", ops)
	}
}

func TestSaveKitRestoreRunsTheFullGate(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit")
	source := exportFolder(t, map[string]string{"savegame.bin": "restored\n"})
	preview, err := savekit.PreviewRestore(source)
	if err != nil {
		t.Fatal(err)
	}
	request := session.ToolRequest{Tool: "savekit", SaveKit: &savekit.Request{
		Action: "restore", TitleID: writeTitle, UID: writeUID,
		SourceFolder: source, Confirm: true, PreviewDigest: preview.Digest}}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	outcome := m.State().SaveKit.Write
	if outcome == nil || !outcome.Verified || outcome.Files != 1 || outcome.Replaced != 1 {
		t.Fatalf("restore outcome: %+v", outcome)
	}
	if ops := loggedOps(t, log); fmt.Sprint(ops) != "[backup restore backup]" {
		t.Fatalf("operation order: %v", ops)
	}
}

func TestSaveKitCompareStaysReadOnly(t *testing.T) {
	log := fixtureLog(t)
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "ok", "savekit")
	matching := exportFolder(t, map[string]string{"savegame.bin": "save data\n"})
	request := session.ToolRequest{Tool: "savekit", SaveKit: &savekit.Request{
		Action: "verify", TitleID: writeTitle, UID: writeUID, ExpectedFolder: matching}}
	if err := m.RunWith(context.Background(), run, request, func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
	outcome := m.State().SaveKit.Write
	if outcome == nil || outcome.Action != "compare" || !outcome.Verified {
		t.Fatalf("compare outcome: %+v", outcome)
	}
	differing := exportFolder(t, map[string]string{"savegame.bin": "other data\n"})
	request.SaveKit.ExpectedFolder = differing
	err := m.RunWith(context.Background(), run, request, func(session.Progress) {})
	if err == nil {
		t.Fatal("a differing live save matched the export")
	}
	outcome = m.State().SaveKit.Write
	if outcome == nil || outcome.Verified || outcome.Backup == "" {
		t.Fatalf("the differing comparison kept no evidence: %+v", outcome)
	}
	// A compare never mutates and never blocks later operations.
	for _, op := range loggedOps(t, log) {
		if op != "backup" {
			t.Fatalf("a compare ran a mutation: %v", loggedOps(t, log))
		}
	}
	source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
	if err := m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {}); err != nil {
		t.Fatal(err)
	}
}

func TestSaveKitMutationNeedsTheLeaseAndTheStage(t *testing.T) {
	without := savekitManager(t, t.TempDir(), nil, "ok", "savekit")
	source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
	if err := without.RunWith(context.Background(), testRun(), writeRequest(t, source, preview, false),
		func(session.Progress) {}); err == nil {
		t.Fatal("a mutation ran without a retained lease")
	}
	runner, run, support := heldSession(t)
	unstaged := savekitManager(t, support, runner, "ok")
	err := unstaged.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
		func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "SaveKit") {
		t.Fatalf("an unstaged mutation did not state the recorded reason: %v", err)
	}
}

func TestSaveKitWriteSharesTheSingleOwner(t *testing.T) {
	runner, run, support := heldSession(t)
	m := savekitManager(t, support, runner, "slow", "savekit")
	source, preview := writeSourceFolder(t, map[string]string{"extra.bin": "extra\n"})
	done := make(chan error, 1)
	go func() {
		done <- m.RunWith(context.Background(), run, writeRequest(t, source, preview, false),
			func(session.Progress) {})
	}()
	for m.State().Tool != "savekit" {
		select {
		case err := <-done:
			t.Fatalf("the write ended early: %v", err)
		default:
		}
	}
	read := session.ToolRequest{Tool: "savekit-read", SaveKit: &savekit.Request{Action: "accounts"}}
	err := m.RunWith(context.Background(), run, read, func(session.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "savekit") {
		t.Fatalf("a read ran while a mutation owned the lease: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

package retained

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/thedavidweng/any2jb/manifest"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

// runSaveKitWrite runs one gated save operation. The order is fixed and every
// step runs at most once: the confirmed preview is rebound to the source, a
// verified backup of the existing save is taken and re-verified against the
// confirmed target, the pinned mutation runs once, and a fresh read-back
// export proves the result before anything is called verified. A backup or
// target-validation failure aborts before any console change; a mutation or
// read-back failure is reported with the verified backup kept accessible and
// blocks further mutations until the console restarts. Nothing is retried.
func (m *Manager) runSaveKitWrite(ctx context.Context, run session.Run, request session.ToolRequest, workspace string, report session.Report) error {
	if request.SaveKit == nil {
		return errors.New("missing SaveKit request")
	}
	requested := *request.SaveKit
	req := &requested
	if err := req.Validate(); err != nil {
		return err
	}
	ctx, release, err := m.acquireSaveKit(ctx, "savekit")
	if err != nil {
		return err
	}
	defer release()
	m.mu.Lock()
	failed := m.state.SaveKit != nil && m.state.SaveKit.Write != nil &&
		m.state.SaveKit.Write.Failure != ""
	m.mu.Unlock()
	if failed && req.Action != "verify" {
		return errors.New("a previous save operation failed; finish this session and restart the console before another save operation")
	}
	module, err := m.savekitModule()
	if err != nil {
		return err
	}
	client := savekit.Client{Node: m.options.Node, Module: module, Scenario: m.options.SaveKitScenario}
	var component manifest.Component
	if request.Launcher != nil {
		if request.Tool != "savekit" || req.Action != "write" {
			return errors.New("launcher saves require the SaveKit write gate")
		}
		folder, _, err := m.PrepareLauncher(ctx, *request.Launcher, *req)
		if err != nil {
			return err
		}
		defer os.RemoveAll(filepath.Dir(folder))
		req.SourceFolder = folder
		component, err = m.options.Manifest.ToolComponent(request.Launcher.ID)
		if err != nil {
			return err
		}
		users, err := client.Run(ctx, workspace, "", savekit.Request{Action: "accounts"})
		if err != nil {
			return err
		}
		if !slices.Contains(users.Users, req.UID) {
			return errors.New("select the console account that owns the launcher save")
		}
	}
	if req.Action == "verify" {
		return m.compareLiveSave(ctx, client, workspace, req, report)
	}
	// The preview the user confirmed must match the source exactly as it is
	// now; for a restore the verified backup's identity is the target proof.
	preview, err := previewFor(req)
	if err != nil {
		return err
	}
	if preview.Digest != req.PreviewDigest {
		return errors.New("the source changed since its preview; preview it again before confirming")
	}
	if req.Action == "restore" {
		if err := restoreTarget(req, preview); err != nil {
			return err
		}
	}
	backup, live, err := m.verifiedLiveSave(ctx, client, workspace, req, "backup", report)
	if err != nil {
		// A failed or unverifiable backup leaves the console state uncertain:
		// report, keep recovery state and block further mutations. A
		// target-validation failure happens before any console change, so a
		// corrected target may still run.
		if !errors.Is(err, errTargetValidation) {
			m.recordWrite(SaveKitWrite{Action: req.Action, Failure: err.Error()})
		}
		return err
	}
	if request.Launcher != nil {
		// This refusal happens before any console change, so it stays
		// correctable and records no operation outcome.
		if err := m.launcherGuard(*request.Launcher, component, live, preview, *req); err != nil {
			return fmt.Errorf("%w; the verified backup is kept at %s and no file was written", err, backup)
		}
	}
	switch req.Action {
	case "write":
		replacements := 0
		for _, file := range preview.Files {
			if containsFile(live.Files, file.Path) {
				replacements++
			}
		}
		if replacements > 0 && !req.AllowReplace {
			return fmt.Errorf("the selected folder would replace %d existing save file(s); enable replacement explicitly before confirming", replacements)
		}
	case "delete":
		if !containsFile(live.Files, "/"+req.FileName) {
			return errors.New("the selected file is not in the live save; no file was deleted")
		}
	}
	report(session.Progress{SideEffect: true})
	result, err := client.Run(ctx, workspace, backup, *req)
	if err != nil {
		m.recordWrite(SaveKitWrite{Action: req.Action, Backup: backup, Failure: err.Error()})
		return fmt.Errorf("the save operation failed; the verified backup is kept at %s: %w", backup, err)
	}
	readback, readExport, err := m.verifiedLiveSave(ctx, client, workspace, req, "read-back", report)
	if err != nil {
		m.recordWrite(SaveKitWrite{Action: req.Action, Backup: backup, Failure: err.Error()})
		return fmt.Errorf("the save operation could not be read back; the verified backup is kept at %s: %w", backup, err)
	}
	verify := savekit.CompareWritten
	if req.Action == "delete" {
		verify = func(readback savekit.Export, preview savekit.Preview) error {
			return savekit.CompareDeleted(readback, live, req.FileName)
		}
	}
	if err := verify(readExport, preview); err != nil {
		m.recordWrite(SaveKitWrite{Action: req.Action, Backup: backup, Failure: err.Error()})
		return fmt.Errorf("post-write verification failed; the verified backup is kept at %s and the read-back at %s: %w",
			backup, readback, err)
	}
	m.recordWrite(SaveKitWrite{Action: req.Action, Verified: true,
		Files: result.Files, Replaced: result.Replaced, Backup: backup})
	return nil
}

// compareLiveSave checks the live save against a verified export. It is
// read-only: the live pull lands in a fresh export folder, a match or a
// difference is reported, and the comparison export is kept either way.
func (m *Manager) compareLiveSave(ctx context.Context, client savekit.Client, workspace string, req *savekit.Request, report session.Report) error {
	expected, err := savekit.ReadExport(req.ExpectedFolder)
	if err != nil {
		return fmt.Errorf("the comparison folder is not a verified export: %w", err)
	}
	if err := expected.MatchesTarget(*req); err != nil {
		return fmt.Errorf("the comparison export does not match the target: %w", err)
	}
	live, liveExport, err := m.verifiedLiveSave(ctx, client, workspace, req, "comparison export", report)
	if err != nil {
		return err
	}
	if err := savekit.CompareExports(liveExport, expected); err != nil {
		m.recordWrite(SaveKitWrite{Action: "compare", Backup: live})
		return fmt.Errorf("%w; the comparison export is kept at %s", err, live)
	}
	m.recordWrite(SaveKitWrite{Action: "compare", Verified: true,
		Files: len(expected.Files), Backup: live})
	return nil
}

// errTargetValidation marks a confirmed-target mismatch, which always
// aborts before any console change and never blocks a corrected attempt.
var errTargetValidation = errors.New("target validation failed")

// verifiedLiveSave pulls the targeted save into a fresh export folder and
// re-verifies the export's integrity and identity against the confirmed
// target. step names the phase for failure messages.
func (m *Manager) verifiedLiveSave(ctx context.Context, client savekit.Client, workspace string, req *savekit.Request, step string, report session.Report) (string, savekit.Export, error) {
	folder, err := m.freshExportFolder(req.TitleID)
	if err != nil {
		return "", savekit.Export{}, err
	}
	read := savekit.Request{Action: "backup", TitleID: req.TitleID, UID: req.UID,
		Archive: req.Archive}
	report(session.Progress{SideEffect: true})
	if _, err := client.Run(ctx, workspace, folder, read); err != nil {
		return "", savekit.Export{}, fmt.Errorf("the %s of the live save failed; no save was changed by this step: %w", step, err)
	}
	export, err := savekit.ReadExport(folder)
	if err != nil {
		return "", savekit.Export{}, fmt.Errorf("the %s failed verification: %w", step, err)
	}
	if err := export.MatchesTarget(*req); err != nil {
		return "", savekit.Export{}, fmt.Errorf("%w; no save was changed: %w", errTargetValidation, err)
	}
	return folder, export, nil
}

// previewFor recomputes the confirmed preview from the current source.
func previewFor(req *savekit.Request) (savekit.Preview, error) {
	switch req.Action {
	case "write":
		preview, err := savekit.PreviewWrite(req.SourceFolder)
		if err != nil {
			return savekit.Preview{}, fmt.Errorf("the write preview failed: %w", err)
		}
		return preview, nil
	case "restore":
		preview, err := savekit.PreviewRestore(req.SourceFolder)
		if err != nil {
			return savekit.Preview{}, fmt.Errorf("the selected backup is not a verified restore source: %w", err)
		}
		return preview, nil
	default:
		return savekit.PreviewDelete(req.FileName), nil
	}
}

// restoreTarget validates the verified restore source against the confirmed
// target: title, account and archive rank and index.
func restoreTarget(req *savekit.Request, preview savekit.Preview) error {
	if preview.TitleID != req.TitleID {
		return fmt.Errorf("the selected backup belongs to title %s, not the confirmed %s",
			preview.TitleID, req.TitleID)
	}
	if preview.UID != req.UID {
		return errors.New("the selected backup belongs to a different account than confirmed")
	}
	want := savekit.Archive{Space: 1, Type: 1}
	if req.Archive != nil {
		want = *req.Archive
	}
	if preview.Archive.Space != want.Space || preview.Archive.Type != want.Type ||
		preview.Archive.Rank != want.Rank || preview.Archive.Index != want.Index {
		return errors.New("the selected backup's archive differs from the confirmed target")
	}
	return nil
}

func containsFile(files []savekit.ExportFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func (m *Manager) recordWrite(outcome SaveKitWrite) {
	m.mu.Lock()
	defer m.mu.Unlock()
	observed := m.state.SaveKit
	if observed == nil {
		observed = &SaveKitState{}
		m.state.SaveKit = observed
	}
	observed.Action = outcome.Action
	observed.Write = &outcome
}

package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

// saveKitPanel is the SaveKit view's own ephemeral state. The session
// controller owns session state; observations live in retained.State.
type saveKitPanel struct {
	open    bool
	failed  bool
	account string
	titleID string
	archive string
	all     bool
	// The gated operations. Each preview is recomputed by the manager at run
	// time and bound to the request by its digest; each confirmation covers
	// exactly one operation and resets when it ends.
	sourceFolder   string
	restoreFolder  string
	compareFolder  string
	deleteName     string
	writePreview   *savekit.Preview
	restorePreview *savekit.Preview
	previewInvalid bool
	allowReplace   bool
	confirmWrite   bool
	confirmRestore bool
	confirmDelete  bool
}

// archiveLabel identifies one inventory archive in the archive selector.
func archiveLabel(entry savekit.Entry) string {
	return fmt.Sprintf("%s · %s · space %d type %d rank %d index %d · %s bytes",
		entry.TitleID, entry.UID[:16], entry.Space, entry.Type, entry.Rank, entry.Index, entry.Size)
}

func (a *app) saveKitView(c *ui.Context) {
	p := &a.saveKit
	s := a.controller.Snapshot()
	state := retained.State{}
	if a.tools != nil {
		state = a.tools.State()
	}
	observed := state.SaveKit
	leased := s.Stage == session.RetainedRPC || s.Stage == session.RunningTool
	ui.Text(c, locales.SaveKitTitle()).FontSize(20).Bold()
	ui.Text(c, locales.SaveKitIntro()).TextColor(c.Theme().TextMuted)
	switch {
	case state.Lost:
		ui.Text(c, locales.ToolsLost()).Bold()
	case !leased:
		ui.Text(c, locales.SaveKitNeedLease())
	}
	reason := ""
	if !slices.Contains(s.Profile.Tools, "savekit-read") {
		reason = locales.ToolsNotPermitted()
	} else if a.tools != nil {
		reason = a.tools.Unavailable("savekit-read", s.Profile.ID)
	}
	if reason != "" {
		ui.Text(c.Key("savekit-reason"), reason).TextColor(c.Theme().TextMuted)
	}
	owned := state.Tool != "" && state.Tool != "savekit-read" && state.Tool != "savekit"
	if owned {
		ui.Text(c.Key("savekit-owner"), locales.ToolsOwner(state.Tool)).TextColor(c.Theme().TextMuted)
	}
	ready := leased && !s.Busy && !state.Lost && reason == "" && !owned

	ui.Text(c, locales.SaveKitManual()).TextColor(c.Theme().TextMuted)
	if ui.Button(c.Key("savekit-accounts"), locales.SaveKitAccounts()).Disabled(!ready).Clicked() {
		a.runSaveKit(savekit.Request{Action: "accounts"})
	}
	if ui.Button(c.Key("savekit-titles"), locales.SaveKitTitles()).Disabled(!ready).Clicked() {
		a.runSaveKit(savekit.Request{Action: "titles"})
	}
	if ui.Button(c.Key("savekit-inventory"), locales.SaveKitInventory()).Disabled(!ready).Clicked() {
		a.runSaveKit(savekit.Request{Action: "inventory"})
	}

	var selected *savekit.Entry
	if observed != nil && len(observed.Inventory) > 0 {
		labels := make([]string, 0, len(observed.Inventory))
		for i := range observed.Inventory {
			labels = append(labels, archiveLabel(observed.Inventory[i]))
		}
		ui.Select(c.Key("savekit-archive"), &p.archive, labels).Label(locales.SaveKitArchive())
		for i := range observed.Inventory {
			if labels[i] == p.archive {
				entry := observed.Inventory[i]
				selected = &entry
			}
		}
	}
	if selected != nil {
		ui.Text(c, locales.SaveKitSelected(selected.TitleID, selected.UID[:16],
			fmt.Sprint(selected.Space), fmt.Sprint(selected.Type),
			fmt.Sprint(selected.Rank), fmt.Sprint(selected.Index))).Selectable()
	} else {
		if observed != nil && len(observed.Users) > 0 {
			ui.Select(c.Key("savekit-account"), &p.account, observed.Users).Label(locales.SaveKitAccount())
		}
		ui.TextInput(c.Key("savekit-title-id"), &p.titleID).Label(locales.SaveKitTitleID())
	}
	ui.Checkbox(c.Key("savekit-all"), &p.all, locales.SaveKitAll())

	target := p.target(selected)
	inspect := target
	inspect.Action = "inspect"
	backup := target
	backup.Action = "backup"
	filetime := target
	filetime.Action = "filetime"
	filetime.All = false
	if ui.Button(c.Key("savekit-inspect"), locales.SaveKitInspect()).Disabled(!ready || inspect.Validate() != nil).Clicked() {
		a.runSaveKit(inspect)
	}
	if ui.Button(c.Key("savekit-backup"), locales.SaveKitBackup()).Disabled(!ready || backup.Validate() != nil).Clicked() {
		a.runSaveKit(backup)
	}
	if ui.Button(c.Key("savekit-filetime"), locales.SaveKitFiletime()).Disabled(!ready || filetime.Validate() != nil).Clicked() {
		a.runSaveKit(filetime)
	}

	if observed != nil && observed.Output != "" {
		ui.Text(c, locales.SaveKitResult())
		ui.Text(c, observed.Output).Selectable()
	}
	if observed != nil && observed.Path != "" {
		ui.Text(c, locales.SaveKitExportSaved(observed.Path)).Selectable()
		if ui.Button(c.Key("savekit-open-export"), locales.SaveKitOpenExport()).Clicked() {
			p.failed = mygo.Shell.OpenPath(observed.Path) != nil
		}
	}

	a.saveKitWriteSection(c, p, s, state, observed, ready, target)

	if p.failed || a.commandRejected {
		ui.Text(c, locales.SaveKitFailed())
	}
	if ui.Button(c.Key("savekit-back"), locales.SaveKitBack()).Disabled(s.Busy).Clicked() {
		p.open = false
	}
}

// saveKitWriteSection renders the gated write, restore, compare and delete
// controls. Every mutation needs its own preview, its own confirmation and a
// verified backup; the manager re-checks all of them at run time.
func (a *app) saveKitWriteSection(c *ui.Context, p *saveKitPanel, s session.Snapshot, state retained.State, observed *retained.SaveKitState, ready bool, target savekit.Request) {
	ui.Text(c, locales.SaveKitWriteSection()).FontSize(16).Bold()
	writeReason := ""
	if !slices.Contains(s.Profile.Tools, "savekit") {
		writeReason = locales.ToolsNotPermitted()
	} else if a.tools != nil {
		writeReason = a.tools.Unavailable("savekit", s.Profile.ID)
	}
	if writeReason != "" {
		ui.Text(c.Key("savekit-write-reason"), writeReason).TextColor(c.Theme().TextMuted)
	}
	blocked := observed != nil && observed.Write != nil && observed.Write.Failure != ""
	writeReady := ready && writeReason == "" && !blocked
	ui.Text(c, locales.SaveKitWriteHint()).TextColor(c.Theme().TextMuted)

	archive := savekit.Archive{Space: 1, Type: 1}
	if target.Archive != nil {
		archive = *target.Archive
	}
	defaultTarget := archive.Space == 1 && archive.Type == 1 && archive.Rank == 0 &&
		archive.Index == 0 && archive.StaticID == 0
	account := target.UID
	if len(account) > 16 {
		account = account[:16]
	}
	targetText := locales.SaveKitWriteTarget(target.TitleID, account, fmt.Sprint(archive.Space),
		fmt.Sprint(archive.Type), fmt.Sprint(archive.Rank), fmt.Sprint(archive.Index))

	// Write: a local folder of save files into the default account archive.
	ui.TextInput(c.Key("savekit-write-source"), &p.sourceFolder).Label(locales.SaveKitSourceFolder())
	if ui.Button(c.Key("savekit-preview-write"), locales.SaveKitPreviewWrite()).
		Disabled(!writeReady || !defaultTarget || strings.TrimSpace(p.sourceFolder) == "").Clicked() {
		preview, err := savekit.PreviewWrite(strings.TrimSpace(p.sourceFolder))
		p.previewInvalid = err != nil
		p.writePreview = nil
		if err == nil {
			p.writePreview = &preview
		}
		p.confirmWrite = false
	}
	if p.writePreview != nil {
		ui.Text(c, locales.SaveKitPreviewFiles(fmt.Sprint(len(p.writePreview.Files)),
			fmt.Sprint(p.writePreview.Bytes)))
		shown := p.writePreview.Files
		if len(shown) > 20 {
			shown = shown[:20]
		}
		for _, file := range shown {
			ui.Text(c, file.Path+"  "+fmt.Sprint(file.Size)).Selectable()
		}
		ui.Text(c, targetText).Selectable()
		ui.Checkbox(c.Key("savekit-allow-replace"), &p.allowReplace, locales.SaveKitAllowReplace())
		ui.Checkbox(c.Key("savekit-confirm-write"), &p.confirmWrite, locales.SaveKitConfirmOperation())
	}
	write := target
	write.Archive = nil
	write.Action = "write"
	write.SourceFolder = strings.TrimSpace(p.sourceFolder)
	write.AllowReplace = p.allowReplace
	write.Confirm = p.confirmWrite
	if p.writePreview != nil {
		write.PreviewDigest = p.writePreview.Digest
	}
	if ui.Button(c.Key("savekit-write"), locales.SaveKitWrite()).
		Disabled(!writeReady || !defaultTarget || write.Validate() != nil).Clicked() {
		a.runSaveKitWrite(write)
	}
	if !defaultTarget {
		ui.Text(c, locales.SaveKitDefaultArchiveOnly()).TextColor(c.Theme().TextMuted)
	}

	// Restore: a verified backup back into its own archive.
	ui.TextInput(c.Key("savekit-restore-source"), &p.restoreFolder).Label(locales.SaveKitRestoreFolder())
	if ui.Button(c.Key("savekit-preview-restore"), locales.SaveKitPreviewRestore()).
		Disabled(!writeReady || strings.TrimSpace(p.restoreFolder) == "").Clicked() {
		preview, err := savekit.PreviewRestore(strings.TrimSpace(p.restoreFolder))
		p.previewInvalid = err != nil
		p.restorePreview = nil
		if err == nil {
			p.restorePreview = &preview
		}
		p.confirmRestore = false
	}
	ui.Text(c, locales.SaveKitRestoreHint()).TextColor(c.Theme().TextMuted)
	if p.restorePreview != nil {
		ui.Text(c, locales.SaveKitPreviewFiles(fmt.Sprint(len(p.restorePreview.Files)),
			fmt.Sprint(p.restorePreview.Bytes)))
		ui.Text(c, targetText).Selectable()
		ui.Checkbox(c.Key("savekit-confirm-restore"), &p.confirmRestore, locales.SaveKitConfirmOperation())
	}
	restore := target
	restore.Action = "restore"
	restore.SourceFolder = strings.TrimSpace(p.restoreFolder)
	restore.Confirm = p.confirmRestore
	if p.restorePreview != nil {
		restore.PreviewDigest = p.restorePreview.Digest
	}
	if ui.Button(c.Key("savekit-restore"), locales.SaveKitRestore()).
		Disabled(!writeReady || restore.Validate() != nil).Clicked() {
		a.runSaveKitWrite(restore)
	}

	// Compare: a read-only check of the live save against a verified export.
	ui.TextInput(c.Key("savekit-compare-source"), &p.compareFolder).Label(locales.SaveKitCompareFolder())
	compare := target
	compare.Action = "verify"
	compare.ExpectedFolder = strings.TrimSpace(p.compareFolder)
	if ui.Button(c.Key("savekit-compare"), locales.SaveKitCompare()).
		Disabled(!writeReady || compare.Validate() != nil).Clicked() {
		a.runSaveKitWrite(compare)
	}

	// Delete: exactly one file at the save root of the default account archive.
	ui.TextInput(c.Key("savekit-delete-name"), &p.deleteName).Label(locales.SaveKitDeleteName())
	ui.Text(c, locales.SaveKitDeleteHint()).TextColor(c.Theme().TextMuted)
	ui.Checkbox(c.Key("savekit-confirm-delete"), &p.confirmDelete, locales.SaveKitConfirmOperation())
	delete_ := target
	delete_.Archive = nil
	delete_.Action = "delete"
	delete_.FileName = p.deleteName
	delete_.Confirm = p.confirmDelete
	delete_.PreviewDigest = savekit.PreviewDelete(p.deleteName).Digest
	if ui.Button(c.Key("savekit-delete"), locales.SaveKitDelete()).
		Disabled(!writeReady || !defaultTarget || delete_.Validate() != nil).Clicked() {
		a.runSaveKitWrite(delete_)
	}
	if p.previewInvalid {
		ui.Text(c, locales.SaveKitPreviewInvalid()).TextColor(c.Theme().TextMuted)
	}

	if observed != nil && observed.Write != nil {
		a.saveKitWriteOutcome(c, p, observed.Write)
	}
}

// saveKitWriteOutcome reports one operation's result: verified only when the
// read-back proved it, otherwise the failure with its recovery path.
func (a *app) saveKitWriteOutcome(c *ui.Context, p *saveKitPanel, out *retained.SaveKitWrite) {
	switch {
	case out.Failure != "" && out.Backup != "":
		ui.Text(c, locales.SaveKitWriteFailed(out.Backup)).Selectable()
	case out.Failure != "":
		ui.Text(c, locales.SaveKitWriteFailedNoBackup())
	case out.Action == "compare" && out.Verified:
		ui.Text(c, locales.SaveKitCompareMatch(fmt.Sprint(out.Files), out.Backup)).Selectable()
	case out.Action == "compare":
		ui.Text(c, locales.SaveKitCompareDiffers(out.Backup)).Selectable()
	default:
		ui.Text(c, locales.SaveKitWriteVerified(saveKitActionName(out.Action),
			fmt.Sprint(out.Files), fmt.Sprint(out.Replaced), out.Backup)).Selectable()
	}
	if out.Backup != "" {
		if ui.Button(c.Key("savekit-open-backup"), locales.SaveKitOpenBackup()).Clicked() {
			p.failed = mygo.Shell.OpenPath(out.Backup) != nil
		}
	}
}

func saveKitActionName(action string) string {
	switch action {
	case "restore":
		return locales.SaveKitActionRestore()
	case "delete":
		return locales.SaveKitActionDelete()
	default:
		return locales.SaveKitActionWrite()
	}
}

// target resolves the operation's save identity: the selected archive fills
// title, account and archive; otherwise the manual account and title ID apply.
func (p *saveKitPanel) target(selected *savekit.Entry) savekit.Request {
	if selected != nil {
		archive := selected.Archive
		return savekit.Request{TitleID: selected.TitleID, UID: selected.UID,
			Archive: &archive, All: p.all}
	}
	return savekit.Request{TitleID: strings.ToLower(strings.TrimSpace(p.titleID)),
		UID: p.account, All: p.all}
}

func (a *app) runSaveKit(request savekit.Request) {
	a.command(a.controller.RunToolWith(session.ToolRequest{Tool: "savekit-read", SaveKit: &request}))
	a.saveKit.failed = a.commandRejected
}

// runSaveKitWrite runs one gated operation. Confirmations and previews reset
// afterwards, so the next operation needs its own preview and confirmation.
func (a *app) runSaveKitWrite(request savekit.Request) {
	a.command(a.controller.RunToolWith(session.ToolRequest{Tool: "savekit", SaveKit: &request}))
	a.saveKit.failed = a.commandRejected
	a.saveKit.confirmWrite = false
	a.saveKit.confirmRestore = false
	a.saveKit.confirmDelete = false
	a.saveKit.writePreview = nil
	a.saveKit.restorePreview = nil
}

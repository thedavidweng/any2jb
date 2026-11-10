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
	sourceFolder         string
	restoreFolder        string
	compareFolder        string
	deleteName           string
	writePreview         *savekit.Preview
	restorePreview       *savekit.Preview
	previewInvalid       bool
	allowReplace         bool
	confirmWrite         bool
	confirmRestore       bool
	confirmDelete        bool
	confirmationTarget   string
	deletePreviewName    string
	writePreviewSource   string
	restorePreviewSource string
	confirmedReplace     bool
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
	a.launcherSection(c, s, state, ready)

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
	targetText := locales.SaveKitWriteTarget(target.TitleID, target.UID, fmt.Sprint(archive.Space),
		fmt.Sprint(archive.Type), fmt.Sprint(archive.Rank), fmt.Sprint(archive.Index))
	targetKey := fmt.Sprintf("%s:%s:%+v:%t", target.TitleID, target.UID, archive, target.All)
	if p.confirmationTarget != targetKey {
		p.confirmWrite, p.confirmRestore, p.confirmDelete = false, false, false
		p.confirmationTarget = targetKey
	}

	// Write: a local folder of save files into the default account archive.
	ui.TextInput(c.Key("savekit-write-source"), &p.sourceFolder).Label(locales.SaveKitSourceFolder())
	if p.writePreviewSource != p.sourceFolder {
		p.writePreview = nil
		p.confirmWrite = false
		p.writePreviewSource = p.sourceFolder
	}
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
		for _, file := range p.writePreview.Files {
			ui.Text(c, file.Path+"  "+fmt.Sprint(file.Size)).Selectable()
		}
		ui.Text(c, targetText).Selectable()
		ui.Checkbox(c.Key("savekit-allow-replace"), &p.allowReplace, locales.SaveKitAllowReplace())
		if p.confirmedReplace != p.allowReplace {
			p.confirmWrite = false
			p.confirmedReplace = p.allowReplace
		}
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
	if p.restorePreviewSource != p.restoreFolder {
		p.restorePreview = nil
		p.confirmRestore = false
		p.restorePreviewSource = p.restoreFolder
	}
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
		for _, file := range p.restorePreview.Files {
			ui.Text(c, file.Path+"  "+fmt.Sprint(file.Size)).Selectable()
		}
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
	if p.deletePreviewName != p.deleteName {
		p.confirmDelete = false
		p.deletePreviewName = p.deleteName
	}
	ui.Text(c, locales.SaveKitDeleteHint()).TextColor(c.Theme().TextMuted)
	ui.Text(c, targetText).Selectable()
	if savekit.SafeRootName(p.deleteName) {
		ui.Text(c, "/"+p.deleteName).Selectable()
	}
	ui.Checkbox(c.Key("savekit-confirm-delete"), &p.confirmDelete, locales.SaveKitConfirmOperation())
	remove := target
	remove.Archive = nil
	remove.Action = "delete"
	remove.FileName = p.deleteName
	remove.Confirm = p.confirmDelete
	remove.PreviewDigest = savekit.PreviewDelete(p.deleteName).Digest
	if ui.Button(c.Key("savekit-delete"), locales.SaveKitDelete()).
		Disabled(!writeReady || !defaultTarget || remove.Validate() != nil).Clicked() {
		a.runSaveKitWrite(remove)
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
		ui.Text(c.Key("savekit-outcome"), locales.SaveKitWriteFailed(out.Backup)).Selectable().Role(ui.RoleStatus)
	case out.Failure != "":
		ui.Text(c.Key("savekit-outcome"), locales.SaveKitWriteFailedNoBackup()).Role(ui.RoleStatus)
	case out.Action == "compare" && out.Verified:
		ui.Text(c.Key("savekit-outcome"), locales.SaveKitCompareMatch(fmt.Sprint(out.Files), out.Backup)).Selectable().Role(ui.RoleStatus)
	case out.Action == "compare":
		ui.Text(c.Key("savekit-outcome"), locales.SaveKitCompareDiffers(out.Backup)).Selectable().Role(ui.RoleStatus)
	default:
		ui.Text(c.Key("savekit-outcome"), locales.SaveKitWriteVerified(saveKitActionName(out.Action),
			fmt.Sprint(out.Files), fmt.Sprint(out.Replaced), out.Backup)).Selectable().Role(ui.RoleStatus)
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

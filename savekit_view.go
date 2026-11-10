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

// saveKitPanel is the read-only SaveKit view's own ephemeral state. The
// session controller owns session state; observations live in retained.State.
type saveKitPanel struct {
	open    bool
	failed  bool
	account string
	titleID string
	archive string
	all     bool
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
	owned := state.Tool != "" && state.Tool != "savekit-read"
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
	if p.failed || a.commandRejected {
		ui.Text(c, locales.SaveKitFailed())
	}
	if ui.Button(c.Key("savekit-back"), locales.SaveKitBack()).Disabled(s.Busy).Clicked() {
		p.open = false
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

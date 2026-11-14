package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

type toolsPanel struct {
	busy, failed bool
	consoleIPv4  string
	sessions     []session.RetainedSession
	chosen, game string
	update       func(func())
}

// discoverRetained reads owned local sessions off the UI thread; discovery
// performs file checks and a live loopback request.
func (a *app) discoverRetained() {
	p := &a.toolsPanel
	if p.busy || p.update == nil || a.Controller == nil {
		return
	}
	p.busy = true
	controller, update := a.Controller, p.update
	go func() {
		found, err := controller.DiscoverRetained(context.Background())
		update(func() {
			p.busy, p.failed, p.sessions = false, err != nil, found
			p.chosen = ""
			if len(found) > 0 {
				p.chosen = found[0].ID
			}
		})
	}()
}

func (a *app) advancedToolsView(c *ui.Context) {
	p := &a.toolsPanel
	s := a.Controller.Snapshot()
	state := retained.State{}
	if a.Tools != nil {
		state = a.Tools.State()
	}
	leased := s.Leased()
	sectionTitle(c, locales.ToolsTitle())
	ui.Text(c, locales.ToolsReadOnly()).TextColor(c.Theme().TextMuted)
	switch {
	case state.Lost:
		ui.Text(c.Key("tools-state"), locales.ToolsLost()).Bold().Role(ui.RoleStatus)
	case leased && s.Attached:
		ui.Text(c.Key("tools-state"), locales.ToolsAttached()).Role(ui.RoleStatus)
	case leased:
		ui.Text(c.Key("tools-state"), locales.ToolsOwned()).Role(ui.RoleStatus)
	default:
		ui.Text(c.Key("tools-state"), locales.ToolsNeedLease()).Role(ui.RoleStatus)
	}
	if state.CleanupBlocked {
		ui.Text(c.Key("tools-cleanup-blocked"), locales.ToolsCleanupBlocked()).Bold().Role(ui.RoleStatus)
	}
	a.attachView(c, s)
	ui.Text(c, locales.ToolsConsoleAddress())
	ui.TextInput(c.Key("tool-console"), &p.consoleIPv4).Label(locales.ToolsConsoleAddress()).
		Placeholder(locales.ToolsConsolePlaceholder()).Disabled(s.Busy || !leased)
	muted(c, locales.ToolsConsoleHint())
	status, statusReason := a.toolGate(s, state, "rpc-status")
	if statusReason == "" {
		if inline(ui.Button(c.Key("rpc-status"), locales.ToolsStatus())).Disabled(status).Clicked() {
			a.runTool("rpc-status", "")
		}
	}
	dump, dumpReason := a.toolGate(s, state, "system-code-dump")
	if dumpReason == "" {
		if inline(ui.Button(c.Key("system-dump"), locales.ToolsDump())).Disabled(dump).Clicked() {
			a.runTool("system-code-dump", "")
		}
	}
	webdav, webdavReason := a.toolGate(s, state, "ouncecake-webdav")
	if webdavReason == "" {
		if inline(ui.Button(c.Key("ouncecake-webdav"), locales.ToolsOunceWebdav())).Disabled(webdav).Clicked() {
			a.runTool("ouncecake-webdav", p.consoleIPv4)
		}
	}
	ftp, ftpReason := a.toolGate(s, state, "ouncecake-ftp")
	if ftpReason == "" {
		if inline(ui.Button(c.Key("ouncecake-ftp"), locales.ToolsOunceFtp())).Disabled(ftp).Clicked() {
			a.runTool("ouncecake-ftp", p.consoleIPv4)
		}
	}
	a.homebrewView(c, s, state)
	a.modloaderView(c, s, state)
	a.toolRefusals(c, s, state)
	if len(state.Games) > 0 {
		ids := make([]string, 0, len(state.Games))
		for _, game := range state.Games {
			ids = append(ids, game.TitleID+" "+game.Name)
		}
		ui.Text(c, locales.ToolsGame())
		ui.Select(c.Key("ouncecake-game"), &p.game, ids).Label(locales.ToolsGame())
		if inline(ui.Button(c.Key("ouncecake-select"), locales.ToolsGameSelect())).Disabled(p.game == "").Clicked() {
			title, _, _ := strings.Cut(p.game, " ")
			p.failed = a.Tools.Select(title) != nil
		}
	}
	if state.Endpoint != "" {
		ui.Text(c, locales.ToolsEndpoint(state.Endpoint)).Selectable()
	}
	if state.Export != "" {
		ui.Text(c, locales.ToolsDumpSaved(state.Export)).Selectable()
		if inline(ui.Button(c.Key("dump-open"), locales.ToolsOpenExport())).Clicked() {
			p.failed = mygo.Shell.OpenPath(state.Export) != nil
		}
	}
	if inline(ui.Button(c.Key("tool-stop"), locales.ToolsStop())).Disabled(s.Busy || !leased || state.Tool == "").Clicked() {
		a.command(a.Controller.StopTool())
	}
	if inline(ui.Button(c.Key("savekit-open"), locales.SaveKitOpen())).Disabled(s.Busy).Clicked() {
		a.saveKit.open = true
	}
	if p.failed {
		ui.Text(c, locales.ToolsFailed())
	}
}

// toolGate separates the two refusals a user must be able to tell apart: the
// host profile not enabling a tool, and a recorded distribution decision.
func (a *app) toolGate(s session.Snapshot, state retained.State, tool string) (bool, string) {
	reason := locales.ToolsNotPermitted()
	if slices.Contains(s.Profile.Tools, tool) {
		reason = ""
		if a.Tools != nil {
			reason = a.Tools.UnavailableText(tool, s.Profile.ID).Local()
		}
	}
	leased := s.Leased()
	owned := state.Tool != "" && state.Tool != tool
	return s.Busy || !leased || state.Lost || state.ModloaderInstalled || reason != "" || owned, reason
}

func (a *app) toolRefusals(c *ui.Context, s session.Snapshot, state retained.State) {
	reasons := []string{}
	names := map[string][]string{}
	for _, tool := range []struct{ id, name string }{
		{"rpc-status", locales.ToolsStatus()}, {"system-code-dump", locales.ToolsDump()},
		{"ouncecake-webdav", locales.ToolsOunceWebdav()}, {"ouncecake-ftp", locales.ToolsOunceFtp()},
		{"mintbrew", locales.HbMintbrew()},
		{"native-hblauncher", locales.HbNative()},
		{"custom-homebrew", locales.HbCustom()}, {"modloader", locales.ModInstall()},
	} {
		_, reason := a.toolGate(s, state, tool.id)
		if reason == "" {
			continue
		}
		if _, exists := names[reason]; !exists {
			reasons = append(reasons, reason)
		}
		names[reason] = append(names[reason], tool.name)
	}
	for _, reason := range reasons {
		muted(c, locales.ToolsGroupedReason(strings.Join(names[reason], locales.ListSeparator()), reason))
	}
	if state.Tool != "" {
		muted(c, locales.ToolsOwner(state.Tool))
	}
}

func (a *app) runTool(tool, consoleIPv4 string) {
	a.command(a.Controller.RunToolWith(session.ToolRequest{Tool: tool, ConsoleIPv4: consoleIPv4}))
}

func (a *app) attachView(c *ui.Context, s session.Snapshot) {
	p := &a.toolsPanel
	leased := s.Leased()
	if !leased || s.Attached {
		ui.Text(c, locales.ToolsAttachNote())
	}
	if leased {
		if s.Attached {
			ui.Text(c, locales.ToolsDetachNote())
			if inline(ui.Button(c.Key("rpc-detach"), locales.ToolsDetach())).Disabled(s.Busy).Clicked() {
				a.command(a.Controller.DetachRetained())
			}
		}
		return
	}
	if inline(ui.Button(c.Key("rpc-find"), locales.ToolsFind())).Disabled(s.Busy || p.busy || p.update == nil).Clicked() {
		a.discoverRetained()
	}
	if len(p.sessions) == 0 {
		ui.Text(c, locales.ToolsNoSessions())
		return
	}
	ids := make([]string, 0, len(p.sessions))
	for _, found := range p.sessions {
		ids = append(ids, found.ID)
	}
	ui.Select(c.Key("rpc-choose"), &p.chosen, ids).Label(locales.ToolsChoose())
	if inline(ui.Button(c.Key("rpc-attach"), locales.ToolsAttach())).Disabled(s.Busy || p.chosen == "").Clicked() {
		a.command(a.Controller.AttachRetained(p.chosen))
	}
}

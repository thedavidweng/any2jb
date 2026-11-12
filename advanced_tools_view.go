package main

import (
	"context"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

// toolAdapter is the read-only tool adapter's own ephemeral data. The session
// controller remains the only owner of session state.
type toolAdapter interface {
	Unavailable(tool, profileID string) string
	State() retained.State
	Select(titleID string) error
	PreviewLauncher(context.Context, launcher.Request, savekit.Request) (savekit.Preview, error)
}

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
	if p.busy || p.update == nil || a.controller == nil {
		return
	}
	p.busy = true
	controller, update := a.controller, p.update
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
	s := a.controller.Snapshot()
	state := retained.State{}
	if a.tools != nil {
		state = a.tools.State()
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
	ui.TextInput(c.Key("tool-console"), &p.consoleIPv4).Label(locales.ToolsConsoleAddress()).Disabled(s.Busy || !leased)
	status, statusReason := a.toolGate(s, state, "rpc-status")
	if inline(ui.Button(c.Key("rpc-status"), locales.ToolsStatus())).Disabled(status).Clicked() {
		a.runTool("rpc-status", "")
	}
	a.toolReason(c, "rpc-status", state, statusReason)
	dump, dumpReason := a.toolGate(s, state, "system-code-dump")
	if inline(ui.Button(c.Key("system-dump"), locales.ToolsDump())).Disabled(dump).Clicked() {
		a.runTool("system-code-dump", "")
	}
	a.toolReason(c, "system-code-dump", state, dumpReason)
	webdav, webdavReason := a.toolGate(s, state, "ouncecake-webdav")
	if inline(ui.Button(c.Key("ouncecake-webdav"), locales.ToolsOunceWebdav())).Disabled(webdav).Clicked() {
		a.runTool("ouncecake-webdav", p.consoleIPv4)
	}
	a.toolReason(c, "ouncecake-webdav", state, webdavReason)
	ftp, ftpReason := a.toolGate(s, state, "ouncecake-ftp")
	if inline(ui.Button(c.Key("ouncecake-ftp"), locales.ToolsOunceFtp())).Disabled(ftp).Clicked() {
		a.runTool("ouncecake-ftp", p.consoleIPv4)
	}
	a.toolReason(c, "ouncecake-ftp", state, ftpReason)
	sd, sdReason := a.toolGate(s, state, "sd-browser")
	if inline(ui.Button(c.Key("sd-browser"), locales.ToolsSDBrowser())).Disabled(sd).Clicked() {
		a.runTool("sd-browser", p.consoleIPv4)
	}
	a.toolReason(c, "sd-browser", state, sdReason)
	a.homebrewView(c, s, state)
	a.modloaderView(c, s, state)
	if len(state.Games) > 0 {
		ids := make([]string, 0, len(state.Games))
		for _, game := range state.Games {
			ids = append(ids, game.TitleID+" "+game.Name)
		}
		ui.Text(c, locales.ToolsGame())
		ui.Select(c.Key("ouncecake-game"), &p.game, ids).Label(locales.ToolsGame())
		if inline(ui.Button(c.Key("ouncecake-select"), locales.ToolsGameSelect())).Disabled(p.game == "").Clicked() {
			title, _, _ := strings.Cut(p.game, " ")
			p.failed = a.tools.Select(title) != nil
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
		a.command(a.controller.StopTool())
	}
	if inline(ui.Button(c.Key("savekit-open"), locales.SaveKitOpen())).Disabled(s.Busy).Clicked() {
		a.saveKit.open = true
	}
	// A gated or excluded component states the recorded reason instead of an
	// action; host capability booleans never decide this.
	if d, err := a.manifest.DistributionFor("browser-research"); err == nil {
		ui.Text(c, locales.ToolsExcluded("browser-research", localize(d.Reason))).TextColor(c.Theme().TextMuted)
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
		if a.tools != nil {
			reason = a.tools.Unavailable(tool, s.Profile.ID)
		}
	}
	leased := s.Leased()
	owned := state.Tool != "" && state.Tool != tool
	return s.Busy || !leased || state.Lost || state.ModloaderInstalled || reason != "" || owned, reason
}

func (a *app) toolReason(c *ui.Context, tool string, state retained.State, reason string) {
	if reason != "" {
		ui.Text(c.Key(tool+"-reason"), localize(reason)).TextColor(c.Theme().TextMuted)
		return
	}
	if state.Tool != "" && state.Tool != tool {
		ui.Text(c.Key(tool+"-owner"), locales.ToolsOwner(state.Tool)).TextColor(c.Theme().TextMuted)
	}
}

func (a *app) runTool(tool, consoleIPv4 string) {
	a.command(a.controller.RunToolWith(session.ToolRequest{Tool: tool, ConsoleIPv4: consoleIPv4}))
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
				a.command(a.controller.DetachRetained())
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
		a.command(a.controller.AttachRetained(p.chosen))
	}
}

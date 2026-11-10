package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/retained"
	"github.com/thedavidweng/any2jb/session"
)

// homebrewPanel is the homebrew launcher's ephemeral input. The session
// controller validates every field again before a launch.
type homebrewPanel struct {
	sdFolder, appFolder            string
	izExchange, blitbun, vacootube bool
}

// homebrewView offers the pinned homebrew launchers. Gating is the same
// toolGate contract as the read-only tools: a recorded distribution decision
// or an unstaged component states its reason, and one owner runs at a time.
func (a *app) homebrewView(c *ui.Context, s session.Snapshot, state retained.State) {
	p := &a.homebrewPanel
	leased := s.Stage == session.RetainedRPC || s.Stage == session.RunningTool
	ui.Text(c, locales.HbTitle()).FontSize(16).Bold()
	ui.Text(c, locales.HbNote()).TextColor(c.Theme().TextMuted)
	mintbrew, mintbrewReason := a.toolGate(s, state, "mintbrew")
	if ui.Button(c.Key("hb-mintbrew"), locales.HbMintbrew()).Disabled(mintbrew).Clicked() {
		a.runHomebrew("mintbrew")
	}
	a.toolReason(c, "hb-mintbrew", state, mintbrewReason)
	ui.TextInput(c.Key("hb-sd-folder"), &p.sdFolder).Label(locales.HbSDFolder()).Disabled(s.Busy || !leased)
	ui.TextInput(c.Key("hb-app-folder"), &p.appFolder).Label(locales.HbAppFolder()).Disabled(s.Busy || !leased)
	native, nativeReason := a.toolGate(s, state, "native-hblauncher")
	if ui.Button(c.Key("hb-native"), locales.HbNative()).Disabled(native).Clicked() {
		a.runHomebrew("native-hblauncher")
	}
	a.toolReason(c, "hb-native", state, nativeReason)
	ui.Text(c, locales.HbModules())
	ui.Checkbox(c.Key("hb-mod-iz-exchange"), &p.izExchange, locales.HbModIZExchange()).Disabled(s.Busy || !leased)
	ui.Checkbox(c.Key("hb-mod-blitbun"), &p.blitbun, locales.HbModBlitbun()).Disabled(s.Busy || !leased)
	ui.Checkbox(c.Key("hb-mod-vacootube"), &p.vacootube, locales.HbModVacootube()).Disabled(s.Busy || !leased)
	iz, izReason := a.toolGate(s, state, "izlauncher")
	if ui.Button(c.Key("hb-izlauncher"), locales.HbIZ()).Disabled(iz).Clicked() {
		a.runHomebrew("izlauncher")
	}
	a.toolReason(c, "hb-izlauncher", state, izReason)
}

func (a *app) runHomebrew(tool string) {
	p := &a.homebrewPanel
	request := session.ToolRequest{Tool: tool}
	switch tool {
	case "native-hblauncher":
		request.SDRomFolder = strings.TrimSpace(p.sdFolder)
		request.AppFolder = strings.TrimSpace(p.appFolder)
	case "izlauncher":
		for _, module := range []struct {
			id      string
			enabled bool
		}{
			{"iz-exchange", p.izExchange}, {"blitbun", p.blitbun}, {"vacootube", p.vacootube},
		} {
			if module.enabled {
				request.Modules = append(request.Modules, module.id)
			}
		}
	}
	a.command(a.controller.RunToolWith(request))
}

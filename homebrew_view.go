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
	sdFolder, appFolder, customFolder, modsFolder string
	izExchange, blitbun, vacootube                bool
}

// homebrewView offers the pinned homebrew launchers. Gating is the same
// toolGate contract as the read-only tools: a recorded distribution decision
// or an unstaged component states its reason, and one owner runs at a time.
func (a *app) homebrewView(c *ui.Context, s session.Snapshot, state retained.State) {
	p := &a.homebrewPanel
	leased := s.Leased()
	mintbrew, mintbrewReason := a.toolGate(s, state, "mintbrew")
	native, nativeReason := a.toolGate(s, state, "native-hblauncher")
	iz, izReason := a.toolGate(s, state, "izlauncher")
	custom, customReason := a.toolGate(s, state, "custom-homebrew")
	if mintbrewReason != "" && nativeReason != "" && izReason != "" && customReason != "" {
		return
	}
	ui.Text(c, locales.HbTitle()).FontSize(16).Bold()
	ui.Text(c, locales.HbNote()).TextColor(c.Theme().TextMuted)
	if mintbrewReason == "" {
		if inline(ui.Button(c.Key("hb-mintbrew"), locales.HbMintbrew())).Disabled(mintbrew).Clicked() {
			a.runHomebrew("mintbrew")
		}
	}
	if nativeReason == "" {
		ui.TextInput(c.Key("hb-sd-folder"), &p.sdFolder).Label(locales.HbSDFolder()).Disabled(s.Busy || !leased)
		ui.TextInput(c.Key("hb-app-folder"), &p.appFolder).Label(locales.HbAppFolder()).Disabled(s.Busy || !leased)
		if inline(ui.Button(c.Key("hb-native"), locales.HbNative())).Disabled(native).Clicked() {
			a.runHomebrew("native-hblauncher")
		}
	}
	if izReason == "" {
		ui.Text(c, locales.HbModules())
		ui.Checkbox(c.Key("hb-mod-iz-exchange"), &p.izExchange, locales.HbModIZExchange()).Disabled(s.Busy || !leased)
		ui.Checkbox(c.Key("hb-mod-blitbun"), &p.blitbun, locales.HbModBlitbun()).Disabled(s.Busy || !leased)
		ui.Checkbox(c.Key("hb-mod-vacootube"), &p.vacootube, locales.HbModVacootube()).Disabled(s.Busy || !leased)
		if inline(ui.Button(c.Key("hb-izlauncher"), locales.HbIZ())).Disabled(iz).Clicked() {
			a.runHomebrew("izlauncher")
		}
	}
	if customReason == "" {
		ui.Text(c, locales.HbCustomTitle()).Bold()
		ui.Text(c, locales.HbCustomNote()).TextColor(c.Theme().TextMuted)
		ui.TextInput(c.Key("hb-custom-folder"), &p.customFolder).Label(locales.HbCustomFolder()).Disabled(s.Busy || !leased)
		if inline(ui.Button(c.Key("hb-custom"), locales.HbCustom())).Disabled(custom).Clicked() {
			a.runHomebrew("custom-homebrew")
		}
	}
}

// modloaderView offers the modloader as the session's final action. A verified
// handoff locks every tool until the console restarts, and the view says so.
func (a *app) modloaderView(c *ui.Context, s session.Snapshot, state retained.State) {
	p := &a.homebrewPanel
	leased := s.Leased()
	install, reason := a.toolGate(s, state, "modloader")
	if reason != "" {
		return
	}
	ui.Text(c, locales.ModTitle()).FontSize(16).Bold()
	ui.Text(c, locales.ModNote()).TextColor(c.Theme().TextMuted)
	if state.ModloaderInstalled {
		ui.Text(c.Key("mod-installed"), locales.ModInstalled()).Bold().Role(ui.RoleStatus)
	}
	ui.TextInput(c.Key("mod-folder"), &p.modsFolder).Label(locales.ModFolder()).Disabled(s.Busy || !leased || state.ModloaderInstalled)
	if inline(ui.Button(c.Key("mod-install"), locales.ModInstall())).Disabled(install).Clicked() {
		a.runHomebrew("modloader")
	}
}

func (a *app) runHomebrew(tool string) {
	p := &a.homebrewPanel
	request := session.ToolRequest{Tool: tool}
	switch tool {
	case "native-hblauncher":
		request.SDRomFolder = strings.TrimSpace(p.sdFolder)
		request.AppFolder = strings.TrimSpace(p.appFolder)
	case "custom-homebrew":
		request.CustomHomebrewFolder = strings.TrimSpace(p.customFolder)
	case "modloader":
		request.ModsFolder = strings.TrimSpace(p.modsFolder)
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

package ui

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/retained"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

// homebrewPanel is the homebrew launcher's ephemeral input. The session
// controller validates every field again before a launch.
type homebrewPanel struct {
	sdFolder, appFolder, customFolder, modsFolder string
	izExchange, blitbun, vacootube                bool
	modsConsent                                   bool
	consentedModsFolder                           string
	customConsent, nativeConsent                  bool
	consentedCustomFolder, consentedAppFolder     string
	installing                                    bool
	installNotice                                 string
}

// homebrewView offers the pinned homebrew launchers. Gating is the same
// toolGate contract as the read-only tools: a recorded distribution decision
// or an unstaged component states its reason, and one owner runs at a time.
func (a *app) homebrewView(c *ui.Context, s session.Snapshot, state retained.State) {
	p := &a.homebrewPanel
	leased := s.Leased()
	installer, ok := a.Tools.(appstate.HomebrewInstaller)
	canInstall := ok && installer.HomebrewInstallable(s.Profile)
	mintbrew, mintbrewReason := a.toolGate(s, state, "mintbrew")
	native, nativeReason := a.toolGate(s, state, "native-hblauncher")
	iz, izReason := a.toolGate(s, state, "izlauncher")
	custom, customReason := a.toolGate(s, state, "custom-homebrew")
	if mintbrewReason != "" && nativeReason != "" && izReason != "" && customReason != "" &&
		!canInstall && !p.installing && p.installNotice == "" {
		return
	}
	sectionTitle(c, locales.HbTitle())
	ui.Text(c, locales.HbNote()).TextColor(c.Theme().TextMuted)
	if canInstall || p.installing {
		ui.Text(c, locales.HbInstallNote()).TextColor(c.Theme().TextMuted)
		if inline(ui.Button(c.Key("hb-install"), locales.HbInstall())).Disabled(s.Busy || p.installing || state.Tool != "" || a.toolsPanel.update == nil).Clicked() {
			a.installHomebrew(installer)
		}
	}
	if p.installing {
		ui.Text(c.Key("hb-install-status"), locales.HbInstalling()).Role(ui.RoleStatus)
	} else if p.installNotice != "" {
		ui.Text(c.Key("hb-install-status"), p.installNotice).Role(ui.RoleStatus)
	}
	if mintbrewReason == "" {
		if inline(ui.Button(c.Key("hb-mintbrew"), locales.HbMintbrew())).Disabled(mintbrew).Clicked() {
			a.runHomebrew("mintbrew")
		}
	}
	if nativeReason == "" {
		ui.TextInput(c.Key("hb-sd-folder"), &p.sdFolder).Label(locales.HbSDFolder()).Disabled(s.Busy || !leased)
		ui.TextInput(c.Key("hb-app-folder"), &p.appFolder).Label(locales.HbAppFolder()).Disabled(s.Busy || !leased)
		if p.appFolder != p.consentedAppFolder {
			p.nativeConsent = false
			p.consentedAppFolder = p.appFolder
		}
		if strings.TrimSpace(p.appFolder) != "" {
			ui.Checkbox(c.Key("hb-native-consent"), &p.nativeConsent, locales.HbCodeConsent()).Disabled(native)
		}
		if inline(ui.Button(c.Key("hb-native"), locales.HbNative())).Disabled(native || strings.TrimSpace(p.appFolder) != "" && !p.nativeConsent).Clicked() {
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
		if p.customFolder != p.consentedCustomFolder {
			p.customConsent = false
			p.consentedCustomFolder = p.customFolder
		}
		ui.Checkbox(c.Key("hb-custom-consent"), &p.customConsent, locales.HbCodeConsent()).Disabled(custom)
		if inline(ui.Button(c.Key("hb-custom"), locales.HbCustom())).Disabled(custom || !p.customConsent).Clicked() {
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
	sectionTitle(c, locales.ModTitle())
	ui.Text(c, locales.ModNote()).TextColor(c.Theme().TextMuted)
	if state.ModloaderInstalled {
		ui.Text(c.Key("mod-installed"), locales.ModInstalled()).Bold().Role(ui.RoleStatus)
	}
	ui.TextInput(c.Key("mod-folder"), &p.modsFolder).Label(locales.ModFolder()).Disabled(s.Busy || !leased || state.ModloaderInstalled)
	if p.modsFolder != p.consentedModsFolder {
		p.modsConsent = false
		p.consentedModsFolder = p.modsFolder
	}
	ui.Checkbox(c.Key("mod-consent"), &p.modsConsent, locales.ModConsent()).Disabled(install)
	if inline(ui.Button(c.Key("mod-install"), locales.ModInstall())).Disabled(install || !p.modsConsent).Clicked() {
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
		request.CodeConsent = p.nativeConsent
		p.nativeConsent = false
	case "custom-homebrew":
		request.CustomHomebrewFolder = strings.TrimSpace(p.customFolder)
		request.CodeConsent = p.customConsent
		p.customConsent = false
	case "modloader":
		request.ModsFolder = strings.TrimSpace(p.modsFolder)
		request.CodeConsent = p.modsConsent
		p.modsConsent = false
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
	a.command(a.Controller.RunToolWith(request))
}

func (a *app) installHomebrew(installer appstate.HomebrewInstaller) {
	p := &a.homebrewPanel
	update := a.toolsPanel.update
	if installer == nil || p.installing || update == nil {
		return
	}
	p.installing = true
	p.installNotice = ""
	go func() {
		err := installer.InstallHomebrew(context.Background())
		update(func() {
			p.installing = false
			if err != nil {
				p.installNotice = appstate.PresentError(err)
			} else {
				p.installNotice = locales.HbInstallReady()
			}
		})
	}()
}

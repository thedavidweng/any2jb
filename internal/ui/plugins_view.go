package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/plugins"
	"github.com/thedavidweng/any2jb/locales"
)

type pluginsPanel struct {
	category string
	selected string
	busy     bool
	notice   string
	failed   bool
	update   func(func())
}

func (a *app) pluginsView(c *ui.Context) {
	p := &a.pluginsPanel
	if p.category == "" {
		p.category = "all"
	}
	ui.Text(c, locales.PluginsTitle()).FontSize(c.Theme().Rem(1.55)).Bold()
	card(c, func() {
		muted(c, locales.PluginsIntro())
		pm := a.Plugins()
		if pm == nil {
			ui.Text(c, locales.NoSession())
			return
		}
		all := pm.PluginsStatus()
		installedCount := 0
		supportedCount := 0
		var filtered []plugins.Status
		for _, item := range all {
			if item.Supported {
				supportedCount++
				if item.Installed {
					installedCount++
				}
			}
			if p.category != "all" && string(item.Spec.Category) != p.category {
				continue
			}
			filtered = append(filtered, item)
		}
		ui.Text(c, locales.PluginsInstalledCount(fmt.Sprint(installedCount), fmt.Sprint(supportedCount))).Bold()

		categories := []string{"all", string(plugins.CategorySession), string(plugins.CategoryHomebrew), string(plugins.CategoryCivSwitch), string(plugins.CategoryArtemis), string(plugins.CategoryUtilities)}
		ui.Select(c.Key("plugin-category"), &p.category, categories).Label(locales.PluginsCategory()).Disabled(p.busy)

		ids := make([]string, 0, len(filtered))
		var current *plugins.Status
		for i := range filtered {
			ids = append(ids, filtered[i].Spec.ID)
			if filtered[i].Spec.ID == p.selected {
				current = &filtered[i]
			}
		}
		if current == nil && len(filtered) > 0 {
			p.selected = filtered[0].Spec.ID
			current = &filtered[0]
		}
		if len(ids) > 0 {
			ui.Select(c.Key("plugin-select"), &p.selected, ids).Label(locales.PluginsSelect()).Disabled(p.busy)
		}

		if current != nil {
			locale := i18n.Locale()
			title := locales.Catalog.Format(locale, current.Spec.TitleKey, nil)
			desc := locales.Catalog.Format(locale, current.Spec.DescKey, nil)
			stateLabel := locales.PluginsStateNotInstalled()
			if !current.Supported {
				stateLabel = locales.PluginsStateUnsupported()
			} else if current.Installed {
				stateLabel = locales.PluginsStateInstalled()
			}
			stateCard(c, func() {
				ui.Text(c, locales.PluginsDetailFormat(title, current.Spec.ID, stateLabel, current.Spec.License)).Bold()
				muted(c, desc)
				if current.Spec.UpstreamURL != "" {
					inline(ui.Link(c.Key("plugin-upstream"), current.Spec.UpstreamName, current.Spec.UpstreamURL))
				}
			})
		}

		canMutate := !p.busy && p.update != nil && !a.pluginSessionBusy()
		ui.Row(c).Wrap().Gap(8).Children(func() {
			canInstallOne := canMutate && current != nil && current.Supported && !current.Installed
			if ui.PrimaryButton(c.Key("plugin-install"), locales.PluginsInstall()).Disabled(!canInstallOne).Clicked() {
				a.runPluginMutation(pm, true, current.Spec.ID)
			}
			canRemoveOne := canMutate && current != nil && current.Installed
			if ui.Button(c.Key("plugin-uninstall"), locales.PluginsUninstall()).Disabled(!canRemoveOne).Clicked() {
				a.runPluginMutation(pm, false, current.Spec.ID)
			}
			if ui.Button(c.Key("plugin-install-category"), locales.PluginsInstallCategory()).Disabled(!canMutate || p.category == "all").Clicked() {
				a.runPluginMutation(pm, true, "category:"+p.category)
			}
			if ui.Button(c.Key("plugin-install-all"), locales.PluginsInstallAll()).Disabled(!canMutate).Clicked() {
				a.runPluginMutation(pm, true, "all")
			}
		})

		if p.busy {
			ui.Text(c.Key("plugin-status"), locales.PluginsInstalling()).Role(ui.RoleStatus)
		} else if p.notice != "" {
			txt := ui.Text(c.Key("plugin-status"), p.notice).Role(ui.RoleStatus)
			if p.failed {
				txt.TextColor(c.Theme().Danger)
			}
		}
	})
}

func (a *app) pluginSessionBusy() bool {
	if a.Controller != nil {
		s := a.Controller.Snapshot()
		if s.Busy {
			return true
		}
	}
	if a.Tools != nil && a.Tools.State().Tool != "" {
		return true
	}
	return false
}

func (a *app) runPluginMutation(pm appstate.PluginManager, install bool, selector string) {
	p := &a.pluginsPanel
	update := p.update
	if pm == nil || p.busy || update == nil {
		return
	}
	p.busy = true
	p.failed = false
	p.notice = ""
	go func() {
		var changed []string
		var err error
		if install {
			changed, err = pm.InstallPlugins(context.Background(), selector)
		} else {
			changed, err = pm.UninstallPlugins(context.Background(), selector)
		}
		update(func() {
			p.busy = false
			if err != nil {
				p.failed = true
				p.notice = appstate.PresentError(err)
				return
			}
			joined := strings.Join(changed, ", ")
			if install {
				p.notice = locales.PluginsInstalledDone(joined)
			} else {
				p.notice = locales.PluginsUninstalledDone(joined)
			}
		})
	}()
}

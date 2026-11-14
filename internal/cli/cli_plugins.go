package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/spf13/cobra"
	"github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/plugins"
	"github.com/thedavidweng/any2jb/locales"
)

func pluginCategoryLabel(cat plugins.Category) string {
	switch cat {
	case plugins.CategorySession:
		return locales.PluginsCatRetained()
	case plugins.CategoryHomebrew:
		return locales.PluginsCatHomebrew()
	case plugins.CategoryCivSwitch:
		return locales.PluginsCatCivSwitch()
	case plugins.CategoryArtemis:
		return locales.PluginsCatArtemis()
	case plugins.CategoryUtilities:
		return locales.PluginsCatDevtools()
	default:
		return string(cat)
	}
}

func (c *commandLine) addPlugins(root *cobra.Command) {
	group := &cobra.Command{Use: "plugins", Short: locales.PluginsTitle()}
	root.AddCommand(group)

	var listCategory string
	listCmd := c.command(group, "list", locales.PluginsTitle(), false, func(*cobra.Command, []string) error {
		pm := c.app.Plugins()
		if pm == nil {
			return cliFail(exitBlocked, locales.CommandRejected())
		}
		all := pm.PluginsStatus()
		var filtered []plugins.Status
		for _, item := range all {
			if listCategory != "" && listCategory != "all" && string(item.Spec.Category) != listCategory {
				continue
			}
			filtered = append(filtered, item)
		}
		if c.json {
			return c.print(filtered)
		}
		for _, item := range filtered {
			state := locales.PluginsStateNotInstalled()
			if !item.Supported {
				state = locales.PluginsStateUnsupported()
			} else if item.Installed {
				state = locales.PluginsStateInstalled()
			}
			title := locales.Catalog.Format(i18n.Locale(), item.Spec.TitleKey, nil)
			fmt.Fprintf(c.out, "%s\t%s\t%s\t%s\n", item.Spec.ID, pluginCategoryLabel(item.Spec.Category), state, title)
		}
		return nil
	})
	listCmd.Flags().StringVar(&listCategory, "category", "", locales.PluginsCategory())

	var installAll bool
	var installCategory string
	installCmd := c.command(group, "install [ID...]", locales.PluginsInstall(), false, func(_ *cobra.Command, args []string) error {
		pm := c.app.Plugins()
		if pm == nil {
			return cliFail(exitBlocked, locales.CommandRejected())
		}
		var selectors []string
		if installAll {
			selectors = append(selectors, "all")
		}
		if cat := strings.TrimSpace(installCategory); cat != "" {
			selectors = append(selectors, "category:"+cat)
		}
		selectors = append(selectors, args...)
		if len(selectors) == 0 {
			return cliFail(exitError, cliMessage("cliInvalidArguments"))
		}
		installed, err := pm.InstallPlugins(context.Background(), selectors...)
		if err != nil {
			return cliFail(exitError, app.PresentError(err))
		}
		return c.print(struct {
			Installed []string `json:"installed"`
		}{Installed: installed})
	})
	installCmd.Args = cobra.ArbitraryArgs
	installCmd.Flags().BoolVar(&installAll, "all", false, locales.PluginsInstallAll())
	installCmd.Flags().StringVar(&installCategory, "category", "", locales.PluginsInstallCategory())

	uninstallCmd := c.command(group, "uninstall ID...", locales.PluginsUninstall(), false, func(_ *cobra.Command, args []string) error {
		pm := c.app.Plugins()
		if pm == nil {
			return cliFail(exitBlocked, locales.CommandRejected())
		}
		removed, err := pm.UninstallPlugins(context.Background(), args...)
		if err != nil {
			return cliFail(exitError, app.PresentError(err))
		}
		return c.print(struct {
			Removed []string `json:"removed"`
		}{Removed: removed})
	})
	uninstallCmd.Args = cobra.MinimumNArgs(1)
}

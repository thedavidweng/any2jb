package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/egoist/mygo"
	"github.com/spf13/cobra"
	"github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/diagnostics"
	"github.com/thedavidweng/any2jb/internal/transport"
	"github.com/thedavidweng/any2jb/locales"
)

func decodeCLIJSON(value string, target any) error {
	d := json.NewDecoder(strings.NewReader(value))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return cliFail(exitError, cliMessage("cliInvalidArguments"))
	}
	return nil
}
func (c *commandLine) addSettings(root *cobra.Command) {
	group := &cobra.Command{Use: "settings", Short: locales.Settings()}
	root.AddCommand(group)
	group.AddCommand(&cobra.Command{Use: "get", Short: locales.Settings(), Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		if err := c.load(); err != nil {
			return err
		}
		return c.print(c.app.Preferences)
	}})
	var profile, edition, input, address, usb string
	set := &cobra.Command{Use: "set", Short: locales.ApplyPreferences(), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := c.load(); err != nil {
			return err
		}
		if err := c.own(); err != nil {
			return err
		}
		old := c.app.Preferences
		if cmd.Flags().Changed("profile") {
			c.app.Preferences.Profile = profile
		}
		if cmd.Flags().Changed("edition") {
			c.app.Preferences.Edition = edition
		}
		if cmd.Flags().Changed("input") {
			c.app.Preferences.Transport = input
		}
		if cmd.Flags().Changed("lan") {
			c.app.Preferences.LANIPv4 = address
		}
		if cmd.Flags().Changed("usb") {
			var prefs app.USBPreferences
			if err := decodeCLIJSON(usb, &prefs); err != nil {
				c.app.Preferences = old
				return err
			}
			c.app.Preferences.USB = &prefs
		}
		if err := c.app.ApplyPreferences(); err != nil {
			c.app.Preferences = old
			return err
		}
		if c.app.KeyboardInput != nil && c.app.Preferences.USB != nil {
			p := c.app.Preferences.USB
			if err := c.app.KeyboardInput.Configure(p.Controller, p.Timing); err != nil {
				return err
			}
		}
		return c.print(c.app.Preferences)
	}}
	group.AddCommand(set)
	set.Flags().StringVar(&profile, "profile", "", locales.HostProfile())
	set.Flags().StringVar(&edition, "edition", "", locales.SmilebasicEdition())
	set.Flags().StringVar(&input, "input", "", locales.TransportMode())
	set.Flags().StringVar(&address, "lan", "", locales.LanOverride())
	set.Flags().StringVar(&usb, "usb", "", locales.UsbKeyboardTitle())
	c.static(group, "profiles", locales.HostProfile(), func(*cobra.Command, []string) error { return c.print(c.app.Manifest.Profiles) })
	c.static(group, "developer", locales.DeveloperOptions(), func(*cobra.Command, []string) error {
		return c.print(locales.DeveloperStaged() + "\nANY2JB_DEVELOPER_CONFIG\n" + locales.BuildLimitations())
	})
	c.static(group, "about", locales.AboutBuild(), func(*cobra.Command, []string) error {
		return c.print(mygo.App.Version() + "\n" + locales.BuildLimitations())
	})
	c.static(group, "updates", locales.AboutBuild(), func(*cobra.Command, []string) error {
		result, err := mygo.Updater.Check(context.Background())
		if err != nil {
			return err
		}
		return c.print(result)
	})
	language := &cobra.Command{Use: "language", Short: cliMessage("cliLanguage"), Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		return c.print(c.locale())
	}}
	root.AddCommand(language)
	c.static(root, "usb-defaults", locales.UsbStockDefaults(), func(*cobra.Command, []string) error { return c.print(transport.DefaultTiming()) })
}

func (c *commandLine) addDiagnostics(root *cobra.Command) {
	group := &cobra.Command{Use: "diagnostics", Short: locales.Diagnostics()}
	root.AddCommand(group)
	var run, source string
	logs := c.command(group, "logs", locales.Diagnostics(), false, func(*cobra.Command, []string) error {
		if run == "" {
			run = c.app.Controller.Snapshot().RunID
		}
		logs, folder, err := (diagnostics.Store{Support: c.support}).Refresh(run)
		if err != nil {
			return err
		}
		if c.json {
			return c.print(struct {
				Logs   diagnostics.Logs
				Folder string
			}{logs, folder})
		}
		value, ok := map[string]string{"all": strings.Join([]string{logs.Host, logs.USB, logs.Pipeline, logs.Acquisition}, "\n"), "host": logs.Host, "usb": logs.USB, "pipeline": logs.Pipeline, "acquisition": logs.Acquisition}[source]
		if !ok {
			return cliFail(exitError, cliMessage("cliInvalidArguments"))
		}
		return c.print(value)
	})
	logs.Flags().StringVar(&run, "run", "", cliMessage("cliRun"))
	logs.Flags().StringVar(&source, "source", "all", locales.LogSource())
	c.command(group, "export", locales.ExportSupportBundle(), false, func(*cobra.Command, []string) error {
		path, err := c.app.Controller.ExportDiagnostics(context.Background())
		if err != nil {
			return err
		}
		return c.print(path)
	})
	c.command(group, "folders", locales.OpenSessionFolder(), false, func(*cobra.Command, []string) error { return c.print(locales.LocalDataFolders(c.support)) })
	var cleanup string
	clean := c.command(group, "cleanup", locales.PreviewCleanup(), false, func(*cobra.Command, []string) error {
		s := c.app.Controller.Snapshot()
		if s.Busy || s.SideEffects {
			return cliFail(exitBlocked, locales.CommandRejected())
		}
		store := diagnostics.Store{Support: c.support}
		plan, err := store.PreviewCleanup()
		if err != nil {
			return err
		}
		if err := c.print(plan); err != nil {
			return err
		}
		if cleanup == "" {
			return nil
		}
		if err := c.consent(cleanup, "delete-temporary-files", locales.CleanupScope()); err != nil {
			return err
		}
		return store.Cleanup(plan)
	})
	clean.Flags().StringVar(&cleanup, "consent", "", cliMessage("cliConsent"))
}

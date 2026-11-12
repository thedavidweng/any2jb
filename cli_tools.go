package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thedavidweng/any2jb/launcher"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/savekit"
	"github.com/thedavidweng/any2jb/session"
)

func (c *commandLine) addTools(root *cobra.Command) {
	tools := &cobra.Command{Use: "tool", Short: locales.ToolsTitle()}
	root.AddCommand(tools)
	c.command(tools, "list", locales.ToolsTitle(), false, func(*cobra.Command, []string) error {
		type entry struct{ ID, Unavailable string }
		var entries []entry
		s := c.app.controller.Snapshot()
		for _, id := range s.Profile.Tools {
			reason := ""
			if c.app.tools != nil {
				reason = c.app.tools.Unavailable(id, s.Profile.ID)
			}
			entries = append(entries, entry{id, localize(reason)})
		}
		return c.print(entries)
	})
	c.command(tools, "state", locales.ToolsStatus(), false, func(*cobra.Command, []string) error {
		if c.app.tools == nil {
			return cliFail(exitBlocked, locales.ToolsNeedLease())
		}
		return c.print(c.app.tools.State())
	})
	var request session.ToolRequest
	var consent string
	run := c.command(tools, "run TOOL", locales.ToolsTitle(), true, func(_ *cobra.Command, args []string) error {
		request.Tool = args[0]
		// The shared validator decides which requests run user-supplied code.
		if request.ValidateCodeConsent() != nil {
			risk := locales.HbCustomNote() + "\n" + locales.HbCodeConsent()
			if request.Tool == "modloader" {
				risk = locales.ModNote() + "\n" + locales.ModConsent()
			}
			if err := c.consent(consent, "run-user-code", risk); err != nil {
				return err
			}
			request.CodeConsent = true
		}
		return c.operation(c.app.controller.RunToolWith(request))
	})
	run.Args = cobra.ExactArgs(1)
	run.Flags().StringVar(&request.ConsoleIPv4, "console", "", locales.ToolsConsoleAddress())
	run.Flags().StringVar(&request.AppFolder, "app-folder", "", locales.HbAppFolder())
	run.Flags().StringVar(&request.SDRomFolder, "sd-folder", "", locales.HbSDFolder())
	run.Flags().StringVar(&request.ModsFolder, "mods-folder", "", locales.ModFolder())
	run.Flags().StringVar(&request.CustomHomebrewFolder, "custom-folder", "", locales.HbCustomFolder())
	run.Flags().StringSliceVar(&request.Modules, "modules", nil, locales.HbModules())
	run.Flags().StringVar(&consent, "consent", "", cliMessage("cliConsent"))
	c.command(tools, "stop", locales.ToolsStop(), true, func(*cobra.Command, []string) error { return c.operation(c.app.controller.StopTool()) })
	selectGame := c.command(tools, "select TITLE", locales.ToolsGameSelect(), true, func(_ *cobra.Command, args []string) error {
		if c.app.tools == nil {
			return cliFail(exitBlocked, locales.ToolsNeedLease())
		}
		return c.app.tools.Select(args[0])
	})
	selectGame.Args = cobra.ExactArgs(1)
	c.command(root, "discover", locales.ToolsTitle(), false, func(*cobra.Command, []string) error {
		found, err := c.app.controller.DiscoverRetained(context.Background())
		if err != nil {
			return err
		}
		return c.print(found)
	})
	attach := c.command(root, "connect ID", locales.ToolsTitle(), true, func(_ *cobra.Command, args []string) error {
		return c.operation(c.app.controller.AttachRetained(args[0]))
	})
	attach.Args = cobra.ExactArgs(1)
	c.command(root, "disconnect", locales.ToolsTitle(), true, func(*cobra.Command, []string) error { return c.operation(c.app.controller.DetachRetained()) })
	c.addSaveKit(root)
	c.addLaunchers(root)
}

func (c *commandLine) addSaveKit(root *cobra.Command) {
	group := &cobra.Command{Use: "savekit", Short: locales.SaveKit()}
	root.AddCommand(group)
	for _, action := range []string{"accounts", "titles", "inventory", "inspect", "backup", "filetime", "verify", "write", "restore", "delete"} {
		var request savekit.Request
		var consent string
		cmd := c.command(group, action, locales.SaveKit(), true, func(cmd *cobra.Command, _ []string) error {
			request.Action = cmd.Name()
			if request.Action == "write" || request.Action == "restore" || request.Action == "delete" {
				preview, err := cliSavePreview(request)
				if err != nil {
					return err
				}
				if err := c.print(preview); err != nil {
					return err
				}
				risk := locales.SaveKitWriteTarget(request.TitleID, request.UID, "1", "1", "0", "0")
				if request.Archive != nil {
					a := request.Archive
					risk = locales.SaveKitWriteTarget(request.TitleID, request.UID, fmt.Sprint(a.Space), fmt.Sprint(a.Type), fmt.Sprint(a.Rank), fmt.Sprint(a.Index))
				}
				if err := c.consent(consent, "change-save", risk+"\n"+locales.LauncherConfirm()); err != nil {
					return err
				}
				request.Confirm, request.PreviewDigest = true, preview.Digest
			}
			if err := c.operation(c.app.controller.RunToolWith(session.ToolRequest{Tool: "savekit", SaveKit: &request})); err != nil {
				return err
			}
			if c.app.tools != nil {
				return c.print(c.app.tools.State())
			}
			return nil
		})
		cmd.Flags().StringVar(&request.TitleID, "title", "", locales.LauncherPackage())
		cmd.Flags().StringVar(&request.UID, "account", "", locales.LauncherAccount())
		cmd.Flags().StringVar(&request.SourceFolder, "source", "", cliMessage("cliFolder"))
		cmd.Flags().StringVar(&request.FileName, "file", "", cliMessage("cliFile"))
		cmd.Flags().StringVar(&request.ExpectedFolder, "expected", "", cliMessage("cliFolder"))
		cmd.Flags().BoolVar(&request.All, "all", false, locales.LogAll())
		cmd.Flags().BoolVar(&request.AllowReplace, "replace", false, locales.LauncherConfirm())
		var archive string
		cmd.Flags().StringVar(&archive, "archive", "", cliMessage("cliArchive"))
		cmd.PreRunE = func(*cobra.Command, []string) error {
			if archive != "" {
				var a savekit.Archive
				if err := decodeCLIJSON(archive, &a); err != nil {
					return err
				}
				request.Archive = &a
			}
			return nil
		}
		cmd.Flags().StringVar(&consent, "consent", "", cliMessage("cliConsent"))
	}
	var request savekit.Request
	preview := c.static(group, "preview ACTION", locales.LauncherPreview(), func(_ *cobra.Command, args []string) error {
		request.Action = args[0]
		p, err := cliSavePreview(request)
		if err != nil {
			return err
		}
		return c.print(p)
	})
	preview.Args = cobra.ExactArgs(1)
	preview.Flags().StringVar(&request.SourceFolder, "source", "", cliMessage("cliFolder"))
	preview.Flags().StringVar(&request.FileName, "file", "", cliMessage("cliFile"))
}
func cliSavePreview(request savekit.Request) (savekit.Preview, error) {
	switch request.Action {
	case "write":
		return savekit.PreviewWrite(request.SourceFolder)
	case "restore":
		return savekit.PreviewRestore(request.SourceFolder)
	case "delete":
		return savekit.PreviewDelete(request.FileName), nil
	default:
		return savekit.Preview{}, cliFail(exitBlocked, locales.CommandRejected())
	}
}

func (c *commandLine) addLaunchers(root *cobra.Command) {
	group := &cobra.Command{Use: "launcher", Short: locales.LauncherSection()}
	root.AddCommand(group)
	c.static(group, "list", locales.LauncherPackage(), func(*cobra.Command, []string) error {
		var entries []any
		for _, id := range launcher.IDs(c.app.manifest) {
			component, err := c.app.manifest.ToolComponent(id)
			if err != nil {
				return err
			}
			entries = append(entries, component)
		}
		return c.print(entries)
	})
	for _, action := range []string{"preview", "install"} {
		var request launcher.Request
		var account, consent string
		cmd := c.command(group, action+" PACKAGE", locales.LauncherSection(), true, func(cmd *cobra.Command, args []string) error {
			request.ID = args[0]
			component, err := c.app.manifest.ToolComponent(request.ID)
			if err != nil {
				return err
			}
			if component.Launcher == nil || c.app.tools == nil {
				return cliFail(exitBlocked, locales.CommandRejected())
			}
			target := savekit.Request{TitleID: component.Launcher.TitleID, UID: account}
			preview, err := c.app.tools.PreviewLauncher(context.Background(), request, target)
			if err != nil {
				return err
			}
			if err := c.print(preview); err != nil {
				return err
			}
			if cmd.Name() == "preview" {
				return nil
			}
			risk := strings.Join([]string{locales.LauncherRestrictions(), locales.LauncherSideEffects(), locales.LauncherExistingUntouched(),
				locales.SaveKitWriteTarget(target.TitleID, target.UID, "1", "1", "0", "0"), locales.LauncherConfirm()}, "\n")
			if err := c.consent(consent, "install-launcher-save", risk); err != nil {
				return err
			}
			target.Action, target.AllowReplace, target.Confirm, target.PreviewDigest = "write", true, true, preview.Digest
			return c.operation(c.app.controller.RunToolWith(session.ToolRequest{Tool: "savekit", SaveKit: &target, Launcher: &request}))
		})
		cmd.Args = cobra.ExactArgs(1)
		cmd.Flags().StringVar(&account, "account", "", locales.LauncherAccount())
		cmd.Flags().StringVar(&request.Version, "version", "", locales.LauncherVersion())
		cmd.Flags().BoolVar(&request.PhysicalCompatibility, "physical-cartridge", false, locales.LauncherPhysicalCartridge())
		cmd.Flags().StringVar(&request.BackupFolder, "backup-folder", "", locales.LauncherFullGameExport())
		cmd.Flags().StringVar(&consent, "consent", "", cliMessage("cliConsent"))
	}
}

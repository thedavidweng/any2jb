package cli

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

func (c *commandLine) addCivSwitch(root *cobra.Command) {
	group := &cobra.Command{Use: "civswitch", Short: locales.CivTitle()}
	root.AddCommand(group)
	var prerequisites session.CivSwitchPrerequisites
	var confirm string
	start := c.command(group, "launch", locales.StartJailbreak(), true, func(*cobra.Command, []string) error {
		risk := strings.Join([]string{locales.CivPrerequisites(), locales.CivNoRPC(), locales.CivComposite(), locales.CivDuration(), locales.CivProbe(),
			locales.CivCartridge(), locales.CivLanguage(), locales.CivNetwork(), locales.CivChat()}, "\n")
		if err := c.consent(confirm, "civswitch-prerequisites", risk); err != nil {
			return err
		}
		prerequisites.PhysicalSwitch1Cartridge100, prerequisites.EnglishUS, prerequisites.SameReachableNetwork, prerequisites.LobbyChatFocused = true, true, true, true
		return c.operation(c.app.Controller.StartCivSwitch(prerequisites))
	})
	start.Flags().StringVar(&prerequisites.ConsoleIPv4, "console", "", locales.ToolsConsoleAddress())
	start.Flags().StringVar(&confirm, "confirm", "", cliMessage("cliConfirm"))
	connect := c.command(group, "connect IPV4", locales.ToolsTitle(), true, func(_ *cobra.Command, args []string) error {
		return c.operation(c.app.Controller.ConnectCivSwitch(args[0]))
	})
	connect.Args = cobra.ExactArgs(1)
	c.command(group, "recheck", locales.CheckingReadiness(), true, func(*cobra.Command, []string) error { return c.operation(c.app.Controller.RecheckCivSwitch()) })
	c.command(group, "availability", locales.ToolsTitle(), false, func(*cobra.Command, []string) error {
		return c.print(app.CivSwitchUnavailableLocal(c.app.Controller))
	})
	var trigger string
	install := c.command(group, "install-trigger", locales.ToolsTitle(), true, func(*cobra.Command, []string) error {
		risk := strings.Join([]string{locales.CivPersistence(), locales.CivDependentNote(), locales.CivTriggerWarning(), locales.CivTriggerConsent()}, "\n")
		if err := c.consent(trigger, "replace-civswitch-saves", risk); err != nil {
			return err
		}
		return c.operation(c.app.Controller.InstallCivSwitchTrigger(true))
	})
	install.Flags().StringVar(&trigger, "consent", "", cliMessage("cliConsent"))
	var jsConsent string
	sendJS := c.command(group, "send-js FILE", locales.ToolsTitle(), true, func(_ *cobra.Command, args []string) error {
		risk := locales.CivCodeWarning() + "\n" + locales.CivCodeConsent()
		if err := c.consent(jsConsent, "run-javascript", risk); err != nil {
			return err
		}
		return c.operation(c.app.Controller.SendCivSwitchJavaScript(args[0], true))
	})
	sendJS.Args = cobra.ExactArgs(1)
	sendJS.Flags().StringVar(&jsConsent, "consent", "", cliMessage("cliConsent"))
	c.command(group, "leave", locales.CivBack(), true, func(*cobra.Command, []string) error {
		s := c.app.Controller.Snapshot()
		if !s.CivSwitch {
			return nil
		}
		return c.operation(c.app.Controller.SelectProfile(s.Profile, s.Edition))
	})
	c.addCivSwitchFSAndHomebrew(group)
}

func (c *commandLine) addCivSwitchFSAndHomebrew(group *cobra.Command) {
	fsGroup := &cobra.Command{Use: "fs", Short: locales.CivFSHostSection()}
	group.AddCommand(fsGroup)
	var hostIPv4 string
	var waitForLoader bool
	fsStart := c.command(fsGroup, "start", locales.CivFSStart(), true, func(*cobra.Command, []string) error {
		host := strings.TrimSpace(hostIPv4)
		if host == "" {
			host = c.app.Controller.Snapshot().LANIPv4
		}
		return c.operation(c.app.Controller.StartCivSwitchFSHost(host, waitForLoader))
	})
	fsStart.Flags().StringVar(&hostIPv4, "host", "", locales.CivFSHostAddress())
	fsStart.Flags().BoolVar(&waitForLoader, "wait", false, locales.CivFSWaitLoader())
	c.command(fsGroup, "stop", locales.CivFSStop(), true, func(*cobra.Command, []string) error {
		return c.operation(c.app.Controller.StopCivSwitchFSHost())
	})

	hbGroup := &cobra.Command{Use: "homebrew", Short: locales.CivFSHostSection()}
	group.AddCommand(hbGroup)
	c.command(hbGroup, "probe", locales.CivHBProbe(), true, func(*cobra.Command, []string) error {
		if err := c.operation(c.app.Controller.ProbeCivHomebrew()); err != nil {
			return err
		}
		return c.print(c.app.Controller.Snapshot().CivSwitchActions.CivHomebrew)
	})
	c.command(hbGroup, "launch", locales.CivHBLaunch(), true, func(*cobra.Command, []string) error {
		return c.operation(c.app.Controller.LaunchCivHomebrew())
	})
	c.command(hbGroup, "stop", locales.CivHBStop(), true, func(*cobra.Command, []string) error {
		return c.operation(c.app.Controller.StopCivHomebrew())
	})
	var hbInstallConsent string
	hbInstall := c.command(hbGroup, "install-trigger", locales.CivHBInstallTrigger(), true, func(*cobra.Command, []string) error {
		risk := locales.CivTriggerWarning() + "\n" + locales.CivHBTriggerConsent()
		if err := c.consent(hbInstallConsent, "replace-civswitch-saves", risk); err != nil {
			return err
		}
		return c.operation(c.app.Controller.InstallCivHomebrewTrigger(true))
	})
	hbInstall.Flags().StringVar(&hbInstallConsent, "consent", "", cliMessage("cliConsent"))
	var hbRestoreConsent string
	hbRestore := c.command(hbGroup, "restore-trigger", locales.CivHBRestoreTrigger(), true, func(*cobra.Command, []string) error {
		risk := locales.CivTriggerWarning() + "\n" + locales.CivHBTriggerConsent()
		if err := c.consent(hbRestoreConsent, "restore-civswitch-saves", risk); err != nil {
			return err
		}
		return c.operation(c.app.Controller.RestoreCivHomebrewTrigger())
	})
	hbRestore.Flags().StringVar(&hbRestoreConsent, "consent", "", cliMessage("cliConsent"))
	c.command(hbGroup, "shutdown", locales.CivHBShutdown(), true, func(*cobra.Command, []string) error {
		return c.operation(c.app.Controller.ShutdownCivHomebrew())
	})
	var pkgID, pkgFile string
	hbPkg := c.command(hbGroup, "install-package", locales.CivHBPkgInstall(), true, func(*cobra.Command, []string) error {
		spec := strings.TrimSpace(pkgFile)
		if spec == "" {
			spec = strings.TrimSpace(pkgID)
		}
		if spec == "" {
			return cliFail(exitError, cliMessage("cliInvalidArguments"))
		}
		return c.operation(c.app.Controller.InstallCivHomebrewPackage(spec))
	})
	hbPkg.Flags().StringVar(&pkgID, "package", "", locales.CivHBPkgSelect())
	hbPkg.Flags().StringVar(&pkgFile, "file", "", locales.CivHBPkgPath())
}

func (c *commandLine) addKeyboard(root *cobra.Command) {
	group := &cobra.Command{Use: "keyboard", Short: locales.UsbKeyboardTitle()}
	root.AddCommand(group)
	c.command(group, "check", locales.UsbApplySettings(), true, func(*cobra.Command, []string) error {
		if c.app.KeyboardInput == nil {
			return cliFail(exitBlocked, locales.UsbKeyboardUnavailable())
		}
		s := c.app.Controller.Snapshot()
		return c.app.KeyboardInput.Check(context.Background(), session.Run{ID: s.RunID, Profile: s.Profile, Edition: s.Edition, LANIPv4: s.LANIPv4, Transport: s.Transport})
	})
	c.command(group, "open", locales.UsbStartForwarding(), true, func(*cobra.Command, []string) error { return c.operation(c.app.Controller.StartKeyboard()) })
	report := c.command(group, "report HEX", locales.UsbKeyboardTitle(), true, func(_ *cobra.Command, args []string) error {
		raw, err := hex.DecodeString(args[0])
		if err != nil {
			return err
		}
		if len(raw) != 8 {
			return cliFail(exitError, cliMessage("cliInvalidArguments"))
		}
		var report [8]byte
		copy(report[:], raw)
		_, err = c.app.Controller.ForwardKeyboardReport(report)
		return err
	})
	report.Args = cobra.ExactArgs(1)
	c.command(group, "close", locales.UsbStopForwarding(), true, func(*cobra.Command, []string) error { return c.operation(c.app.Controller.CloseKeyboard()) })
	driver := &cobra.Command{Use: "driver", Short: locales.DriverSetup()}
	root.AddCommand(driver)
	c.command(driver, "status", locales.DriverTitle(), false, func(*cobra.Command, []string) error {
		c.say(strings.Join([]string{locales.DriverDisclosure(), locales.DriverSigning(), locales.DriverRecovery(), locales.DriverApproval(), locales.DriverRestart(), locales.DriverNoSecurityChange()}, "\n"))
		if c.app.DriverGate == nil {
			return cliFail(exitBlocked, locales.CommandRejected())
		}
		if err := c.app.DriverGate.Availability(); err != nil {
			return err
		}
		return nil
	})
	var consent string
	install := c.command(driver, "install", locales.DriverApprove(), true, func(*cobra.Command, []string) error {
		risk := strings.Join([]string{locales.DriverDisclosure(), locales.DriverSigning(), locales.DriverRecovery(), locales.DriverApproval(), locales.DriverRestart(), locales.DriverNoSecurityChange(), locales.DriverConfirmText()}, "\n")
		if err := c.consent(consent, "install-driver", risk); err != nil {
			return err
		}
		if c.app.DriverGate == nil {
			return cliFail(exitBlocked, locales.CommandRejected())
		}
		result, err := c.app.DriverGate.Install(context.Background(), true)
		if printErr := c.print(result); printErr != nil {
			return printErr
		}
		return err
	})
	install.Flags().StringVar(&consent, "consent", "", cliMessage("cliConsent"))
}

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
	for _, action := range []string{"send-js", "stage-js"} {
		var consent string
		cmd := c.command(group, action+" FILE", locales.ToolsTitle(), true, func(cmd *cobra.Command, args []string) error {
			risk := locales.CivCodeWarning() + "\n" + locales.CivCodeConsent()
			if err := c.consent(consent, "run-javascript", risk); err != nil {
				return err
			}
			if cmd.Name() == "send-js" {
				return c.operation(c.app.Controller.SendCivSwitchJavaScript(args[0], true))
			}
			return c.operation(c.app.Controller.StageCivSwitchJavaScript(args[0], true))
		})
		cmd.Args = cobra.ExactArgs(1)
		cmd.Flags().StringVar(&consent, "consent", "", cliMessage("cliConsent"))
	}
	c.command(group, "control-center", locales.ToolsTitle(), true, func(*cobra.Command, []string) error {
		return c.operation(c.app.Controller.StartCivSwitchControlCenter())
	})
	c.command(group, "leave", locales.CivBack(), true, func(*cobra.Command, []string) error {
		s := c.app.Controller.Snapshot()
		if !s.CivSwitch {
			return nil
		}
		return c.operation(c.app.Controller.SelectProfile(s.Profile, s.Edition))
	})
	c.command(group, "stop-control-center", locales.ToolsStop(), true, func(*cobra.Command, []string) error {
		return c.operation(c.app.Controller.StopCivSwitchControlCenter())
	})
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

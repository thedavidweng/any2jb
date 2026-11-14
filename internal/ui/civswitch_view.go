package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

type civSwitchPanel struct {
	open             bool
	saveConsent      bool
	codeConsent      bool
	openFailed       bool
	javaScript       string
	fsHostAddress    string
	fsWaitForLoader  bool
	hbTriggerConsent bool
	hbPackage        string
	hbPackageFile    string
	prerequisites    session.CivSwitchPrerequisites
}

func (a *app) civSwitchView(c *ui.Context) {
	p := &a.civSwitchPanel
	s := a.Controller.Snapshot()
	sectionTitle(c, locales.CivTitle())
	a.restartView(c, s)
	ui.Text(c, locales.CivPrerequisites())
	ui.Text(c, locales.CivNoRPC()).Bold()
	ui.Text(c, locales.CivComposite())
	ui.Text(c, locales.CivDuration())
	ui.Text(c, locales.CivProbe())
	unavailable := appstate.CivSwitchUnavailableLocal(a.Controller)
	if unavailable != "" {
		ui.Text(c, unavailable)
	}
	if a.flow.stage != flowLaunch && a.flow.stage != flowTools {
		ui.TextInput(c.Key("civ-address"), &p.prerequisites.ConsoleIPv4).Label(locales.CivAddress()).Disabled(s.Busy)
	}
	eligible := !s.Busy && (s.Stage == session.Idle || s.Stage == session.Ready || s.Stage == session.NeedsSetup || s.Stage == session.Failed || s.Stage == session.Finished)
	confirmed := p.prerequisites.PhysicalSwitch1Cartridge100 && p.prerequisites.EnglishUS && p.prerequisites.SameReachableNetwork && p.prerequisites.LobbyChatFocused
	composite := s.Transport.Capabilities.Keyboard && s.Transport.Capabilities.Gamepad
	if a.flow.stage == flowLaunch || a.flow.stage == flowDetection {
		if inline(ui.PrimaryButton(c.Key("civ-start"), locales.CivStart())).Disabled(!eligible || unavailable != "" || !confirmed || !composite).Clicked() {
			a.command(a.Controller.StartCivSwitch(p.prerequisites))
		}
	}
	if a.flow.stage != flowTools {
		if inline(ui.Button(c.Key("civ-attach"), locales.CivAttach())).Disabled(!eligible || unavailable != "").Clicked() {
			a.command(a.Controller.ConnectCivSwitch(p.prerequisites.ConsoleIPv4))
		}
	}
	if s.CivSwitch {
		if s.LoaderVerified {
			ui.Text(c, locales.CivVerified()).Bold()
		} else {
			ui.Text(c, appstate.StageTitle(s.Stage))
		}
		if s.EstimatedDuration > 0 {
			ui.Text(c, locales.CivEstimate(fmt.Sprint(int(s.EstimatedDuration.Seconds()))))
		}
		if s.Total > 0 {
			ui.Text(c, fmt.Sprintf("%s: %d / %d", locales.Progress(), s.Current, s.Total))
		}
		if s.Stage == session.Failed {
			ui.Text(c, locales.SafeFailure())
		}
		if s.Busy && s.Stage != session.Verified {
			if inline(ui.Button(c.Key("civ-cancel"), locales.Cancel())).Disabled(s.SideEffects).Clicked() {
				a.command(a.Controller.Cancel())
			}
			if inline(ui.Button(c.Key("civ-stop"), locales.CivStop())).Disabled(!s.SideEffects).Clicked() {
				a.command(a.Controller.Stop())
			}
		}
		if inline(ui.Button(c.Key("civ-recheck"), locales.CivRecheck())).Disabled(s.Busy || (s.Stage != session.Indeterminate && s.Stage != session.Failed && s.Stage != session.Verified) || unavailable != "").Clicked() {
			a.command(a.Controller.RecheckCivSwitch())
		}
		if s.Stage == session.Verified {
			a.civSwitchActionsView(c, s)
			ui.Text(c, locales.CivDisconnectNote())
			if inline(ui.Button(c.Key("civ-finish"), locales.CivFinish())).Disabled(s.Busy).Clicked() {
				a.command(a.Controller.Finish())
			}
		}
	}
	if a.commandRejected {
		ui.Text(c, locales.CommandRejected())
	}
	if inline(ui.Button(c.Key("civ-back"), locales.CivBack())).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate || (s.CivSwitch && s.Stage == session.Verified)).Clicked() {
		if s.CivSwitch {
			a.command(a.Controller.SelectProfile(s.Profile, s.Edition))
		}
		p.open = false
		a.flow.stage = flowPrepare
	}
}

func (a *app) civSwitchActionsView(c *ui.Context, s session.Snapshot) {
	p := &a.civSwitchPanel
	if p.hbPackage == "" {
		p.hbPackage = "moonlight-civswitch"
	}
	actions := s.CivSwitchActions
	ui.Text(c, locales.CivPersistence()).Bold()
	ui.Text(c, locales.CivDependentNote())
	ui.Text(c, locales.CivTriggerWarning())
	ui.Checkbox(c.Key("civ-consent-saves"), &p.saveConsent, locales.CivTriggerConsent()).Disabled(s.Busy)
	if inline(ui.Button(c.Key("civ-trigger"), locales.CivTrigger())).
		Disabled(s.Busy || !p.saveConsent || actions.TriggerInstalled).Clicked() {
		a.command(a.Controller.InstallCivSwitchTrigger(p.saveConsent))
	}
	if actions.TriggerInstalled {
		ui.Text(c, locales.CivTriggerInstalled())
	}
	if actions.BackupPath != "" {
		ui.Text(c, locales.CivBackupSaved(actions.BackupPath))
		if inline(ui.Button(c.Key("civ-backup-open"), locales.CivBackupOpen())).Clicked() {
			p.openFailed = mygo.Shell.OpenPath(actions.BackupPath) != nil
		}
	}
	ui.Text(c, locales.CivCodeWarning()).Bold()
	ui.TextInput(c.Key("civ-js-file"), &p.javaScript).Label(locales.CivJSFile()).Disabled(s.Busy)
	ui.Checkbox(c.Key("civ-consent-code"), &p.codeConsent, locales.CivCodeConsent()).Disabled(s.Busy)
	code := !s.Busy && p.codeConsent && strings.TrimSpace(p.javaScript) != ""
	if inline(ui.Button(c.Key("civ-js"), locales.CivJSSend())).Disabled(!code).Clicked() {
		a.command(a.Controller.SendCivSwitchJavaScript(strings.TrimSpace(p.javaScript), p.codeConsent))
	}

	rule(c)
	ui.Text(c, locales.CivFSHostSection()).Bold()
	ui.Text(c, locales.CivFSHostIntro())
	ui.TextInput(c.Key("civ-fs-host"), &p.fsHostAddress).Label(locales.CivFSHostAddress()).Disabled(s.Busy || actions.FSHostLive)
	ui.Checkbox(c.Key("civ-fs-wait"), &p.fsWaitForLoader, locales.CivFSWaitLoader()).Disabled(s.Busy || actions.FSHostLive)
	ui.Row(c).Wrap().Gap(8).Children(func() {
		if ui.Button(c.Key("civ-fs-start"), locales.CivFSStart()).Disabled(s.Busy || actions.FSHostLive || actions.CivHomebrewLive).Clicked() {
			hostIP := strings.TrimSpace(p.fsHostAddress)
			if hostIP == "" {
				hostIP = a.defaultLocalIPv4()
			}
			a.command(a.Controller.StartCivSwitchFSHost(hostIP, p.fsWaitForLoader))
		}
		if actions.FSHostLive {
			if ui.Button(c.Key("civ-fs-stop"), locales.CivFSStop()).Clicked() {
				a.command(a.Controller.StopCivSwitchFSHost())
			}
		}
	})
	if actions.FSHostLive {
		ui.Text(c, locales.CivFSLive(actions.FSRunPath))
	}

	rule(c)
	ui.Row(c).Wrap().Gap(8).Children(func() {
		if ui.Button(c.Key("civ-hb-probe"), locales.CivHBProbe()).Disabled(s.Busy).Clicked() {
			a.command(a.Controller.ProbeCivHomebrew())
		}
		if ui.Button(c.Key("civ-hb-launch"), locales.CivHBLaunch()).Disabled(s.Busy || actions.CivHomebrewLive || actions.FSHostLive).Clicked() {
			a.command(a.Controller.LaunchCivHomebrew())
		}
		if actions.CivHomebrewLive {
			if ui.Button(c.Key("civ-hb-stop"), locales.CivHBStop()).Clicked() {
				a.command(a.Controller.StopCivHomebrew())
			}
		}
		if ui.Button(c.Key("civ-hb-shutdown"), locales.CivHBShutdown()).Disabled(s.Busy).Clicked() {
			a.command(a.Controller.ShutdownCivHomebrew())
		}
	})
	if actions.CivHomebrew.Mode != "" || actions.CivHomebrew.Launcher || actions.CivHomebrew.FTP {
		ui.Text(c, locales.CivHBStatusFormat(actions.CivHomebrew.Mode, fmt.Sprint(actions.CivHomebrew.Launcher)))
	}
	ui.Checkbox(c.Key("civ-hb-consent"), &p.hbTriggerConsent, locales.CivHBTriggerConsent()).Disabled(s.Busy)
	ui.Row(c).Wrap().Gap(8).Children(func() {
		if ui.Button(c.Key("civ-hb-install-trigger"), locales.CivHBInstallTrigger()).Disabled(s.Busy || !p.hbTriggerConsent).Clicked() {
			a.command(a.Controller.InstallCivHomebrewTrigger(p.hbTriggerConsent))
		}
		if ui.Button(c.Key("civ-hb-restore-trigger"), locales.CivHBRestoreTrigger()).Disabled(s.Busy || !p.hbTriggerConsent).Clicked() {
			a.command(a.Controller.RestoreCivHomebrewTrigger())
		}
	})
	packages := []string{"moonlight-civswitch", "sw2nes", "jellyfin-civswitch", "custom"}
	ui.Select(c.Key("civ-hb-pkg-select"), &p.hbPackage, packages).Label(locales.CivHBPkgSelect()).Disabled(s.Busy)
	if p.hbPackage == "custom" {
		ui.TextInput(c.Key("civ-hb-pkg-file"), &p.hbPackageFile).Label(locales.CivHBPkgPath()).Disabled(s.Busy)
	}
	needsPkgFile := p.hbPackage == "custom" && strings.TrimSpace(p.hbPackageFile) == ""
	if inline(ui.Button(c.Key("civ-hb-pkg-install"), locales.CivHBPkgInstall())).Disabled(s.Busy || needsPkgFile).Clicked() {
		spec := p.hbPackage
		if spec == "custom" {
			spec = strings.TrimSpace(p.hbPackageFile)
		}
		a.command(a.Controller.InstallCivHomebrewPackage(spec))
	}

	if p.openFailed {
		ui.Text(c, locales.CivOpenFailed())
	}
}

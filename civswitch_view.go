package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

type civSwitchPanel struct {
	open          bool
	saveConsent   bool
	codeConsent   bool
	openFailed    bool
	javaScript    string
	prerequisites session.CivSwitchPrerequisites
}

func (a *app) civSwitchView(c *ui.Context) {
	p := &a.civSwitchPanel
	s := a.controller.Snapshot()
	sectionTitle(c, locales.CivTitle())
	a.restartView(c, s)
	ui.Text(c, locales.CivPrerequisites())
	ui.Text(c, locales.CivNoRPC()).Bold()
	ui.Text(c, locales.CivComposite())
	ui.Text(c, locales.CivDuration())
	ui.Text(c, locales.CivProbe())
	unavailable := a.controller.CivSwitchUnavailable()
	if unavailable != "" {
		ui.Text(c, localize(unavailable))
	}
	if a.flow.stage != flowLaunch && a.flow.stage != flowTools {
		ui.TextInput(c.Key("civ-address"), &p.prerequisites.ConsoleIPv4).Label(locales.CivAddress()).Disabled(s.Busy)
		ui.Checkbox(c.Key("civ-cartridge"), &p.prerequisites.PhysicalSwitch1Cartridge100, locales.CivCartridge()).Disabled(s.Busy)
		ui.Checkbox(c.Key("civ-language"), &p.prerequisites.EnglishUS, locales.CivLanguage()).Disabled(s.Busy)
		ui.Checkbox(c.Key("civ-network"), &p.prerequisites.SameReachableNetwork, locales.CivNetwork()).Disabled(s.Busy)
		ui.Checkbox(c.Key("civ-chat"), &p.prerequisites.LobbyChatFocused, locales.CivChat()).Disabled(s.Busy)
	}
	eligible := !s.Busy && (s.Stage == session.Idle || s.Stage == session.Ready || s.Stage == session.NeedsSetup || s.Stage == session.Failed || s.Stage == session.Finished)
	confirmed := p.prerequisites.PhysicalSwitch1Cartridge100 && p.prerequisites.EnglishUS && p.prerequisites.SameReachableNetwork && p.prerequisites.LobbyChatFocused
	composite := s.Transport.Capabilities.Keyboard && s.Transport.Capabilities.Gamepad
	if a.flow.stage == flowLaunch || a.flow.stage == flowDetection {
		if inline(ui.PrimaryButton(c.Key("civ-start"), locales.CivStart())).Disabled(!eligible || unavailable != "" || !confirmed || !composite).Clicked() {
			a.command(a.controller.StartCivSwitch(p.prerequisites))
		}
	}
	if a.flow.stage != flowTools {
		if inline(ui.Button(c.Key("civ-attach"), locales.CivAttach())).Disabled(!eligible || unavailable != "").Clicked() {
			a.command(a.controller.ConnectCivSwitch(p.prerequisites.ConsoleIPv4))
		}
	}
	if s.CivSwitch {
		if s.LoaderVerified {
			ui.Text(c, locales.CivVerified()).Bold()
		} else {
			ui.Text(c, stageTitle(s.Stage))
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
				a.command(a.controller.Cancel())
			}
			if inline(ui.Button(c.Key("civ-stop"), locales.CivStop())).Disabled(!s.SideEffects).Clicked() {
				a.command(a.controller.Stop())
			}
		}
		if inline(ui.Button(c.Key("civ-recheck"), locales.CivRecheck())).Disabled(s.Busy || (s.Stage != session.Indeterminate && s.Stage != session.Failed && s.Stage != session.Verified) || unavailable != "").Clicked() {
			a.command(a.controller.RecheckCivSwitch())
		}
		if s.Stage == session.Verified {
			a.civSwitchActionsView(c, s)
			ui.Text(c, locales.CivDisconnectNote())
			if inline(ui.Button(c.Key("civ-finish"), locales.CivFinish())).Disabled(s.Busy).Clicked() {
				a.command(a.controller.Finish())
			}
		}
	}
	if a.commandRejected {
		ui.Text(c, locales.CommandRejected())
	}
	if inline(ui.Button(c.Key("civ-back"), locales.CivBack())).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate || (s.CivSwitch && s.Stage == session.Verified)).Clicked() {
		// Selecting a host explicitly ends the separate route; no loader proof is
		// carried into a SmileBASIC session.
		if s.CivSwitch {
			a.command(a.controller.SelectProfile(s.Profile, s.Edition))
		}
		p.open = false
		a.flow.stage = flowSelection
	}
}

// Every action here writes to the console or runs code on it, so each carries
// its own confirmation and rechecks the loader. The read-only Control Center
// and its save export need no code consent.
func (a *app) civSwitchActionsView(c *ui.Context, s session.Snapshot) {
	p := &a.civSwitchPanel
	actions := s.CivSwitchActions
	ui.Text(c, locales.CivPersistence()).Bold()
	ui.Text(c, locales.CivDependentNote())
	ui.Text(c, locales.CivTriggerWarning())
	ui.Checkbox(c.Key("civ-consent-saves"), &p.saveConsent, locales.CivTriggerConsent()).Disabled(s.Busy)
	if inline(ui.Button(c.Key("civ-trigger"), locales.CivTrigger())).
		Disabled(s.Busy || !p.saveConsent || actions.TriggerInstalled).Clicked() {
		a.command(a.controller.InstallCivSwitchTrigger(p.saveConsent))
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
	code := !s.Busy && p.codeConsent && p.javaScript != ""
	if inline(ui.Button(c.Key("civ-js"), locales.CivJSSend())).Disabled(!code).Clicked() {
		a.command(a.controller.SendCivSwitchJavaScript(p.javaScript, p.codeConsent))
	}
	ui.Text(c, locales.CivControlStageNote())
	if inline(ui.Button(c.Key("civ-control-stage"), locales.CivControlStage())).Disabled(!code).Clicked() {
		a.command(a.controller.StageCivSwitchJavaScript(p.javaScript, p.codeConsent))
	}
	ui.Text(c, locales.CivControlNote())
	if inline(ui.Button(c.Key("civ-control-start"), locales.CivControlStart())).Disabled(s.Busy).Clicked() {
		a.command(a.controller.StartCivSwitchControlCenter())
	}
	if actions.ControlCenterLive {
		ui.Text(c, locales.CivControlLive())
		if inline(ui.Button(c.Key("civ-control-stop"), locales.CivControlStop())).Clicked() {
			a.command(a.controller.StopCivSwitchControlCenter())
		}
	}
	if actions.ExportPath != "" {
		ui.Text(c, locales.CivExportSaved(actions.ExportPath))
		if inline(ui.Button(c.Key("civ-export-open"), locales.CivExportOpen())).Clicked() {
			p.openFailed = mygo.Shell.OpenPath(actions.ExportPath) != nil
		}
	}
	if p.openFailed {
		ui.Text(c, locales.CivOpenFailed())
	}
}

package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

type civSwitchPanel struct {
	open          bool
	prerequisites session.CivSwitchPrerequisites
}

func (a *app) civSwitchView(c *ui.Context) {
	p := &a.civSwitchPanel
	s := a.controller.Snapshot()
	ui.Text(c, locales.CivTitle()).FontSize(20).Bold()
	a.restartView(c, s)
	ui.Text(c, locales.CivPrerequisites())
	ui.Text(c, locales.CivNoRPC()).Bold()
	ui.Text(c, locales.CivComposite())
	ui.Text(c, locales.CivDuration())
	ui.Text(c, locales.CivProbe())
	unavailable := a.controller.CivSwitchUnavailable()
	if unavailable != "" {
		ui.Text(c, unavailable)
	}
	ui.TextInput(c.Key("civ-address"), &p.prerequisites.ConsoleIPv4).Label(locales.CivAddress()).Disabled(s.Busy)
	ui.Checkbox(c.Key("civ-cartridge"), &p.prerequisites.PhysicalSwitch1Cartridge100, locales.CivCartridge()).Disabled(s.Busy)
	ui.Checkbox(c.Key("civ-language"), &p.prerequisites.EnglishUS, locales.CivLanguage()).Disabled(s.Busy)
	ui.Checkbox(c.Key("civ-network"), &p.prerequisites.SameReachableNetwork, locales.CivNetwork()).Disabled(s.Busy)
	ui.Checkbox(c.Key("civ-chat"), &p.prerequisites.LobbyChatFocused, locales.CivChat()).Disabled(s.Busy)
	eligible := !s.Busy && (s.Stage == session.Idle || s.Stage == session.Ready || s.Stage == session.NeedsSetup || s.Stage == session.Failed || s.Stage == session.Finished)
	confirmed := p.prerequisites.PhysicalSwitch1Cartridge100 && p.prerequisites.EnglishUS && p.prerequisites.SameReachableNetwork && p.prerequisites.LobbyChatFocused
	composite := s.Transport.Capabilities.Keyboard && s.Transport.Capabilities.Gamepad
	if ui.PrimaryButton(c.Key("civ-start"), locales.CivStart()).Disabled(!eligible || unavailable != "" || !confirmed || !composite).Clicked() {
		a.command(a.controller.StartCivSwitch(p.prerequisites))
	}
	if ui.Button(c.Key("civ-attach"), locales.CivAttach()).Disabled(!eligible || unavailable != "").Clicked() {
		a.command(a.controller.ConnectCivSwitch(p.prerequisites.ConsoleIPv4))
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
		if s.Busy {
			if ui.Button(c.Key("civ-cancel"), locales.Cancel()).Disabled(s.SideEffects).Clicked() {
				a.command(a.controller.Cancel())
			}
			if ui.Button(c.Key("civ-stop"), locales.CivStop()).Disabled(!s.SideEffects).Clicked() {
				a.command(a.controller.Stop())
			}
		}
		if ui.Button(c.Key("civ-recheck"), locales.CivRecheck()).Disabled(s.Busy || (s.Stage != session.Indeterminate && s.Stage != session.Failed && s.Stage != session.Verified) || unavailable != "").Clicked() {
			a.command(a.controller.RecheckCivSwitch())
		}
		if s.Stage == session.Verified {
			ui.Text(c, locales.CivDisconnectNote())
			if ui.Button(c.Key("civ-finish"), locales.CivFinish()).Disabled(s.Busy).Clicked() {
				a.command(a.controller.Finish())
			}
		}
	}
	if a.commandRejected {
		ui.Text(c, locales.CommandRejected())
	}
	if ui.Button(c.Key("civ-back"), locales.CivBack()).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate || (s.CivSwitch && s.Stage == session.Verified)).Clicked() {
		// Selecting a host explicitly ends the separate route; no loader proof is
		// carried into a SmileBASIC session.
		if s.CivSwitch {
			a.command(a.controller.SelectProfile(s.Profile, s.Edition))
		}
		p.open = false
	}
}

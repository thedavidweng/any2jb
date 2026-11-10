package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

func (a *app) command(_ session.Snapshot, err error) {
	a.commandRejected = err != nil
}

func (a *app) sessionView(c *ui.Context) {
	s := a.controller.Snapshot()
	if s.Simulated {
		ui.Text(c, locales.SimulatedEnvironment()).TextColor(c.Theme().TextMuted)
	}
	ui.Text(c, stageTitle(s.Stage)).FontSize(20).Bold()
	if s.Stage == session.Indeterminate {
		ui.Text(c, locales.RestartConsole())
	} else if s.Stage == session.Failed {
		ui.Text(c, locales.SafeFailure())
	}
	for _, check := range s.Checks {
		ui.Text(c.Key(check.ID+"-message"), check.Message)
		if check.NextAction != "" {
			ui.Text(c.Key(check.ID+"-action"), check.NextAction)
		}
	}
	if s.Total > 0 {
		ui.Text(c, fmt.Sprintf("%s: %d / %d", locales.Progress(), s.Current, s.Total))
	}
	if a.commandRejected {
		ui.Text(c, locales.CommandRejected())
	}
	ui.Row(c).Gap(12).Children(func() {
		if ui.Button(c.Key("inspect"), locales.CheckReadiness()).
			Disabled(s.Busy || s.Stage == session.Indeterminate || s.SideEffects).Clicked() {
			a.command(a.controller.Inspect())
		}
		label := locales.StartJailbreak()
		if s.Transport.Capabilities.Manual {
			label = locales.GuidedManualLaunch()
		}
		if ui.PrimaryButton(c.Key("start"), label).Disabled(!s.CanStart()).Clicked() {
			a.command(a.controller.Start())
		}
	})
	if (s.Busy && s.Stage != session.Verified && s.Stage != session.RetainedRPC && s.Stage != session.Finished) || s.Stage == session.BootstrapSetup {
		ui.Row(c).Gap(12).Children(func() {
			if ui.Button(c.Key("cancel"), locales.Cancel()).Disabled(s.SideEffects).Clicked() {
				a.command(a.controller.Cancel())
			}
			if ui.Button(c.Key("stop"), locales.Stop()).Disabled(!s.SideEffects).Clicked() {
				a.command(a.controller.Stop())
			}
		})
	}
	if (s.Stage == session.NeedsSetup || s.Stage == session.Ready) && !s.Busy {
		if ui.Button(c.Key("bootstrap"), locales.PrepareBootstrap()).Clicked() {
			a.command(a.controller.PrepareBootstrap())
		}
	}
	if s.Stage == session.BootstrapSetup && !s.Busy && s.BootstrapPrepared {
		if !s.SaveSubmitted {
			if ui.Button(c.Key("save"), locales.SubmitSave()).Clicked() {
				a.command(a.controller.SaveBootstrap(false))
			}
		} else {
			ui.Text(c, locales.SavePrompt())
			if ui.Button(c.Key("confirm-save"), locales.ConfirmSaved()).Clicked() {
				a.command(a.controller.SaveBootstrap(true))
			}
		}
	}
	if s.Stage == session.Verified || s.Stage == session.RetainedRPC {
		if ui.Button(c.Key("finish"), locales.Finish()).Disabled(s.Busy).Clicked() {
			a.command(a.controller.Finish())
		}
	}
}

func stageTitle(stage session.Stage) string {
	switch stage {
	case session.Idle:
		return locales.Home()
	case session.Inspecting:
		return locales.CheckingReadiness()
	case session.NeedsSetup:
		return locales.SetupNeedsAttention()
	case session.Ready:
		return locales.ReadyToLaunch()
	case session.BootstrapSetup:
		return locales.BootstrapSetup()
	case session.Verified:
		return locales.VerifiedSession()
	case session.RetainedRPC:
		return locales.RetainedSession()
	case session.Finished:
		return locales.SessionFinished()
	case session.Failed:
		return locales.SessionFailed()
	case session.Indeterminate:
		return locales.RecoveryRequired()
	default:
		return locales.SessionInProgress()
	}
}

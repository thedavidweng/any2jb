package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

func (a *app) command(s session.Snapshot, err error) {
	a.commandRejected = err != nil
	a.commandInstruction = ""
	if rejection, ok := err.(*session.Rejection); ok {
		switch rejection.Reason {
		case session.EditionUnsupported:
			a.commandInstruction = locales.UnsupportedEdition()
		case session.BootstrapMismatch, session.BootstrapUnconfirmed:
			a.commandInstruction = locales.MatchingBootstrapRequired(s.Profile.ID, s.Edition, s.Profile.SaveName)
		case session.KeyboardUnavailable:
			a.commandInstruction = locales.UsbKeyboardUnavailable()
		}
	}
}

func (a *app) sessionView(c *ui.Context) {
	if a.keyboard.open {
		a.keyboardView(c)
		if a.keyboard.forwarding || a.keyboard.completion != nil {
			return
		}
	}
	s := a.controller.Snapshot()
	pending := a.manifest != nil && (s.Profile.ID != a.preferences.Profile ||
		s.Edition != a.preferences.Edition || s.LANOverride != a.preferences.LANIPv4 || s.Transport.Mode != a.preferences.Transport)
	if pending {
		ui.Text(c, locales.ApplyBeforeLaunch())
	}
	if s.Simulated {
		ui.Text(c, locales.SimulatedEnvironment()).TextColor(c.Theme().TextMuted)
	}
	ui.Text(c, stageTitle(s.Stage)).FontSize(20).Bold()
	a.manualView(c, s)
	a.usbConfirmation.view(c)
	if s.CleanupConfirmed {
		ui.Text(c, locales.ResourcesReleased())
	}
	if s.Stage == session.Verified {
		ui.Text(c, locales.VerificationEvidence())
	} else if s.Stage == session.RetainedRPC {
		ui.Text(c, locales.HeldVerificationEvidence())
	}
	if s.Profile.BootstrapSHA256 != "" && !s.BootstrapSaved {
		ui.Text(c, locales.MatchingBootstrapRequired(s.Profile.ID, s.Edition, s.Profile.SaveName))
		if !s.Busy && !s.SideEffects && !pending && (s.Stage == session.Idle || s.Stage == session.Ready || s.Stage == session.NeedsSetup) {
			if ui.Button(c.Key("bootstrap-reuse"), locales.UseSavedBootstrap()).Clicked() {
				a.bootstrapReuse = true
			}
			if a.bootstrapReuse {
				ui.Text(c, locales.ConfirmBootstrapIdentity(s.Profile.ID, s.Edition, s.Profile.SaveName, s.Profile.Revision, s.Profile.BootstrapSHA256))
				if ui.Button(c.Key("bootstrap-reuse-confirm"), locales.ConfirmBootstrapIdentityAction()).Clicked() {
					a.bootstrapError = a.confirmExistingBootstrap() != nil
					a.bootstrapReuse = false
				}
			}
		}
	}
	if a.bootstrapError {
		ui.Text(c, locales.BootstrapPersistenceError())
	}
	a.restartView(c, s)
	if s.Stage == session.Failed {
		ui.Text(c, locales.SafeFailure())
	}
	for _, check := range s.Checks {
		ui.Text(c.Key(check.ID+"-state"), checkStateTitle(check.State)).Bold()
		if check.GatesTransport == "usb-hid" {
			ui.Text(c, locales.UsbOnly()).TextColor(c.Theme().TextMuted)
		}
		message := check.Message
		if check.State == session.CheckVerified && s.Transport.Controller != "" &&
			(check.ID == "usb-controllers" || check.Controller == s.Transport.Controller) {
			message = locales.UsbEnumerationObserved(s.Transport.Controller)
		}
		ui.Text(c.Key(check.ID+"-message"), message)
		if check.NextAction != "" {
			ui.Text(c.Key(check.ID+"-action"), check.NextAction)
		}
	}
	if s.Total > 0 {
		ui.Text(c, fmt.Sprintf("%s: %d / %d", locales.Progress(), s.Current, s.Total))
		if s.HasEstimate && s.Busy {
			ui.Text(c, locales.TypingRemaining(fmt.Sprint((s.Remaining.Milliseconds()+999)/1000))).TextColor(c.Theme().TextMuted)
		}
	}
	if a.manifest != nil {
		for _, check := range s.Checks {
			if check.ID == "driver-present" || check.ID == "driver-loaded" || check.ID == "driver-restart" {
				if ui.Button(c.Key("driver-setup"), locales.DriverSetup()).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate).Clicked() {
					a.driverPanel.open = true
				}
				break
			}
		}
	}
	if ui.Button(c.Key("usb-keyboard"), locales.UsbKeyboardTitle()).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate).Clicked() {
		a.keyboard.open = true
	}
	if a.commandRejected {
		message := a.commandInstruction
		if message == "" {
			message = locales.CommandRejected()
		}
		ui.Text(c, message)
	}
	ui.Row(c).Gap(12).Children(func() {
		if ui.Button(c.Key("inspect"), locales.CheckReadiness()).
			Disabled(pending || s.Busy || s.Stage == session.Indeterminate || s.SideEffects).Clicked() {
			a.command(a.controller.Inspect())
		}
		label := locales.StartJailbreak()
		if s.Transport.Capabilities.Manual || (a.manual != nil && !s.CanStart()) {
			label = locales.GuidedManualLaunch()
		}
		if ui.PrimaryButton(c.Key("start"), label).Disabled(pending || !s.CanStart()).Clicked() {
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
		if ui.Button(c.Key("bootstrap"), locales.PrepareBootstrap()).Disabled(pending).Clicked() {
			a.allowOverwrite = false
			a.command(a.controller.PrepareBootstrap())
		}
	}
	if s.Stage == session.BootstrapSetup && !s.Busy && s.BootstrapPrepared {
		if !s.SaveSubmitted {
			ui.Checkbox(c.Key("bootstrap-overwrite"), &a.allowOverwrite, locales.AllowBootstrapOverwrite())
			if ui.Button(c.Key("save"), locales.SubmitSave()).Disabled(!a.allowOverwrite).Clicked() {
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
	if a.manifest != nil {
		ui.Text(c, locales.ReferencePrerequisites()).Bold()
		ui.Text(c, locales.ConsoleFirmware())
		ui.Text(c, locales.SmilebasicPrerequisites())
		ui.Text(c, locales.NetworkPrerequisites())
		ui.Text(c, locales.CablePrerequisites())
		ui.Text(c, locales.DriverSecurityGuidance()).TextColor(c.Theme().TextMuted)
	}
}

func checkStateTitle(state session.CheckState) string {
	switch state {
	case session.CheckVerified:
		return locales.CheckVerified()
	case session.Available:
		return locales.CheckAvailable()
	default:
		return locales.CheckAttention()
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

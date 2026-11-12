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
	pending := a.preferencesPending(s)
	if pending {
		ui.Text(c, locales.ApplyBeforeLaunch()).Bold().TextColor(c.Theme().Warning)
	}
	stage := ui.Text(c.Key("session-stage"), stageTitle(s.Stage)).Role(ui.RoleStatus)
	if a.flow.stage == flowSelection {
		// The card around this view carries the section's title, so the
		// session state is a line in it, not a second heading.
		stage.TextColor(c.Theme().TextMuted)
	} else {
		stage.FontSize(c.Theme().Rem(1.25)).Bold()
	}
	// A long-lived tool child keeps the retained_rpc stage, so its ownership
	// is stated next to the stage.
	if a.tools != nil {
		if owner := a.tools.State().Tool; owner != "" {
			ui.Text(c.Key("session-tool-owner"), locales.ToolRunning(owner)).Role(ui.RoleStatus)
		}
	}
	if a.flow.stage == flowLaunch {
		a.launchStepsView(c, s)
	}
	a.manualView(c, s)
	a.usbConfirmation.view(c)
	if s.CleanupConfirmed {
		ui.Text(c, locales.ResourcesReleased())
	}
	if s.Stage == session.Verified || s.Stage == session.RetainedRPC {
		summary, evidence := locales.VerifiedSummary(), locales.VerificationEvidence()
		if s.Stage == session.RetainedRPC {
			summary, evidence = locales.VerifiedHeldSummary(), locales.HeldVerificationEvidence()
		}
		stateCard(c, toneVerified, func() {
			badge(c, "session-verified-badge", toneVerified, locales.CheckVerified())
			ui.Text(c, summary)
			ui.Collapsible(c.Key("verified-details"), locales.DetailsLabel(), &a.flow.evidence, func() {
				panelBody(c, func() {
					ui.Text(c, evidence)
					if s.Stage == session.RetainedRPC {
						muted(c, locales.ToolsLeaseDetail())
					}
				})
			})
		})
	}
	if a.flow.stage == flowSelection && s.Profile.BootstrapSHA256 != "" && !s.BootstrapSaved {
		ui.Text(c, locales.MatchingBootstrapRequired(s.Profile.ID, s.Edition, s.Profile.SaveName))
		if !s.Busy && !s.SideEffects && !pending && (s.Stage == session.Idle || s.Stage == session.Ready || s.Stage == session.NeedsSetup) {
			if inline(ui.Button(c.Key("bootstrap-reuse"), locales.UseSavedBootstrap())).Clicked() {
				a.bootstrapReuse = true
			}
			if a.bootstrapReuse {
				ui.Text(c, locales.ConfirmBootstrapIdentity(s.Profile.ID, s.Edition, s.Profile.SaveName, s.Profile.Revision, s.Profile.BootstrapSHA256))
				if inline(ui.Button(c.Key("bootstrap-reuse-confirm"), locales.ConfirmBootstrapIdentityAction())).Clicked() {
					a.bootstrapError = a.confirmExistingBootstrap() != nil
					a.bootstrapReuse = false
				}
			}
		}
	}
	if a.bootstrapError {
		ui.Text(c, locales.BootstrapPersistenceError()).TextColor(c.Theme().Danger)
	}
	a.restartView(c, s)
	if s.Stage == session.Failed {
		ui.Text(c, locales.SafeFailure()).Bold().TextColor(c.Theme().Danger)
	}
	if a.commandRejected {
		message := a.commandInstruction
		if message == "" {
			message = locales.CommandRejected()
		}
		ui.Text(c, message).TextColor(c.Theme().Danger)
	}
	// While a launch runs, the only actions are the ones that end it.
	if a.flow.stage == flowLaunch && !s.Busy && s.Stage != session.Indeterminate {
		label := locales.StartJailbreak()
		// The label names the selected transport, never the outcome: a blocked
		// automatic launch stays "Start" and its blocking checks say why.
		if s.Transport.Capabilities.Manual {
			label = locales.GuidedManualLaunch()
		}
		if inline(ui.PrimaryButton(c.Key("start"), label)).Disabled(pending || !s.CanStart()).Clicked() {
			a.command(a.controller.Start())
		}
	}
	// A running tool is stopped by the tools panel's own action, so the
	// launch stop actions stay out of that state.
	if (s.Busy && s.Stage != session.Verified && s.Stage != session.RetainedRPC &&
		s.Stage != session.RunningTool && s.Stage != session.Finished) || s.Stage == session.BootstrapSetup {
		// Both stay visible with their consequence, as docs/accessibility.md
		// requires; only the one that is valid now leads.
		if s.SideEffects {
			ui.Text(c, locales.StopExplains())
			if inline(ui.PrimaryButton(c.Key("stop"), locales.Stop())).Clicked() {
				a.command(a.controller.Stop())
			}
			muted(c, locales.CancelExplains())
			inline(ui.Button(c.Key("cancel"), locales.Cancel())).Disabled(true)
		} else {
			ui.Text(c, locales.CancelExplains())
			if inline(ui.PrimaryButton(c.Key("cancel"), locales.Cancel())).Clicked() {
				a.command(a.controller.Cancel())
			}
			muted(c, locales.StopExplains())
			inline(ui.Button(c.Key("stop"), locales.Stop())).Disabled(true)
		}
	}
	if a.flow.stage == flowSelection && !s.BootstrapSaved && s.Profile.BootstrapSHA256 != "" &&
		(s.Stage == session.NeedsSetup || s.Stage == session.Ready) && !s.Busy {
		if inline(ui.PrimaryButton(c.Key("bootstrap"), locales.PrepareBootstrap())).Disabled(pending).Clicked() {
			a.allowOverwrite = false
			a.command(a.controller.PrepareBootstrap())
		}
	}
	if s.Stage == session.BootstrapSetup && !s.Busy && s.BootstrapPrepared {
		if !s.SaveSubmitted {
			ui.Checkbox(c.Key("bootstrap-overwrite"), &a.allowOverwrite, locales.AllowBootstrapOverwrite())
			if inline(ui.PrimaryButton(c.Key("save"), locales.SubmitSave())).Disabled(!a.allowOverwrite).Clicked() {
				a.command(a.controller.SaveBootstrap(false))
			}
		} else {
			ui.Text(c, locales.SavePrompt())
			if inline(ui.PrimaryButton(c.Key("confirm-save"), locales.ConfirmSaved())).Clicked() {
				a.command(a.controller.SaveBootstrap(true))
			}
		}
	}
	if s.Stage == session.Verified || s.Stage == session.RetainedRPC {
		if inline(ui.Button(c.Key("finish"), locales.Finish())).Disabled(s.Busy).Clicked() {
			a.command(a.controller.Finish())
		}
	}
}

// launchStepsView shows the run's real position, from the controller's stage
// and the guided prompt it is waiting on. It claims no step the controller
// has not reported.
func (a *app) launchStepsView(c *ui.Context, s session.Snapshot) {
	manual := s.Transport.Capabilities.Manual
	names := []string{locales.StepHost(), locales.StepTyping(), locales.StepExploit(), locales.StepVerify()}
	if manual {
		names = []string{locales.StepHost(), locales.StepLoad(), locales.StepRun(), locales.StepRemove(), locales.StepStart(), locales.StepVerify()}
	}
	current := -1
	switch s.Stage {
	case session.PreparingHost:
		current = 0
	case session.WaitingForInput, session.Inputting, session.WaitingForConsole:
		current = 1
	case session.RunningExploit:
		current = len(names) - 2
	case session.Verifying:
		current = len(names) - 1
	case session.Verified, session.RetainedRPC, session.RunningTool, session.Finished:
		current = len(names)
	}
	if manual && current == 1 && a.manual != nil {
		if p := a.manual.Prompt(); p.RunID == s.RunID {
			switch p.Kind {
			case "load", "load-confirm":
				current = 1
			case "run":
				current = 2
			case "remove":
				current = 3
			case "start":
				current = 4
			}
		}
	}
	if current < 0 && s.Stage != session.Ready {
		return
	}
	ui.Column(c).FillWidth().Gap(c.Theme().Space(2)).Children(func() {
		sectionTitle(c, locales.LaunchSteps())
		steps(c, names, current)
		if s.Total > 0 {
			meter(c, float32(s.Current)/float32(s.Total))
			muted(c, fmt.Sprintf("%s: %d / %d", locales.Progress(), s.Current, s.Total))
			if s.HasEstimate && s.Busy {
				muted(c, locales.TypingRemaining(fmt.Sprint((s.Remaining.Milliseconds()+999)/1000)))
			}
		}
	})
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

func (a *app) readinessView(c *ui.Context, s session.Snapshot, detection bool) {
	for _, check := range s.Checks {
		if detection != detectionCheck(check) || !checkApplies(check, s.Transport) {
			continue
		}
		k := checkTone(check.State)
		stateCard(c, k, func() {
			ui.Row(c).FillWidth().Wrap().Gap(c.Theme().Space(2)).Children(func() {
				ui.Text(c, checkName(check)).Bold().Shrink(1)
				grow(c)
				badge(c, check.ID+"-state", k, checkStateTitle(check.State))
			})
			message := check.Message
			if check.State == session.CheckVerified && s.Transport.Controller != "" &&
				(check.ID == "usb-controllers" || check.Controller == s.Transport.Controller) {
				message = locales.UsbEnumerationObserved(s.Transport.Controller)
			}
			ui.Text(c.Key(check.ID+"-message"), localize(message))
			if check.NextAction != "" {
				ui.Text(c.Key(check.ID+"-action"), locales.NextStep(localize(check.NextAction)))
			}
			if check.GatesTransport == "usb-hid" {
				muted(c, locales.UsbOnly())
			}
		})
	}
}

// routeCheckList names what this route will be checked for.
func routeCheckList(c *ui.Context, transport session.Transport) {
	muted(c, locales.RouteCheckListTitle())
	names := []string{locales.CheckRuntime(), locales.CheckHostProgram()}
	if !transport.Capabilities.Manual && !transport.Capabilities.ExternalPico {
		names = append(names, locales.CheckUsbHelpers())
	}
	ui.Column(c).FillWidth().Gap(c.Theme().Space(1)).Children(func() {
		for _, name := range names {
			ui.Text(c, "· "+name)
		}
	})
}

// checkList names what the selected method will be checked for, so the
// first visit explains the stage instead of showing an empty card.
func checkList(c *ui.Context, transport session.Transport) {
	muted(c, locales.CheckListTitle())
	names := []string{locales.CheckNetwork(), locales.CheckTypingMethod()}
	if !transport.Capabilities.Manual && !transport.Capabilities.ExternalPico {
		names = append(names, locales.CheckSip(), locales.CheckDriverLoaded(), locales.CheckUsbController(), locales.CheckUsbHelpers())
	}
	ui.Column(c).FillWidth().Gap(c.Theme().Space(1)).Children(func() {
		for _, name := range names {
			ui.Text(c, "· "+name)
		}
	})
}

func referencePrerequisites(c *ui.Context, usb bool) {
	sectionTitle(c, locales.ReferencePrerequisites())
	ui.Text(c, locales.ConsoleFirmware())
	ui.Text(c, locales.SmilebasicPrerequisites())
	ui.Text(c, locales.NetworkPrerequisites())
	if usb {
		ui.Text(c, locales.CablePrerequisites())
		muted(c, locales.DriverSecurityGuidance())
	}
}

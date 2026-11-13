package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

func (a *app) command(s session.Snapshot, err error) {
	a.commandRejected = err != nil
	a.commandInstruction = ""
	if rejection, ok := err.(*session.Rejection); ok {
		a.commandInstruction = appstate.KnownRejectionInstruction(s, rejection)
	}
}

func (a *app) sessionView(c *ui.Context) {
	if a.keyboard.open {
		a.keyboardView(c)
		if a.keyboard.forwarding || a.keyboard.completion != nil {
			return
		}
	}
	s := a.Controller.Snapshot()
	pending := a.preferencesPending(s)
	if pending {
		ui.Text(c, locales.ApplyBeforeLaunch()).Bold().TextColor(c.Theme().Warning)
	}
	// In selection the route rows carry the verdict; only work in progress,
	// such as bootstrap setup, is named here.
	showStage := a.flow.stage != flowSelection ||
		(s.Stage != session.Idle && s.Stage != session.Ready && s.Stage != session.NeedsSetup)
	if showStage {
		switch a.flow.stage {
		case flowSelection:
			// The card around this view carries the section's title, so the
			// session state is a line in it, not a second heading.
			muted(c.Key("session-stage"), appstate.StageTitle(s.Stage)).Role(ui.RoleStatus)
		case flowTools:
			// It opens the stage, above the tools it allows.
			ui.Text(c.Key("session-stage"), appstate.StageTitle(s.Stage)).Role(ui.RoleStatus)
		default:
			// In launch the session state is the section's title, and a
			// recovery state ends its line, as an observed row does.
			ui.Row(c).FillWidth().Wrap().Gap(c.Theme().Space(2)).Children(func() {
				sectionTitle(c.Key("session-stage"), appstate.StageTitle(s.Stage)).Role(ui.RoleStatus).Shrink(1)
				if s.Stage == session.Indeterminate {
					grow(c)
					badge(c, "console-restart-badge", toneAttention, locales.CheckAttention())
				}
			})
		}
	}
	// A long-lived tool child keeps the retained_rpc stage, so its ownership
	// is stated next to the stage; the tools panel states it in its own stage.
	if a.Tools != nil && a.flow.stage != flowTools {
		if owner := a.Tools.State().Tool; owner != "" {
			ui.Text(c.Key("session-tool-owner"), locales.ToolRunning(owner)).Role(ui.RoleStatus)
		}
	}
	if a.flow.stage == flowLaunch {
		a.launchStepsView(c, s)
	}
	a.manualView(c, s)
	usbPromptView(c, &a.USBConfirmation)
	if s.CleanupConfirmed {
		ui.Text(c, locales.ResourcesReleased())
	}
	if s.Stage == session.Verified || s.Stage == session.RetainedRPC {
		summary, evidence := locales.VerifiedSummary(), locales.VerificationEvidence()
		if s.Stage == session.RetainedRPC {
			summary, evidence = locales.VerifiedHeldSummary(), locales.HeldVerificationEvidence()
		}
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			// In the tools stage the muted stage line above is the summary.
			if a.flow.stage != flowTools {
				muted(c, summary)
			}
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
				ui.Text(c, locales.ConfirmBootstrapIdentity(s.Profile.SaveName, s.Profile.ID, s.Edition, s.Profile.Revision, s.Profile.BootstrapSHA256))
				if inline(ui.Button(c.Key("bootstrap-reuse-confirm"), locales.ConfirmBootstrapIdentityAction())).Clicked() {
					a.bootstrapError = a.ConfirmExistingBootstrap() != nil
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
			a.command(a.Controller.Start())
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
				a.command(a.Controller.Stop())
			}
			muted(c, locales.CancelExplains())
			inline(ui.Button(c.Key("cancel"), locales.Cancel())).Disabled(true)
		} else {
			ui.Text(c, locales.CancelExplains())
			if inline(ui.PrimaryButton(c.Key("cancel"), locales.Cancel())).Clicked() {
				a.command(a.Controller.Cancel())
			}
			muted(c, locales.StopExplains())
			inline(ui.Button(c.Key("stop"), locales.Stop())).Disabled(true)
		}
	}
	if a.flow.stage == flowSelection && !s.BootstrapSaved && s.Profile.BootstrapSHA256 != "" &&
		(s.Stage == session.NeedsSetup || s.Stage == session.Ready) && !s.Busy {
		if inline(ui.PrimaryButton(c.Key("bootstrap"), locales.PrepareBootstrap())).Disabled(pending).Clicked() {
			a.allowOverwrite = false
			a.command(a.Controller.PrepareBootstrap())
		}
	}
	if s.Stage == session.BootstrapSetup && !s.Busy && s.BootstrapPrepared {
		if !s.SaveSubmitted {
			ui.Checkbox(c.Key("bootstrap-overwrite"), &a.allowOverwrite, locales.AllowBootstrapOverwrite())
			if inline(ui.PrimaryButton(c.Key("save"), locales.SubmitSave())).Disabled(!a.allowOverwrite).Clicked() {
				a.command(a.Controller.SaveBootstrap(false))
			}
		} else {
			ui.Text(c, locales.SavePrompt())
			if inline(ui.PrimaryButton(c.Key("confirm-save"), locales.ConfirmSaved())).Clicked() {
				a.command(a.Controller.SaveBootstrap(true))
			}
		}
	}
	if a.flow.stage != flowTools && (s.Stage == session.Verified || s.Stage == session.RetainedRPC) {
		if inline(ui.Button(c.Key("finish"), locales.Finish())).Disabled(s.Busy).Clicked() {
			a.command(a.Controller.Finish())
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
	if manual && current == 1 && a.Manual != nil {
		if p := a.Manual.Prompt(); p.RunID == s.RunID {
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
		muted(c, locales.LaunchSteps())
		steps(c, names, current)
		if s.Total > 0 {
			meter(c, float32(s.Current)/float32(s.Total))
			if s.HasEstimate && s.Busy {
				muted(c, locales.TypingRemaining(fmt.Sprint((s.Remaining.Milliseconds()+999)/1000)))
			}
		}
	})
}

// readinessView shows the observed checks that show selects, each as its
// subject, state, the adapter's message and its next action.
func readinessView(c *ui.Context, s session.Snapshot, show func(session.Check) bool) {
	for _, check := range s.Checks {
		if !show(check) {
			continue
		}
		k := checkTone(check.State)
		stateCard(c, func() {
			ui.Row(c).FillWidth().Wrap().Gap(c.Theme().Space(2)).Children(func() {
				ui.Text(c, appstate.CheckName(check)).Bold().Shrink(1)
				grow(c)
				badge(c, check.ID+"-state", k, appstate.CheckStateTitle(check.State))
			})
			ui.Text(c.Key(check.ID+"-message"), appstate.CheckMessage(check, s.Transport.Controller))
			if action := appstate.CheckNextAction(check); action != "" {
				ui.Text(c.Key(check.ID+"-action"), locales.NextStep(action))
			}
			if check.GatesTransport == session.GateUSBHID {
				muted(c, locales.UsbOnly())
			}
			rule(c)
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

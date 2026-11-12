package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

type flowStage int

const (
	flowDetection flowStage = iota
	flowSelection
	flowLaunch
	flowTools
)

// Presentation only. All operation eligibility and evidence belong to the controller.
type flowUI struct {
	stage, rendered flowStage
	initialized     bool
	observed        session.Stage
	method          string
	heading         ui.Handle
	settings        bool
	diagnostics     bool
	entered         int
	prerequisites   bool
	expert          bool
	attach          bool
	evidence        bool
}

func detectionCheck(check session.Check) bool {
	return check.ID != "runtime" && check.ID != "engine"
}

func checkApplies(check session.Check, transport session.Transport) bool {
	return !(check.GatesTransport == "usb-hid" && (transport.Capabilities.Manual || transport.Capabilities.ExternalPico)) &&
		(check.Controller == "" || check.Controller == transport.Controller)
}

func (a *app) preferencesPending(s session.Snapshot) bool {
	return s.Profile.ID != a.preferences.Profile || s.Edition != a.preferences.Edition ||
		s.LANOverride != a.preferences.LANIPv4 || s.Transport.Mode != a.preferences.Transport
}

func (a *app) detectionReady(s session.Snapshot) bool {
	if a.preferencesPending(s) || s.Busy || s.SideEffects || len(s.Checks) == 0 ||
		s.Stage == session.Indeterminate || s.Stage == session.Failed {
		return false
	}
	for _, check := range s.Checks {
		if detectionCheck(check) && checkApplies(check, s.Transport) && check.State == session.RequiresAttention {
			return false
		}
	}
	return true
}

func (a *app) flowView(c *ui.Context) {
	s := a.controller.Snapshot()
	switch {
	case s.Stage == session.Verified || s.Stage == session.RetainedRPC || s.Stage == session.RunningTool:
		a.flow.stage = flowTools
	case s.Stage == session.BootstrapSetup:
		a.flow.stage = flowSelection
	case s.Stage == session.Idle && a.flow.observed == session.Indeterminate:
		a.flow.stage = flowDetection
	case s.Stage == session.Indeterminate || (s.Stage == session.Finished && a.flow.observed != s.Stage) ||
		(s.Busy && s.Stage != session.Inspecting):
		a.flow.stage = flowLaunch
	case a.flow.stage == flowTools:
		a.flow.stage = flowLaunch
	}
	a.flow.observed = s.Stage
	titles := []string{locales.FlowDetection(), locales.FlowSelection(), locales.FlowLaunch(), locales.FlowTools()}
	stepper(c, a.flow.stage, titles)
	// The stepper carries the position visually; the heading names the stage
	// once, and announces the position to assistive technology.
	ui.Text(c.Key("flow-stage-heading"), titles[a.flow.stage]).FontSize(c.Theme().Rem(1.55)).Bold().
		Label(locales.FlowPosition(fmt.Sprint(int(a.flow.stage)+1), titles[a.flow.stage])).
		Role(ui.RoleStatus).Focusable().Bind(&a.flow.heading)
	if !a.flow.initialized || a.flow.rendered != a.flow.stage {
		a.flow.heading.Focus(c)
		a.flow.rendered, a.flow.initialized = a.flow.stage, true
	}
	stage := ui.Column(c.Key(fmt.Sprint("stage-", int(a.flow.stage)))).FillWidth().Gap(16)
	a.flow.entered = enter(c, stage, a.flow.entered, int(a.flow.stage)+1)
	stage.Children(func() {
		switch a.flow.stage {
		case flowDetection:
			a.detectionView(c, s)
		case flowSelection:
			a.selectionView(c, s)
		case flowLaunch:
			if a.civSwitchPanel.open || s.CivSwitch {
				card(c, func() { a.civSwitchView(c) })
			} else {
				card(c, func() { a.sessionView(c) })
			}
		case flowTools:
			if s.CivSwitch {
				card(c, func() { a.civSwitchView(c) })
				return
			}
			a.sessionView(c)
			if a.saveKit.open {
				card(c, func() { a.saveKitView(c) })
			} else {
				card(c, func() { a.advancedToolsView(c) })
			}
		}
	})
}

// selectionReady repeats the controller's launch conditions this stage owns:
// it never relaxes them, and it keeps Continue disabled until the chosen
// route's own host checks have actually been observed.
func (a *app) selectionReady(s session.Snapshot) bool {
	if a.preferencesPending(s) || !s.CanStart() {
		return false
	}
	rows := 0
	for _, check := range s.Checks {
		if !detectionCheck(check) && checkApplies(check, s.Transport) && check.State != session.CheckVerified {
			return false
		}
		if !detectionCheck(check) && checkApplies(check, s.Transport) {
			rows++
		}
	}
	return rows > 0
}

func (a *app) selectionView(c *ui.Context, s session.Snapshot) {
	p, _ := a.manifest.Profile(a.preferences.Profile)
	host := p.Label
	if host == "" {
		host = s.Profile.ID
	}
	card(c, func() {
		field(c, locales.FlowRoute(), func() {
			// Radio buttons mark the chosen route by shape and announce it as
			// chosen; the arrows move between the two.
			ui.RadioGroup(c.Key("route"), func() {
				ui.Radio(c.Key("smile-route"), &a.civSwitchPanel.open, false, locales.FlowSmile()).Disabled(s.Busy || s.SideEffects)
				ui.Radio(c.Key("civ-open"), &a.civSwitchPanel.open, true, locales.CivTitle()).Disabled(s.Busy || s.SideEffects)
			}).Row().Wrap().Gap(c.Theme().Space(5)).Label(locales.FlowRoute())
		})
		keyValue(c, locales.HostProfile(), host+" · "+s.Edition)
	})
	if a.civSwitchPanel.open {
		if !s.Transport.Capabilities.Keyboard || !s.Transport.Capabilities.Gamepad {
			stateCard(c, toneAttention, func() {
				badge(c, "civ-method-badge", toneAttention, locales.CheckAttention())
				ui.Text(c.Key("civ-method-unavailable"), locales.FlowCivUnavailable()).Role(ui.RoleStatus)
			})
			return
		}
		card(c, func() { a.civSwitchView(c) })
		return
	}
	// One card for everything this route still needs before launch.
	card(c, func() {
		sectionTitle(c, locales.SelectionRequirements())
		rows := 0
		for _, check := range s.Checks {
			if !detectionCheck(check) && checkApplies(check, s.Transport) {
				rows++
			}
		}
		// During bootstrap setup the setup steps are the stage's work; the
		// checks that allowed it stay out of the way.
		if s.Stage != session.BootstrapSetup {
			if rows == 0 {
				routeCheckList(c, s.Transport)
				ui.Text(c, locales.RouteUnchecked()).Role(ui.RoleStatus)
			}
			a.readinessView(c, s, false)
			if inline(ui.Button(c.Key("selection-inspect"), locales.FlowCheckSelection())).Disabled(s.Busy || s.SideEffects || a.preferencesPending(s)).Clicked() {
				a.command(a.controller.Inspect())
			}
		}
		if s.BootstrapSaved {
			stateCard(c, toneVerified, func() {
				badge(c, "flow-saved-badge", toneVerified, locales.CheckVerified())
				ui.Text(c, locales.FlowSaved()).Role(ui.RoleStatus)
			})
		}
		a.sessionView(c)
		ui.Collapsible(c.Key("selection-details"), locales.ReferencePrerequisites(), &a.flow.prerequisites, func() {
			panelBody(c, func() { referencePrerequisites(c, !s.Transport.Capabilities.Manual) })
		})
	})
	card(c, func() {
		ui.Collapsible(c.Key("selection-expert"), locales.ExpertOptions(), &a.flow.expert, func() {
			panelBody(c, func() { a.profileView(c) })
		})
		ui.Collapsible(c.Key("selection-attach"), locales.ConnectExisting(), &a.flow.attach, func() {
			panelBody(c, func() { a.attachView(c, s) })
		})
	})
}

// footerView pins each stage's way forward and back to the window's bottom
// edge, so the primary action never scrolls out of sight.
func (a *app) footerView(c *ui.Context) {
	s := a.controller.Snapshot()
	switch a.flow.stage {
	case flowDetection:
		ready := a.detectionReady(s)
		footer(c, func() {
			if !ready {
				hint := locales.FlowDetectionBlocked()
				if len(s.Checks) == 0 {
					hint = locales.FlowCheckFirst()
				} else if a.preferencesPending(s) {
					hint = locales.ApplyLanHint()
				} else if s.Busy {
					hint = locales.CheckingReadiness()
				}
				muted(c, hint).Shrink(1)
			}
		}, func() {
			if ui.PrimaryButton(c.Key("flow-next-selection"), locales.FlowNextSelection()).Disabled(!ready).Clicked() {
				a.stopKeyboard()
				a.keyboard.open = false
				a.flow.stage = flowSelection
			}
		})
	case flowSelection:
		civ := a.civSwitchPanel.open && s.Transport.Capabilities.Keyboard && s.Transport.Capabilities.Gamepad
		ready := a.selectionReady(s)
		if civ {
			ready = !s.Busy && a.controller.CivSwitchUnavailable() == "" &&
				a.civSwitchPanel.prerequisites.PhysicalSwitch1Cartridge100 && a.civSwitchPanel.prerequisites.EnglishUS &&
				a.civSwitchPanel.prerequisites.SameReachableNetwork && a.civSwitchPanel.prerequisites.LobbyChatFocused
		}
		footer(c, func() {
			if ui.Button(c.Key("flow-back-detection"), locales.FlowBackDetection()).Disabled(s.Busy || s.SideEffects || s.Stage == session.BootstrapSetup).Clicked() {
				a.flow.stage = flowDetection
			}
			if !ready && !a.civSwitchPanel.open {
				muted(c, locales.SelectionBlocked()).Shrink(1)
			}
		}, func() {
			if a.civSwitchPanel.open && !civ {
				return
			}
			if civ {
				if ui.PrimaryButton(c.Key("flow-civ-launch"), locales.FlowNextLaunch()).Disabled(!ready).Clicked() {
					a.flow.stage = flowLaunch
				}
				return
			}
			if ui.PrimaryButton(c.Key("flow-next-launch"), locales.FlowNextLaunch()).Disabled(!ready).Clicked() {
				a.flow.stage = flowLaunch
			}
		})
	case flowLaunch:
		footer(c, func() {
			if ui.Button(c.Key("flow-back-selection"), locales.FlowBackSelection()).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate).Clicked() {
				a.flow.stage = flowSelection
			}
		}, func() {})
	case flowTools:
		if s.CivSwitch || (s.Stage != session.Verified && s.Stage != session.RetainedRPC) {
			return
		}
		footer(c, func() {}, func() {
			if ui.Button(c.Key("finish"), locales.Finish()).Disabled(s.Busy).Clicked() {
				a.command(a.controller.Finish())
			}
		})
	}
}

// startupSecurityApplies names the built-in USB routes whose driver needs
// startup security the app can only describe, never observe or change.
func startupSecurityApplies(t session.Transport) bool {
	return !t.Capabilities.Manual && !t.Capabilities.ExternalPico
}

func (a *app) detectionView(c *ui.Context, s session.Snapshot) {
	disabled := s.Busy || s.SideEffects || s.Stage == session.Indeterminate
	card(c, func() {
		sectionTitle(c, locales.TransportMode())
		muted(c, locales.FlowDetectHint())
		labels := []string{locales.FlowManual(), locales.FlowKeyboard(), locales.FlowComposite(), locales.FlowPico()}
		modes := []string{"manual", "keyboard", "composite", "external-pico"}
		if !slices.Contains(labels, a.flow.method) {
			for i, mode := range modes {
				if a.preferences.Transport == mode {
					a.flow.method = labels[i]
				}
			}
		}
		// Choosing a method is the whole step: it applies the method and
		// checks what that method needs, through the same commands.
		ui.Select(c.Key("input-method"), &a.flow.method, labels).Label(locales.TransportMode()).Disabled(disabled).OnChange(func() {
			for i, label := range labels {
				if label == a.flow.method {
					a.preferences.Transport = modes[i]
				}
			}
			a.applyAndCheck()
		})
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, locales.LanOverride())
			ui.TextInput(c.Key("lan-override"), &a.preferences.LANIPv4).Label(locales.LanOverride()).
				Placeholder(locales.AutomaticLAN()).Disabled(disabled).OnSubmit(a.applyAndCheck)
		})
		if a.preferenceError != "" {
			ui.Text(c, a.preferenceError).TextColor(c.Theme().Danger)
		}
	})
	card(c, func() {
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			sectionTitle(c, locales.ReadinessChecks())
			grow(c)
			muted(c.Key("session-stage"), stageTitle(s.Stage)).Role(ui.RoleStatus)
		})
		if len(s.Checks) == 0 {
			checkList(c, s.Transport)
		}
		a.readinessView(c, s, true)
		if a.preferencesPending(s) {
			muted(c, locales.ApplyLanHint())
		}
		if inline(ui.Button(c.Key("inspect"), locales.CheckReadiness())).Disabled(disabled || a.preferencesPending(s)).Clicked() {
			a.command(a.controller.Inspect())
		}
	})
	if startupSecurityApplies(s.Transport) {
		ui.Column(c.Key("startup-security-note")).FillWidth().Children(func() {
			stateCard(c, toneManual, func() {
				manualBadge(c)
				ui.Text(c.Key("startup-security-state"), locales.StartupSecurityTitle()).Bold().Role(ui.RoleStatus)
				ui.Text(c, locales.StartupSecurityUnverified())
				ui.Text(c, locales.StartupSecurityAction())
			})
		})
		card(c, func() {
			ui.Text(c, locales.DriverSecurityGuidance())
			ui.Row(c).Wrap().Gap(8).Children(func() {
				if ui.Button(c.Key("driver-setup"), locales.DriverSetup()).Disabled(disabled).Clicked() {
					a.driverPanel.open = true
				}
				if ui.Button(c.Key("usb-keyboard"), locales.UsbKeyboardTitle()).Disabled(disabled).Clicked() {
					a.keyboard.open = !a.keyboard.open
				}
			})
			if a.keyboard.open {
				a.keyboardView(c)
			}
		})
	}
}

// applyAndCheck issues the two controller commands this stage stands for:
// apply the chosen method, then inspect what that method needs.
func (a *app) applyAndCheck() {
	a.preferenceError = ""
	if err := a.applyPreferences(); err != nil {
		a.preferenceError = err.Error()
		return
	}
	a.command(a.controller.Inspect())
}
func (a *app) settingsView(c *ui.Context) {
	ui.Text(c, locales.Settings()).FontSize(c.Theme().Rem(1.55)).Bold()
	card(c, func() { a.profileView(c) })
	if a.controller != nil {
		card(c, func() {
			ui.Row(c).Wrap().Gap(8).Children(func() {
				if ui.Button(c.Key("settings-driver"), locales.DriverSetup()).Clicked() {
					a.driverPanel.open = true
				}
				if ui.Button(c.Key("settings-keyboard"), locales.UsbKeyboardTitle()).Clicked() {
					a.keyboard.open = !a.keyboard.open
				}
			})
			if a.keyboard.open {
				a.keyboardView(c)
			}
		})
	}
	card(c, func() {
		if inline(ui.Button(c.Key("about"), locales.AboutBuild())).Clicked() {
			a.about = !a.about
		}
		if a.about {
			muted(c, locales.BuildLimitations())
		}
	})
}

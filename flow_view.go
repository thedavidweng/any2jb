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
	ui.Text(c.Key("flow-position"), locales.FlowPosition(fmt.Sprint(int(a.flow.stage)+1), titles[a.flow.stage])).
		FontSize(c.Theme().Rem(1.55)).Bold().Role(ui.RoleStatus).Focusable().Bind(&a.flow.heading)
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
			card(c, func() { a.sessionView(c) })
			if a.toolsPanel.open {
				card(c, func() {
					if a.saveKit.open {
						a.saveKitView(c)
					} else {
						a.advancedToolsView(c)
					}
				})
			}
		}
	})
}

func (a *app) selectionView(c *ui.Context, s session.Snapshot) {
	card(c, func() {
		ui.Row(c).Wrap().Gap(8).Children(func() {
			// The chosen route keeps an accent outline; the content below it
			// names the route in words.
			chosen := func(e ui.Element, on bool) ui.Element {
				if on {
					e.Border(2, c.Theme().Accent)
				}
				return e
			}
			if chosen(ui.Button(c.Key("smile-route"), locales.FlowSmile()), !a.civSwitchPanel.open).Disabled(s.Busy || s.SideEffects).Clicked() {
				a.civSwitchPanel.open = false
			}
			if chosen(ui.Button(c.Key("civ-open"), locales.CivTitle()), a.civSwitchPanel.open).Disabled(s.Busy || s.SideEffects).Clicked() {
				a.civSwitchPanel.open = true
			}
		})
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
	card(c, func() { a.profileView(c) })
	card(c, func() {
		sectionTitle(c, locales.ReadinessChecks())
		a.readinessView(c, s, false)
		if inline(ui.Button(c.Key("selection-inspect"), locales.FlowCheckSelection())).Disabled(s.Busy || s.SideEffects || a.preferencesPending(s)).Clicked() {
			a.command(a.controller.Inspect())
		}
	})
	if s.BootstrapSaved {
		stateCard(c, toneVerified, func() {
			badge(c, "flow-saved-badge", toneVerified, locales.CheckVerified())
			ui.Text(c, locales.FlowSaved()).Role(ui.RoleStatus)
		})
	}
	card(c, func() { a.sessionView(c) })
	card(c, func() { referencePrerequisites(c, !s.Transport.Capabilities.Manual) })
	card(c, func() { a.attachView(c, s) })
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
				muted(c, locales.FlowDetectionBlocked()).Shrink(1).Grow(1)
			}
		}, func() {
			if ui.PrimaryButton(c.Key("flow-next-selection"), locales.FlowNextSelection()).Disabled(!ready).Clicked() {
				a.stopKeyboard()
				a.keyboard.open = false
				a.flow.stage = flowSelection
			}
		})
	case flowSelection:
		footer(c, func() {
			if ui.Button(c.Key("flow-back-detection"), locales.FlowBackDetection()).Disabled(s.Busy || s.SideEffects || s.Stage == session.BootstrapSetup).Clicked() {
				a.flow.stage = flowDetection
			}
		}, func() {
			switch {
			case !a.civSwitchPanel.open:
				if ui.PrimaryButton(c.Key("flow-next-launch"), locales.FlowNextLaunch()).Disabled(a.preferencesPending(s) || !s.CanStart()).Clicked() {
					a.flow.stage = flowLaunch
				}
			case s.Transport.Capabilities.Keyboard && s.Transport.Capabilities.Gamepad:
				if ui.PrimaryButton(c.Key("flow-civ-launch"), locales.FlowNextLaunch()).Disabled(s.Busy || a.controller.CivSwitchUnavailable() != "" ||
					!a.civSwitchPanel.prerequisites.PhysicalSwitch1Cartridge100 || !a.civSwitchPanel.prerequisites.EnglishUS ||
					!a.civSwitchPanel.prerequisites.SameReachableNetwork || !a.civSwitchPanel.prerequisites.LobbyChatFocused).Clicked() {
					a.flow.stage = flowLaunch
				}
			}
		})
	case flowLaunch:
		footer(c, func() {
			if ui.Button(c.Key("flow-back-selection"), locales.FlowBackSelection()).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate).Clicked() {
				a.flow.stage = flowSelection
			}
		}, func() {})
	}
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
		ui.Select(c.Key("input-method"), &a.flow.method, labels).Label(locales.TransportMode()).Disabled(disabled).OnChange(func() {
			for i, label := range labels {
				if label == a.flow.method {
					a.preferences.Transport = modes[i]
				}
			}
		})
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, locales.LanOverride())
			ui.TextInput(c.Key("lan-override"), &a.preferences.LANIPv4).Label(locales.LanOverride()).Placeholder(locales.AutomaticLAN()).Disabled(disabled)
		})
		if inline(ui.Button(c.Key("apply-input"), locales.ApplyPreferences())).Disabled(disabled).Clicked() {
			a.preferenceError = ""
			if err := a.applyPreferences(); err != nil {
				a.preferenceError = err.Error()
			}
		}
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
		a.readinessView(c, s, true)
		if inline(ui.Button(c.Key("inspect"), locales.CheckReadiness())).Disabled(disabled || a.preferencesPending(s)).Clicked() {
			a.command(a.controller.Inspect())
		}
	})
	if !s.Transport.Capabilities.Manual && !s.Transport.Capabilities.ExternalPico {
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

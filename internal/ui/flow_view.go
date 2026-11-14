package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/readiness"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

type flowStage int

const (
	flowDetection flowStage = iota
	flowPrepare
	flowSelection
	flowLaunch
	flowTools
)

// Presentation only. All operation eligibility and evidence belong to the controller.
type flowUI struct {
	stage, rendered flowStage
	initialized     bool
	observed        session.Stage
	heading         ui.Handle
	plugins         bool
	utilities       bool
	settings        bool
	diagnostics     bool
	aboutPage       bool
	entered         int
	prerequisites   bool
	expert          bool
	attach          bool
	evidence        bool
	recoverySteps   bool
	// The console can change between runs, so these confirmations live only
	// as long as the app does and are never saved.
	prepared prepared
}

func (a *app) preferencesPending(s session.Snapshot) bool {
	return s.Profile.ID != a.Preferences.Profile || s.Edition != a.Preferences.Edition ||
		s.LANOverride != a.Preferences.LANIPv4 || s.Transport.Mode != a.Preferences.Transport
}

// detectionReady reports whether the observed checks allow leaving the first
// stage with transport t. It only reads the controller's checks.
func (a *app) detectionReady(s session.Snapshot, t session.Transport) bool {
	if a.preferencesPending(s) || s.Busy || s.SideEffects ||
		s.Stage == session.Indeterminate || s.Stage == session.Failed {
		return false
	}
	return session.DetectionChecksPass(s.Checks, t)
}

// syncStage follows the controller into the stage that shows its work.
func (a *app) syncStage() {
	s := a.Controller.Snapshot()
	switch {
	case s.Stage == session.Verified || s.Stage == session.RetainedRPC || s.Stage == session.RunningTool:
		a.flow.stage = flowTools
	case s.Stage == session.BootstrapSetup:
		a.flow.stage = flowSelection
	case s.Stage == session.Idle && a.flow.observed == session.Indeterminate:
		a.flow.stage = flowDetection
	// An inspection reports Ready or NeedsSetup a moment before it stops
	// being busy; that is still the stage that asked for it, not a launch.
	case s.Stage == session.Indeterminate || (s.Stage == session.Finished && a.flow.observed != s.Stage) ||
		(s.Busy && s.Stage != session.Inspecting && s.Stage != session.Ready && s.Stage != session.NeedsSetup):
		a.flow.stage = flowLaunch
	case a.flow.stage == flowTools:
		a.flow.stage = flowLaunch
	}
	a.flow.observed = s.Stage
}

func flowTitles() []string {
	return []string{locales.FlowDetection(), locales.FlowPrepare(), locales.FlowSelection(), locales.FlowLaunch(), locales.FlowTools()}
}

func flowTitlesFor(checks []session.Check) []string {
	titles := flowTitles()
	if session.IsLinuxChecks(checks) {
		titles[0] = locales.FlowDetectionLinux()
	}
	return titles
}

func (a *app) flowView(c *ui.Context) {
	s := a.Controller.Snapshot()
	titles := flowTitlesFor(s.Checks)
	// The sidebar carries the position visually; the heading names the stage
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
		case flowPrepare:
			a.prepareView(c, s)
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
	return session.RouteChecksVerified(s.Checks, s.Transport)
}

func (a *app) selectionView(c *ui.Context, s session.Snapshot) {
	p, _ := a.Manifest.Profile(a.Preferences.Profile)
	host := p.Label
	if host == "" {
		host = s.Profile.ID
	}
	route := locales.FlowSmile()
	if a.civSwitchPanel.open {
		route = locales.CivTitle()
	}
	card(c, func() {
		keyValue(c, locales.FlowRoute(), route)
		keyValue(c, locales.HostProfile(), host+" · "+s.Edition)
	})
	if a.civSwitchPanel.open {
		if !s.Transport.Capabilities.Keyboard || !s.Transport.Capabilities.Gamepad {
			stateCard(c, func() {
				badge(c, "civ-method-badge", toneAttention, locales.CheckAttention())
				ui.Text(c.Key("civ-method-unavailable"), locales.FlowCivUnavailable()).Role(ui.RoleStatus)
			})
			return
		}
		card(c, func() { a.civSwitchView(c) })
		return
	}
	routeCheck := func(check session.Check) bool { return !check.IsDetectionCheck() && check.AppliesTo(s.Transport) }
	// One card for everything this route still needs before launch.
	card(c, func() {
		sectionTitle(c, locales.SelectionRequirements())
		rows := 0
		for _, check := range s.Checks {
			if routeCheck(check) {
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
			readinessView(c, s, routeCheck)
			if inline(ui.Button(c.Key("selection-inspect"), locales.FlowCheckSelection())).Disabled(s.Busy || s.SideEffects || a.preferencesPending(s)).Clicked() {
				a.command(a.Controller.Inspect())
			}
		}
		if s.BootstrapSaved {
			stateCard(c, func() {
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
	s := a.Controller.Snapshot()
	switch a.flow.stage {
	case flowDetection:
		mode := a.continueMode(s)
		ready := mode != "" && a.detectionReady(s, a.TransportFor(mode))
		footer(c, func() {
			hint := ""
			switch {
			case s.Busy:
				hint = locales.CheckingReadiness()
			case len(s.Checks) == 0:
				hint = locales.FlowCheckFirst()
			case a.preferencesPending(s):
				hint = locales.ApplyBeforeLaunch()
			case ready:
			case a.usbRoute(s) == readiness.USBUnavailable && a.detectionReady(s, a.TransportFor("manual")):
				hint = locales.FlowUsbUnavailable()
			default:
				hint = locales.FlowDetectionBlocked()
			}
			if hint != "" {
				muted(c, hint).Shrink(1)
			}
		}, func() {
			if ui.PrimaryButton(c.Key("flow-next-selection"), locales.FlowNextSelection()).Disabled(!ready).Clicked() {
				a.continueWith(mode)
			}
		})
	case flowPrepare:
		civ := a.civSwitchPanel.open
		method := !civ || s.Transport.Capabilities.Keyboard && s.Transport.Capabilities.Gamepad
		ready := method && a.prepareDone()
		footer(c, func() {
			if ui.Button(c.Key("flow-back-detection"), locales.FlowBackDetection()).Disabled(s.Busy || s.SideEffects).Clicked() {
				a.flow.stage = flowDetection
			}
			switch {
			case !method:
				muted(c, locales.PrepCivMethod()).Shrink(1)
			case !ready:
				muted(c, locales.PrepBlocked()).Shrink(1)
			}
		}, func() {
			if ui.PrimaryButton(c.Key("flow-next-prepare"), locales.FlowNextSelection()).Disabled(!ready).Clicked() {
				a.flow.stage = flowSelection
			}
		})
	case flowSelection:
		civ := a.civSwitchPanel.open && s.Transport.Capabilities.Keyboard && s.Transport.Capabilities.Gamepad
		ready := a.selectionReady(s)
		if civ {
			ready = !s.Busy && a.Controller.CivSwitchUnavailable() == "" &&
				a.civSwitchPanel.prerequisites.PhysicalSwitch1Cartridge100 && a.civSwitchPanel.prerequisites.EnglishUS &&
				a.civSwitchPanel.prerequisites.SameReachableNetwork && a.civSwitchPanel.prerequisites.LobbyChatFocused
		}
		footer(c, func() {
			if ui.Button(c.Key("flow-back-prepare"), locales.FlowBackDetection()).Disabled(s.Busy || s.SideEffects || s.Stage == session.BootstrapSetup).Clicked() {
				a.flow.stage = flowPrepare
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
				a.command(a.Controller.Finish())
			}
		})
	}
}

// continueWith moves on to preparing the Switch with the input method the first stage
// settled on. A different method is applied first and checked again, since
// the route's checks depend on it.
func (a *app) continueWith(mode string) {
	a.stopKeyboard()
	a.keyboard.open = false
	a.preferenceError = ""
	if mode != a.Controller.Snapshot().Transport.Mode {
		previous := a.Preferences.Transport
		a.Preferences.Transport = mode
		if err := a.ApplyPreferences(); err != nil {
			a.Preferences.Transport = previous
			a.preferenceError = err.Error()
			return
		}
		a.command(a.Controller.Inspect())
	}
	a.flow.stage = flowPrepare
}

func (a *app) settingsView(c *ui.Context) {
	ui.Text(c, locales.Settings()).FontSize(c.Theme().Rem(1.55)).Bold()
	card(c, func() { a.profileView(c) })
	if a.Controller != nil {
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

// usbMode is the built-in USB method Continue applies: composite when it was
// chosen in Settings, otherwise the plain USB keyboard.
func (a *app) usbMode() string {
	if a.Preferences.Transport == "composite" {
		return "composite"
	}
	return "keyboard"
}

func (a *app) usbRoute(s session.Snapshot) readiness.USBRoute {
	driverAvailable := a.driverPanel.gate == nil || a.driverPanel.gate.Availability() == nil
	return readiness.EvaluateUSBRoute(s.Checks, a.detectionReady(s, a.TransportFor(a.usbMode())), driverAvailable)
}

// continueMode is the automatic input method Continue applies, or "" while
// this host is not set up for it. Guided manual input is never chosen for the
// user. An external Pico chosen in Settings does not use this host's USB port,
// so the USB steps do not decide it.
func (a *app) continueMode(s session.Snapshot) string {
	if a.Preferences.Transport == "external-pico" {
		return "external-pico"
	}
	if a.usbRoute(s) == readiness.USBReady {
		return a.usbMode()
	}
	return ""
}

func (a *app) setupSteps(s session.Snapshot) []readiness.SetupStep {
	return readiness.ProjectSetupSteps(s, a.usbRoute(s))
}

func (a *app) detectionView(c *ui.Context, s session.Snapshot) {
	disabled := s.Busy || s.SideEffects || s.Stage == session.Indeterminate
	t := c.Theme()
	card(c, func() {
		if session.IsLinuxChecks(s.Checks) {
			muted(c, locales.LinuxIntro())
		} else {
			muted(c, locales.MacIntro())
		}
		for _, step := range a.setupSteps(s) {
			rowTone := checkTone(step.State)
			ui.Column(c.Key(step.Key)).FillWidth().Gap(t.Space(1.5)).Children(func() {
				ui.Row(c).FillWidth().Wrap().Gap(t.Space(2)).Children(func() {
					ui.Text(c, step.Title).Bold().Shrink(1)
					grow(c)
					badge(c, step.Key+"-state", rowTone, step.Status)
				})
				if step.Detail != "" {
					ui.Text(c.Key(step.Key+"-detail"), step.Detail)
				}
				switch step.Action {
				// Steps a blocked row needs are shown outright; while the step
				// is only in progress or unknown they wait behind a disclosure.
				case readiness.SetupShowRecovery:
					if rowTone == toneAttention {
						panelBody(c, func() { recoverySteps(c) })
					} else {
						ui.Collapsible(c.Key("recovery-steps"), locales.MacAllowShowSteps(), &a.flow.recoverySteps, func() {
							panelBody(c, func() { recoverySteps(c) })
						})
					}
				case readiness.SetupInstallDriver:
					if inline(ui.Button(c.Key("driver-install"), locales.MacInstallButton())).Disabled(disabled).Clicked() {
						a.driverPanel.open = true
					}
				case readiness.SetupChooseConnection:
					if inline(ui.Button(c.Key("usb-keyboard"), locales.MacConnectButton())).Disabled(disabled).Clicked() {
						a.keyboard.open = !a.keyboard.open
					}
				}
				rule(c)
			})
		}
		if a.keyboard.open {
			a.keyboardView(c)
		}
		if a.preferenceError != "" {
			ui.Text(c, a.preferenceError).TextColor(t.Danger)
		}
		if inline(ui.Button(c.Key("inspect"), locales.CheckAgain())).Disabled(disabled || a.preferencesPending(s)).Clicked() {
			a.command(a.Controller.Inspect())
		}
	})
	// Guided manual input stays reachable for those who want it, as a quiet
	// link after the setup, never beside or like the primary action.
	if inline(ui.Link(c.Key("flow-manual"), locales.FlowTypeYourself(), "")).
		Disabled(!a.detectionReady(s, a.TransportFor("manual"))).Clicked() {
		a.continueWith("manual")
	}
}

// recoverySteps is the walkthrough the user follows in macOS Recovery. The app
// shows it as text and never performs or checks any of it.
func recoverySteps(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1.5)).Children(func() {
		for i, step := range []string{locales.MacRecovery1(), locales.MacRecovery2(), locales.MacRecovery3(),
			locales.MacRecovery4(), locales.MacRecovery5(), locales.MacRecovery6()} {
			ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(2)).Children(func() {
				ui.Text(c, fmt.Sprint(i+1, ".")).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, step).Shrink(1)
			})
		}
	})
	muted(c, locales.MacRecoveryNote())
	// The catalogs carry each guide's URL, so the page is in the app's language
	// where Apple publishes one.
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Link(c.Key("apple-startup-security"), locales.MacGuideStartupSecurity(), locales.MacGuideStartupSecurityURL())
		ui.Link(c.Key("apple-recovery"), locales.MacGuideRecovery(), locales.MacGuideRecoveryURL())
		ui.Link(c.Key("apple-sip"), locales.MacGuideSip(), locales.MacGuideSipURL())
	})
}

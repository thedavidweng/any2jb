package main

import (
	"fmt"

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
	heading         ui.Handle
	settings        bool
	diagnostics     bool
	entered         int
	prerequisites   bool
	expert          bool
	attach          bool
	evidence        bool
	recoverySteps   bool
}

// The input-adapter check describes the method the last inspection used, so
// it belongs to the route stage, which inspects again with the method that
// Continue applied.
func detectionCheck(check session.Check) bool {
	return check.ID != "runtime" && check.ID != "engine" && check.ID != "input-adapter"
}

func checkApplies(check session.Check, transport session.Transport) bool {
	return !(check.GatesTransport == "usb-hid" && (transport.Capabilities.Manual || transport.Capabilities.ExternalPico)) &&
		(check.Controller == "" || check.Controller == transport.Controller)
}

func (a *app) preferencesPending(s session.Snapshot) bool {
	return s.Profile.ID != a.preferences.Profile || s.Edition != a.preferences.Edition ||
		s.LANOverride != a.preferences.LANIPv4 || s.Transport.Mode != a.preferences.Transport
}

// detectionReady reports whether the observed checks allow leaving the first
// stage with transport t. It only reads the controller's checks.
func (a *app) detectionReady(s session.Snapshot, t session.Transport) bool {
	if a.preferencesPending(s) || s.Busy || s.SideEffects || len(s.Checks) == 0 ||
		s.Stage == session.Indeterminate || s.Stage == session.Failed {
		return false
	}
	for _, check := range s.Checks {
		if detectionCheck(check) && checkApplies(check, t) && check.State == session.RequiresAttention {
			return false
		}
	}
	return true
}

// syncStage follows the controller into the stage that shows its work.
func (a *app) syncStage() {
	s := a.controller.Snapshot()
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
	return []string{locales.FlowDetection(), locales.FlowSelection(), locales.FlowLaunch(), locales.FlowTools()}
}

func (a *app) flowView(c *ui.Context) {
	s := a.controller.Snapshot()
	titles := flowTitles()
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
			stateCard(c, func() {
				badge(c, "civ-method-badge", toneAttention, locales.CheckAttention())
				ui.Text(c.Key("civ-method-unavailable"), locales.FlowCivUnavailable()).Role(ui.RoleStatus)
			})
			return
		}
		card(c, func() { a.civSwitchView(c) })
		return
	}
	routeCheck := func(check session.Check) bool { return !detectionCheck(check) && checkApplies(check, s.Transport) }
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
				a.command(a.controller.Inspect())
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
	s := a.controller.Snapshot()
	switch a.flow.stage {
	case flowDetection:
		mode := a.continueMode(s)
		ready := mode != "" && a.detectionReady(s, a.transportFor(mode))
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
			case a.usbRoute(s) == usbUnavailable && a.detectionReady(s, a.transportFor("manual")):
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

// continueWith moves on to the route with the input method the first stage
// settled on. A different method is applied first and checked again, since
// the route's checks depend on it.
func (a *app) continueWith(mode string) {
	a.stopKeyboard()
	a.keyboard.open = false
	a.preferenceError = ""
	if mode != a.controller.Snapshot().Transport.Mode {
		previous := a.preferences.Transport
		a.preferences.Transport = mode
		if err := a.applyPreferences(); err != nil {
			a.preferences.Transport = previous
			a.preferenceError = err.Error()
			return
		}
		a.command(a.controller.Inspect())
	}
	a.flow.stage = flowSelection
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

// usbRoute is what the observed checks say about built-in USB input.
type usbRoute int

const (
	usbPending usbRoute = iota
	usbUnavailable
	usbReady
)

func findCheck(checks []session.Check, id string) session.Check {
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	return session.Check{}
}

// usbMode is the built-in USB method Continue applies: composite when it was
// chosen in Settings, otherwise the plain USB keyboard.
func (a *app) usbMode() string {
	if a.preferences.Transport == "composite" {
		return "composite"
	}
	return "keyboard"
}

// usbRoute is unavailable when this Mac or this build cannot use built-in USB
// input at all, so no step the user takes could make it ready.
func (a *app) usbRoute(s session.Snapshot) usbRoute {
	loaded, present := findCheck(s.Checks, "driver-loaded"), findCheck(s.Checks, "driver-present")
	switch {
	case findCheck(s.Checks, "usb-platform").State == session.RequiresAttention,
		findCheck(s.Checks, "usb-runtime").State == session.RequiresAttention,
		a.driverPanel.gate.Availability() != nil && loaded.State != session.CheckVerified && present.State != session.CheckVerified:
		return usbUnavailable
	case loaded.State == session.CheckVerified && a.detectionReady(s, a.transportFor(a.usbMode())):
		return usbReady
	}
	return usbPending
}

// continueMode is the automatic input method Continue applies, or "" while
// this Mac is not set up for it. Guided manual input is never chosen for the
// user. An external Pico chosen in Settings does not use this Mac's USB port,
// so the USB steps do not decide it.
func (a *app) continueMode(s session.Snapshot) string {
	if a.preferences.Transport == "external-pico" {
		return "external-pico"
	}
	if a.usbRoute(s) == usbReady {
		return a.usbMode()
	}
	return ""
}

type macAction int

const (
	noAction macAction = iota
	showRecoverySteps
	installDriver
	chooseConnection
)

// macStep is one row of the first stage: a plain subject, its state and at
// most one sentence and one action.
type macStep struct {
	key, title, status, detail string
	tone                       tone
	action                     macAction
}

func (a *app) macSteps(s session.Snapshot) []macStep {
	steps := []macStep{
		{key: "mac-network", title: locales.CheckNetwork()},
		{key: "mac-allow", title: locales.MacAllow()},
		{key: "mac-install", title: locales.MacInstall()},
		{key: "mac-connect", title: locales.MacConnect()},
	}
	status := locales.MacNotChecked()
	if s.Stage == session.Inspecting {
		status = locales.MacChecking()
	}
	for i := range steps {
		steps[i].tone, steps[i].status = toneAvailable, status
	}
	if len(s.Checks) == 0 || s.Stage == session.Inspecting {
		return steps
	}
	set := func(step *macStep, k tone, status, detail string, action macAction) {
		step.tone, step.status, step.detail, step.action = k, status, detail, action
	}
	network, sip := findCheck(s.Checks, "network"), findCheck(s.Checks, "sip")
	present, loaded, restart := findCheck(s.Checks, "driver-present"), findCheck(s.Checks, "driver-loaded"), findCheck(s.Checks, "driver-restart")
	controllers := findCheck(s.Checks, "usb-controllers")
	unsupported := findCheck(s.Checks, "usb-platform").State == session.RequiresAttention
	unavailable := a.usbRoute(s) == usbUnavailable
	found := 0
	for _, check := range s.Checks {
		if check.Controller != "" {
			found++
		}
	}

	switch {
	case network.ID == "":
		steps[0].status = locales.MacNotChecked()
	case network.State == session.CheckVerified:
		set(&steps[0], toneVerified, locales.MacDone(), locales.MacNetworkDone(network.LANIPv4), noAction)
	default:
		set(&steps[0], toneAttention, locales.MacNeedsAction(), locales.MacNetworkAction(), noAction)
	}

	// Security is never lowered for a driver this build cannot install.
	switch {
	case unsupported:
		steps[1].status = locales.MacNotAvailable()
	case unavailable:
		steps[1].status = locales.MacWaiting()
	case loaded.State == session.CheckVerified:
		set(&steps[1], toneVerified, locales.MacDone(), locales.MacAllowDone(), noAction)
	case sip.State == session.CheckVerified:
		set(&steps[1], toneAvailable, locales.MacInProgress(), locales.MacAllowProgress(), showRecoverySteps)
	case sip.State == session.RequiresAttention:
		set(&steps[1], toneAttention, locales.MacNeedsAction(), locales.MacAllowAction(), showRecoverySteps)
	default:
		set(&steps[1], toneAvailable, locales.MacNotChecked(), locales.MacAllowUnknown(), showRecoverySteps)
	}

	switch {
	case unsupported:
		steps[2].status = locales.MacNotAvailable()
	case restart.State == session.RequiresAttention:
		set(&steps[2], toneAttention, locales.MacNeedsAction(), locales.MacRestart(), noAction)
	case unavailable && loaded.State != session.CheckVerified:
		set(&steps[2], toneAvailable, locales.MacNotAvailable(), locales.MacInstallBlocked(), noAction)
	case unavailable:
		set(&steps[2], toneAvailable, locales.MacNotAvailable(), locales.MacUsbBlocked(), noAction)
	case loaded.State == session.CheckVerified:
		set(&steps[2], toneVerified, locales.MacDone(), locales.MacInstallDone(), noAction)
	case present.State == session.CheckVerified:
		set(&steps[2], toneAttention, locales.MacNeedsAction(), locales.MacInstallNotRunning(), noAction)
	case present.ID == "":
		steps[2].status = locales.MacNotChecked()
	default:
		set(&steps[2], toneAttention, locales.MacNeedsAction(), locales.MacInstallAction(), installDriver)
	}

	// Discovery is never shown as done: only observed keyboard enumeration
	// verifies the connection.
	switch {
	case unsupported:
		set(&steps[3], toneAvailable, locales.MacNotAvailable(), locales.MacConnectUnsupported(), noAction)
	case unavailable:
		set(&steps[3], toneAvailable, locales.MacWaiting(), locales.MacConnectWaiting(), noAction)
	case controllers.State == session.CheckVerified:
		set(&steps[3], toneVerified, locales.MacDone(), locales.MacConnectDone(), noAction)
	case controllers.State == session.Available:
		set(&steps[3], toneAvailable, locales.MacFound(), locales.MacConnectFound(), chooseConnection)
	case loaded.State != session.CheckVerified:
		set(&steps[3], toneAvailable, locales.MacWaiting(), locales.MacConnectWaiting(), noAction)
	case found > 1:
		set(&steps[3], toneAttention, locales.MacNeedsAction(), locales.MacConnectSeveral(), chooseConnection)
	default:
		set(&steps[3], toneAttention, locales.MacNeedsAction(), locales.MacConnectAction(), noAction)
	}
	return steps
}

func (a *app) detectionView(c *ui.Context, s session.Snapshot) {
	disabled := s.Busy || s.SideEffects || s.Stage == session.Indeterminate
	t := c.Theme()
	card(c, func() {
		muted(c, locales.MacIntro())
		for _, step := range a.macSteps(s) {
			ui.Column(c.Key(step.key)).FillWidth().Gap(t.Space(1.5)).Children(func() {
				ui.Row(c).FillWidth().Wrap().Gap(t.Space(2)).Children(func() {
					ui.Text(c, step.title).Bold().Shrink(1)
					grow(c)
					badge(c, step.key+"-state", step.tone, step.status)
				})
				if step.detail != "" {
					ui.Text(c.Key(step.key+"-detail"), step.detail)
				}
				switch step.action {
				// Steps a blocked row needs are shown outright; while the step
				// is only in progress or unknown they wait behind a disclosure.
				case showRecoverySteps:
					if step.tone == toneAttention {
						panelBody(c, func() { recoverySteps(c) })
					} else {
						ui.Collapsible(c.Key("recovery-steps"), locales.MacAllowShowSteps(), &a.flow.recoverySteps, func() {
							panelBody(c, func() { recoverySteps(c) })
						})
					}
				case installDriver:
					if inline(ui.Button(c.Key("driver-install"), locales.MacInstallButton())).Disabled(disabled).Clicked() {
						a.driverPanel.open = true
					}
				case chooseConnection:
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
			a.command(a.controller.Inspect())
		}
	})
	// Guided manual input stays reachable for those who want it, as a quiet
	// link after the setup, never beside or like the primary action.
	if inline(ui.Link(c.Key("flow-manual"), locales.FlowTypeYourself(), "")).
		Disabled(!a.detectionReady(s, a.transportFor("manual"))).Clicked() {
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

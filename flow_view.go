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
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		for i, title := range titles {
			color := c.Theme().TextMuted
			if flowStage(i) == a.flow.stage {
				color = c.Theme().Text
			}
			ui.Text(c, fmt.Sprint(i+1)+" · "+title).TextColor(color)
		}
	})
	ui.Text(c.Key("flow-position"), locales.FlowPosition(fmt.Sprint(int(a.flow.stage)+1), titles[a.flow.stage])).
		FontSize(20).Bold().Role(ui.RoleStatus).Focusable().Bind(&a.flow.heading)
	if !a.flow.initialized || a.flow.rendered != a.flow.stage {
		a.flow.heading.Focus(c)
		a.flow.rendered, a.flow.initialized = a.flow.stage, true
	}
	switch a.flow.stage {
	case flowDetection:
		a.detectionView(c, s)
	case flowSelection:
		if ui.Button(c.Key("flow-back-detection"), locales.FlowBackDetection()).Disabled(s.Busy || s.SideEffects || s.Stage == session.BootstrapSetup).Clicked() {
			a.flow.stage = flowDetection
		}
		ui.Row(c).Gap(12).Children(func() {
			if ui.Button(c.Key("smile-route"), locales.FlowSmile()).Disabled(s.Busy || s.SideEffects).Clicked() {
				a.civSwitchPanel.open = false
			}
			if ui.Button(c.Key("civ-open"), locales.CivTitle()).Disabled(s.Busy || s.SideEffects).Clicked() {
				a.civSwitchPanel.open = true
			}
		})
		if a.civSwitchPanel.open {
			if !s.Transport.Capabilities.Keyboard || !s.Transport.Capabilities.Gamepad {
				ui.Text(c.Key("civ-method-unavailable"), locales.FlowCivUnavailable()).Role(ui.RoleStatus)
				return
			}
			a.civSwitchView(c)
			if ui.PrimaryButton(c.Key("flow-civ-launch"), locales.FlowNextLaunch()).Disabled(s.Busy || a.controller.CivSwitchUnavailable() != "" ||
				!a.civSwitchPanel.prerequisites.PhysicalSwitch1Cartridge100 || !a.civSwitchPanel.prerequisites.EnglishUS ||
				!a.civSwitchPanel.prerequisites.SameReachableNetwork || !a.civSwitchPanel.prerequisites.LobbyChatFocused).Clicked() {
				a.flow.stage = flowLaunch
			}
			return
		}
		a.profileView(c)
		a.readinessView(c, s, false)
		if ui.Button(c.Key("selection-inspect"), locales.FlowCheckSelection()).Disabled(s.Busy || s.SideEffects || a.preferencesPending(s)).Clicked() {
			a.command(a.controller.Inspect())
		}
		if s.BootstrapSaved {
			ui.Text(c, locales.FlowSaved()).Role(ui.RoleStatus)
		}
		a.sessionView(c)
		referencePrerequisites(c, !s.Transport.Capabilities.Manual)
		a.attachView(c, s)
		if ui.PrimaryButton(c.Key("flow-next-launch"), locales.FlowNextLaunch()).Disabled(a.preferencesPending(s) || !s.CanStart()).Clicked() {
			a.flow.stage = flowLaunch
		}
	case flowLaunch:
		if ui.Button(c.Key("flow-back-selection"), locales.FlowBackSelection()).Disabled(s.Busy || s.SideEffects || s.Stage == session.Indeterminate).Clicked() {
			a.flow.stage = flowSelection
		}
		if a.civSwitchPanel.open || s.CivSwitch {
			a.civSwitchView(c)
		} else {
			a.sessionView(c)
		}
	case flowTools:
		if s.CivSwitch {
			a.civSwitchView(c)
		} else {
			a.sessionView(c)
			if !a.toolsPanel.open {
				if ui.Button(c.Key("tools-open"), locales.ToolsTitle()).Disabled(s.Busy).Clicked() {
					a.toolsPanel.open = true
				}
			} else if a.saveKit.open {
				a.saveKitView(c)
			} else {
				a.advancedToolsView(c)
			}
		}
	}
}

func (a *app) detectionView(c *ui.Context, s session.Snapshot) {
	ui.Text(c, locales.FlowDetectHint())
	disabled := s.Busy || s.SideEffects || s.Stage == session.Indeterminate
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
	ui.Text(c, locales.LanOverride())
	ui.TextInput(c.Key("lan-override"), &a.preferences.LANIPv4).Label(locales.LanOverride()).Placeholder(locales.AutomaticLAN()).Disabled(disabled)
	if ui.Button(c.Key("apply-input"), locales.ApplyPreferences()).Disabled(disabled).Clicked() {
		a.preferenceError = ""
		if err := a.applyPreferences(); err != nil {
			a.preferenceError = err.Error()
		}
	}
	if a.preferenceError != "" {
		ui.Text(c, a.preferenceError)
	}
	ui.Text(c.Key("session-stage"), stageTitle(s.Stage)).Role(ui.RoleStatus)
	a.readinessView(c, s, true)
	if ui.Button(c.Key("inspect"), locales.CheckReadiness()).Disabled(disabled || a.preferencesPending(s)).Clicked() {
		a.command(a.controller.Inspect())
	}
	if !s.Transport.Capabilities.Manual && !s.Transport.Capabilities.ExternalPico {
		ui.Column(c.Key("startup-security-note")).FillWidth().Gap(6).Padding(12).
			Border(1, c.Theme().Border).Background(c.Theme().Surface).Children(func() {
			ui.Text(c.Key("startup-security-state"), locales.StartupSecurityTitle()).Bold().Role(ui.RoleStatus)
			ui.Text(c, locales.StartupSecurityUnverified())
			ui.Text(c, locales.StartupSecurityAction())
		})
		ui.Text(c, locales.DriverSecurityGuidance())
		if ui.Button(c.Key("driver-setup"), locales.DriverSetup()).Disabled(disabled).Clicked() {
			a.driverPanel.open = true
		}
		if ui.Button(c.Key("usb-keyboard"), locales.UsbKeyboardTitle()).Disabled(disabled).Clicked() {
			a.keyboard.open = !a.keyboard.open
		}
		if a.keyboard.open {
			a.keyboardView(c)
		}
	}
	if !a.detectionReady(s) {
		ui.Text(c, locales.FlowDetectionBlocked())
	}
	if ui.PrimaryButton(c.Key("flow-next-selection"), locales.FlowNextSelection()).Disabled(!a.detectionReady(s)).Clicked() {
		a.stopKeyboard()
		a.keyboard.open = false
		a.flow.stage = flowSelection
	}
}

func (a *app) settingsView(c *ui.Context) {
	ui.Text(c, locales.Settings()).FontSize(20).Bold()
	a.profileView(c)
	if a.controller != nil {
		if ui.Button(c.Key("settings-driver"), locales.DriverSetup()).Clicked() {
			a.driverPanel.open = true
		}
		if ui.Button(c.Key("settings-keyboard"), locales.UsbKeyboardTitle()).Clicked() {
			a.keyboard.open = !a.keyboard.open
		}
		if a.keyboard.open {
			a.keyboardView(c)
		}
	}
	if a.about {
		ui.Text(c, locales.BuildLimitations())
	}
	if ui.Button(c.Key("about"), locales.AboutBuild()).Clicked() {
		a.about = !a.about
	}
}

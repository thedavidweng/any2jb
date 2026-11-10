package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

func (a *app) profileView(c *ui.Context) {
	p, _ := a.manifest.Profile(a.preferences.Profile)
	ui.Text(c, locales.HostProfile()+": "+p.Label)
	if ui.Button(c.Key("developer-options"), locales.DeveloperOptions()).Clicked() {
		a.developer = !a.developer
	}
	if !a.developer {
		return
	}
	disabled := false
	if a.controller != nil {
		s := a.controller.Snapshot()
		disabled = s.Busy || s.SideEffects || s.Stage == session.Indeterminate
	}
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, locales.ProfileSettings()).Bold()
		ids := make([]string, 0, len(a.manifest.Profiles))
		for _, profile := range a.manifest.Profiles {
			ids = append(ids, profile.ID)
		}
		ui.Text(c, locales.HostProfile())
		ui.Select(c.Key("host-profile"), &a.preferences.Profile, ids).Label(locales.HostProfile()).Disabled(disabled)
		editions := []string{"full"}
		if p.SupportsDemo {
			editions = append(editions, "demo")
		}
		ui.Text(c, locales.SmilebasicEdition()+": "+strings.Join(p.SupportedSmileBASIC, ", "))
		ui.Select(c.Key("smilebasic-edition"), &a.preferences.Edition, editions).Label(locales.SmilebasicEdition()).Disabled(disabled)
		ui.Text(c, locales.LanOverride())
		ui.TextInput(c.Key("lan-override"), &a.preferences.LANIPv4).Label(locales.LanOverride()).Placeholder(locales.LanOverride()).Disabled(disabled)
		ui.Text(c, locales.TransportMode())
		ui.Select(c.Key("transport-mode"), &a.preferences.Transport, []string{"manual", "keyboard", "composite", "external-pico"}).Label(locales.TransportMode()).Disabled(disabled)
		ui.Text(c, locales.TransportUnverified()).TextColor(c.Theme().TextMuted)
		if ui.Button(c.Key("apply-preferences"), locales.ApplyPreferences()).Disabled(disabled).Clicked() {
			a.preferenceError = ""
			if err := a.applyPreferences(); err != nil {
				a.preferenceError = err.Error()
			}
		}
		if a.preferenceError != "" {
			ui.Text(c, a.preferenceError)
		}
		ui.Text(c, fmt.Sprintf("%s · START: %d · TCP: %d", p.SaveName, p.StartArgumentCount, p.StartPort))
		ui.Button(c.Key("capability-rpc"), locales.HeldRPC()).Disabled(true)
		capabilityReason(c, p.SupportsHeldRPC, locales.RequiresVerifiedSession())
		if ui.Button(c.Key("capability-demo"), locales.DemoEdition()).Disabled(disabled || !p.SupportsDemo).Clicked() {
			a.preferences.Edition = "demo"
		}
		capabilityReason(c, p.SupportsDemo, locales.SupportedEdition())
		ui.Button(c.Key("capability-savekit"), locales.SaveKit()).Disabled(true)
		capabilityReason(c, p.SupportsSaveKit, locales.RequiresVerifiedSession())
		ui.Button(c.Key("capability-native"), locales.NativeLauncher()).Disabled(true)
		capabilityReason(c, p.SupportsNativeHBLauncher, locales.RequiresVerifiedSession())
		ui.Button(c.Key("capability-modloader"), locales.Modloader()).Disabled(true)
		capabilityReason(c, p.SupportsModloader, locales.RequiresVerifiedSession())
	})
}

func capabilityReason(c *ui.Context, supported bool, reason string) {
	if !supported {
		reason = locales.HostUnsupported()
	}
	ui.Text(c, reason).TextColor(c.Theme().TextMuted)
}

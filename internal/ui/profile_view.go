package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

func (a *app) profileView(c *ui.Context) {
	p, _ := a.Manifest.Profile(a.Preferences.Profile)
	keyValue(c, locales.HostProfile(), p.Label)
	if p.Engine == "izmysh" {
		ui.Text(c, locales.IzmyshUnavailable()).TextColor(c.Theme().TextMuted)
	}
	disabled := false
	if a.Controller != nil {
		s := a.Controller.Snapshot()
		disabled = s.Busy || s.SideEffects || s.Stage == session.Indeterminate
	}
	disabled = disabled || a.keyboard.forwarding || a.keyboard.completion != nil
	if a.flow.settings {
		ui.Text(c, locales.LanOverride())
		ui.TextInput(c.Key("lan-override"), &a.Preferences.LANIPv4).Label(locales.LanOverride()).Placeholder(locales.AutomaticLAN()).Disabled(disabled)
	}
	if inline(ui.Button(c.Key("apply-preferences"), locales.ApplyPreferences())).Disabled(disabled).Clicked() {
		a.preferenceError = ""
		if err := a.ApplyPreferences(); err != nil {
			a.preferenceError = err.Error()
		}
	}
	if a.preferenceError != "" {
		ui.Text(c, a.preferenceError)
	}
	if inline(ui.Button(c.Key("developer-options"), locales.DeveloperOptions())).Clicked() {
		a.developer = !a.developer
	}
	if !a.developer {
		return
	}
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, locales.ProfileSettings()).Bold()
		ids := make([]string, 0, len(a.Manifest.Profiles))
		for _, profile := range a.Manifest.Profiles {
			ids = append(ids, profile.ID)
		}
		ui.Text(c, locales.HostProfile())
		ui.Select(c.Key("host-profile"), &a.Preferences.Profile, ids).Label(locales.HostProfile()).Disabled(disabled)
		editions := []string{"full"}
		if p.SupportsDemo {
			editions = append(editions, "demo")
		}
		ui.Text(c, locales.LabeledList(locales.SmilebasicEdition(), strings.Join(p.SupportedSmileBASIC, locales.ListSeparator())))
		ui.Select(c.Key("smilebasic-edition"), &a.Preferences.Edition, editions).Label(locales.SmilebasicEdition()).Disabled(disabled)
		if a.flow.settings {
			ui.Text(c, locales.TransportMode())
			ui.Select(c.Key("transport-mode"), &a.Preferences.Transport, []string{"manual", "keyboard", "composite", "external-pico"}).Label(locales.TransportMode()).Disabled(disabled)
		}
		ui.Text(c, locales.TransportUnverified()).TextColor(c.Theme().TextMuted)
		ui.Text(c, fmt.Sprintf("%s · START: %d · TCP: %d", p.SaveName, p.StartArgumentCount, p.StartPort))
		inline(ui.Button(c.Key("capability-rpc"), locales.HeldRPC())).Disabled(true)
		capabilityReason(c, p.SupportsHeldRPC, locales.RequiresVerifiedSession())
		if inline(ui.Button(c.Key("capability-demo"), locales.DemoEdition())).Disabled(disabled || !p.SupportsDemo).Clicked() {
			a.Preferences.Edition = "demo"
		}
		capabilityReason(c, p.SupportsDemo, locales.SupportedEdition())
		inline(ui.Button(c.Key("capability-savekit"), locales.SaveKit())).Disabled(true)
		capabilityReason(c, p.SupportsSaveKit, locales.RequiresVerifiedSession())
		inline(ui.Button(c.Key("capability-native"), locales.NativeLauncher())).Disabled(true)
		capabilityReason(c, p.SupportsNativeHBLauncher, locales.RequiresVerifiedSession())
		inline(ui.Button(c.Key("capability-modloader"), locales.Modloader())).Disabled(true)
		capabilityReason(c, p.SupportsModloader, locales.RequiresVerifiedSession())
	})
}

func capabilityReason(c *ui.Context, supported bool, reason string) {
	if !supported {
		reason = locales.HostUnsupported()
	}
	ui.Text(c, reason).TextColor(c.Theme().TextMuted)
}

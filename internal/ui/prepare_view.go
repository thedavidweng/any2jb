package ui

import (
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

const ninetyDNSURL = "https://gitlab.com/a/90dns"

// prepared holds the SmileBASIC route's console-side confirmations and the
// shared ones CivSwitch does not take. They are presentation only: nothing
// here is evidence, and no readiness check or launch gate reads it.
type prepared struct {
	network, updates, cable                   bool
	version, smileBASIC, keyboard, directMode bool
}

// prepareDone reports whether every item of the selected route is ticked.
// CivSwitch's items are its launch prerequisites themselves, so StartCivSwitch
// receives exactly what was ticked here.
func (a *app) prepareDone() bool {
	p, r := a.civSwitchPanel.prerequisites, a.flow.prepared
	if a.civSwitchPanel.open {
		return p.SameReachableNetwork && r.updates && r.cable &&
			p.PhysicalSwitch1Cartridge100 && p.EnglishUS && p.LobbyChatFocused
	}
	return r.network && r.updates && r.cable && r.version && r.smileBASIC && r.keyboard && r.directMode
}

func (a *app) prepareView(c *ui.Context, s session.Snapshot) {
	t := c.Theme()
	civ := a.civSwitchPanel.open
	card(c, func() {
		muted(c, locales.PrepIntro())
		field(c, locales.PrepRoute(), func() {
			// Radio buttons mark the chosen route by shape and announce it as
			// chosen; the arrows move between the two.
			ui.RadioGroup(c.Key("route"), func() {
				ui.Radio(c.Key("smile-route"), &a.civSwitchPanel.open, false, locales.FlowSmile()).Disabled(s.Busy || s.SideEffects)
				ui.Radio(c.Key("civ-open"), &a.civSwitchPanel.open, true, locales.CivTitle()).Disabled(s.Busy || s.SideEffects)
			}).Row().Wrap().Gap(t.Space(5)).Label(locales.PrepRoute())
		})
	})
	if civ && (!s.Transport.Capabilities.Keyboard || !s.Transport.Capabilities.Gamepad) {
		card(c, func() {
			stateCard(c, func() {
				badge(c, "civ-method-badge", toneAttention, locales.CheckAttention())
				ui.Text(c.Key("civ-method-unavailable"), locales.FlowCivUnavailable()).Role(ui.RoleStatus)
			})
		})
		return
	}
	p, r := &a.civSwitchPanel.prerequisites, &a.flow.prepared
	// CivSwitch's own network prerequisite is this shared item, so the same
	// requirement is never asked twice.
	network := &r.network
	if civ {
		network = &p.SameReachableNetwork
	}
	card(c, func() {
		sectionTitle(c, locales.PrepConnection())
		checklist(c, func() {
			confirmation(c, "", func() { ui.Checkbox(c.Key("prep-network"), network, locales.PrepNetwork()).FillWidth() })
			confirmation(c, locales.PrepUpdatesNote(), func() { ui.Checkbox(c.Key("prep-updates"), &r.updates, locales.PrepUpdates()).FillWidth() },
				func() { inline(ui.Link(c.Key("prep-guide"), locales.PrepUpdatesLink(), ninetyDNSURL)) })
			confirmation(c, locales.PrepCableNote(), func() { ui.Checkbox(c.Key("prep-cable"), &r.cable, locales.PrepCable()).FillWidth() })
		})
	})
	card(c, func() {
		sectionTitle(c, locales.PrepConsole())
		checklist(c, func() {
			if civ {
				confirmation(c, "", func() {
					ui.Checkbox(c.Key("civ-cartridge"), &p.PhysicalSwitch1Cartridge100, locales.CivCartridge()).FillWidth()
				})
				confirmation(c, "", func() { ui.Checkbox(c.Key("civ-language"), &p.EnglishUS, locales.CivLanguage()).FillWidth() })
				confirmation(c, "", func() { ui.Checkbox(c.Key("civ-chat"), &p.LobbyChatFocused, locales.CivChat()).FillWidth() })
				return
			}
			confirmation(c, locales.PrepVersionNote(), func() { ui.Checkbox(c.Key("prep-version"), &r.version, locales.PrepVersion()).FillWidth() })
			confirmation(c, locales.PrepSmilebasicNote(), func() {
				ui.Checkbox(c.Key("prep-smilebasic"), &r.smileBASIC, locales.PrepSmilebasic()).FillWidth()
			})
			confirmation(c, "", func() { ui.Checkbox(c.Key("prep-keyboard"), &r.keyboard, locales.PrepKeyboard()).FillWidth() })
			confirmation(c, locales.PrepDirectNote(), func() { ui.Checkbox(c.Key("prep-direct"), &r.directMode, locales.PrepDirect()).FillWidth() })
		})
	})
	muted(c, locales.PrepNotSaved())
}

func checklist(c *ui.Context, items func()) {
	ui.Column(c).FillWidth().Gap(c.Theme().Space(3)).Children(items)
}

// confirmation is one checklist item: its check box, then an optional note
// and link aligned with the label, past the box and its gap.
func confirmation(c *ui.Context, note string, box func(), more ...func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		box()
		if note == "" {
			return
		}
		ui.Column(c).FillWidth().Gap(t.Space(1)).Padding(0, 0, 0, t.Space(6)).Children(func() {
			muted(c, note)
			for _, fn := range more {
				fn()
			}
		})
	})
}

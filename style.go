package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

const contentWidth = 720

func rgb(r, g, b uint8) ui.Color { return ui.Color{R: r, G: g, B: b, A: 255} }

// Keep the system accent, contrast and text size on neutral surfaces.
func applyTheme(c *ui.Context) {
	t := *c.Theme()
	t.Radius = 5
	if t.Dark {
		t.Background = rgb(20, 20, 22)
		t.Danger, t.Warning, t.Success = rgb(248, 113, 113), rgb(251, 191, 36), rgb(74, 222, 128)
	} else {
		t.Background = rgb(250, 250, 250)
		t.Danger, t.Warning, t.Success = rgb(180, 35, 24), rgb(154, 91, 0), rgb(20, 116, 58)
	}
	c.SetTheme(&t)
}

func cardFill(c *ui.Context) ui.Color {
	return c.Theme().Background
}

func rule(c *ui.Context) {
	ui.Column(c).FillWidth().Height(1).Shrink(0).Background(c.Theme().Border)
}

func grow(c *ui.Context) { ui.Row(c).Grow(1) }

// card separates tasks without adding another surface.
func card(c *ui.Context, fn func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(3)).Children(func() {
		rule(c)
		ui.Column(c).FillWidth().Gap(t.Space(2)).Children(fn)
	})
}

func sectionTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(c.Theme().Rem(1.08)).Bold()
}

func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}

// footer is a stage's action bar. The leading control (Back, or why the
// stage is blocked) sits at the start and the primary action at the end,
// where macOS places a default button.
func footer(c *ui.Context, lead func(), rest func()) {
	rule(c)
	ui.Row(c).FillWidth().Justify(ui.Center).Padding(c.Theme().Space(3), 20).Background(cardFill(c)).Children(func() {
		ui.Row(c).FillWidth().MaxWidth(contentWidth).Wrap().Gap(c.Theme().Space(2)).Children(func() {
			lead()
			grow(c)
			rest()
		})
	})
}

// inline keeps a lone secondary button at its natural width instead of
// stretching across the column.
func inline(e ui.Element) ui.Element { return e.AlignSelf(ui.Start) }

type tone int

const (
	toneAttention tone = iota
	toneAvailable
	toneVerified
	toneManual
)

func checkTone(state session.CheckState) tone {
	switch state {
	case session.CheckVerified:
		return toneVerified
	case session.Available:
		return toneAvailable
	default:
		return toneAttention
	}
}

// Each state has its own symbol and its own words; the color only repeats
// what they already say.
func (k tone) look(c *ui.Context) (symbol string, color ui.Color) {
	t := c.Theme()
	switch k {
	case toneVerified:
		return "✓", t.TextMuted
	case toneAvailable:
		return "○", t.TextMuted
	case toneManual:
		return "?", t.Text
	default:
		return "!", t.Danger
	}
}

// badge shows a state as a symbol plus its label. The label is its own text
// element so it stays an exact, readable string for assistive technology.
func badge(c *ui.Context, key string, k tone, label string) {
	symbol, color := k.look(c)
	t := c.Theme()
	ui.Row(c).Gap(t.Space(1.5)).AlignSelf(ui.Start).Children(func() {
		ui.Text(c, symbol).TextColor(color).Shrink(0)
		ui.Text(c.Key(key), label).TextColor(color).Role(ui.RoleStatus).Shrink(1)
	})
}

// Only a manual check gets a frame: it is not an observation made by the app.
func stateCard(c *ui.Context, k tone, fn func()) {
	t := c.Theme()
	group := ui.Column(c).FillWidth().Gap(t.Space(1.5))
	if k == toneManual {
		group.Padding(t.Space(3)).Radius(t.Radius).
			Border(1, t.Border).BorderStyle(ui.BorderDashed)
	}
	group.Children(fn)
}

// meter draws determinate progress. The adjacent "Progress: n / total" text
// carries the value for assistive technology.
func meter(c *ui.Context, fraction float32) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(6).Radius(3).Background(t.Border).Children(func() {
		ui.Column(c).WidthPercent(100 * max(0, min(fraction, 1))).Height(6).Radius(3).Background(t.Accent)
	})
}

// The ordered stages use weight, not accent-filled circles, for position.
func stepper(c *ui.Context, current flowStage, titles []string) {
	t := c.Theme()
	ui.Row(c).FillWidth().Wrap().Gap(t.Space(2)).Children(func() {
		for i, title := range titles {
			stage := flowStage(i)
			ui.Row(c).Gap(t.Space(1.5)).Children(func() {
				fg := t.TextMuted
				if stage <= current {
					fg = t.Text
				}
				ui.Text(c, string(rune('1'+i))).FontSize(t.Rem(0.9)).TextColor(fg)
				label := ui.Text(c, title).SingleLine()
				switch {
				case stage == current:
					label.Bold()
				case stage > current:
					label.TextColor(t.TextMuted)
				}
			})
			if i < len(titles)-1 {
				ui.Column(c).Size(16, 1).Background(t.Border)
			}
		}
	})
}

// enter fades a stage in once when it first appears, and returns which one
// that was. Stage changes are rare and purposeful, so they may move; MyGo
// drops the movement under Reduce Motion and keeps only the fade.
func enter(c *ui.Context, e ui.Element, entered, id int) int {
	p := ui.Pose{}
	if entered != id {
		p = ui.Pose{Y: 6, Fade: 1}
		c.AnimationFrame()
	}
	e.AnimatePose(p, ui.Animation{Duration: 200 * time.Millisecond, Ease: easeOutQuint})
	return id
}

func easeOutQuint(t float32) float32 {
	u := 1 - t
	return 1 - u*u*u*u*u
}

// checkName names a check's subject, so a row reads subject, state, action.
// A controller check carries its identifier, which is the subject itself.
func checkName(check session.Check) string {
	switch check.ID {
	case "network":
		return locales.CheckNetwork()
	case "sip":
		return locales.CheckSip()
	case "driver-present":
		return locales.CheckDriverFiles()
	case "driver-loaded":
		return locales.CheckDriverLoaded()
	case "driver-restart":
		return locales.CheckDriverRestart()
	case "usb-platform":
		return locales.CheckUsbPort()
	case "usb-controllers":
		return locales.CheckUsbController()
	case "runtime":
		return locales.CheckRuntime()
	case "engine":
		return locales.CheckHostProgram()
	case "usb-runtime":
		return locales.CheckUsbHelpers()
	case "input-adapter":
		return locales.CheckTypingMethod()
	}
	if check.Controller != "" {
		return locales.CheckUsbController() + " " + check.Controller
	}
	return locales.CheckOther()
}

// panelBody is the inside of a disclosure, which keeps expert or precise
// detail available without crowding the stage.
func panelBody(c *ui.Context, fn func()) {
	ui.Column(c).FillWidth().Gap(c.Theme().Space(2)).Padding(c.Theme().Space(2), 0, 0).Children(fn)
}

func keyValue(c *ui.Context, label, value string) {
	field(c, label, func() { ui.Text(c, value).Shrink(1) })
}

// field puts a value or a control in the column keyValue's values share.
func field(c *ui.Context, label string, value func()) {
	ui.Row(c).FillWidth().Wrap().Gap(c.Theme().Space(2)).AlignItems(ui.Center).Children(func() {
		muted(c, label).MinWidth(c.Theme().Rem(8)).Shrink(0)
		value()
	})
}

// steps shows where a run stands. The current step is marked and named, so
// the state never depends on color alone.
func steps(c *ui.Context, names []string, current int) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(1.5)).Children(func() {
		for i, name := range names {
			ui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
				mark, color := "○", t.TextMuted
				switch {
				case i < current:
					mark, color = "✓", t.TextMuted
				case i == current:
					mark, color = "▶", t.Text
				}
				ui.Text(c, mark).TextColor(color).Shrink(0)
				label := ui.Text(c, name).Shrink(1)
				if i == current {
					label.Bold()
				} else if i > current {
					label.TextColor(t.TextMuted)
				}
			})
		}
	})
}

func manualBadge(c *ui.Context) {
	badge(c, "startup-security-badge", toneManual, locales.CheckManual())
}

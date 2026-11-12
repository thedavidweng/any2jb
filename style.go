package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

const contentWidth = 720

func rgb(r, g, b uint8) ui.Color { return ui.Color{R: r, G: g, B: b, A: 255} }

// applyTheme keeps the system appearance, accent, contrast and text size,
// and only deepens the status colors so state text meets 4.5:1 contrast on
// the light window background.
func applyTheme(c *ui.Context) {
	t := *c.Theme()
	t.Radius = 7
	if t.Dark {
		t.Background = rgb(20, 20, 22)
		t.Danger, t.Warning, t.Success = rgb(248, 113, 113), rgb(251, 191, 36), rgb(74, 222, 128)
	} else {
		t.Background = rgb(244, 245, 247)
		t.Danger, t.Warning, t.Success = rgb(180, 35, 24), rgb(154, 91, 0), rgb(20, 116, 58)
	}
	c.SetTheme(&t)
}

func cardFill(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return rgb(30, 30, 33)
	}
	return rgb(255, 255, 255)
}

func rule(c *ui.Context) {
	ui.Column(c).FillWidth().Height(1).Shrink(0).Background(c.Theme().Border)
}

func grow(c *ui.Context) { ui.Row(c).Grow(1) }

// card groups one task of a stage on a raised surface.
func card(c *ui.Context, fn func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(t.Space(3)).Padding(t.Space(4)).Radius(t.Radius+3).
		Background(cardFill(c)).Border(1, t.Border).Children(fn)
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
		return "✓", t.Success
	case toneAvailable:
		return "○", t.Warning
	case toneManual:
		return "?", t.Accent
	default:
		return "!", t.Danger
	}
}

// badge shows a state as a symbol plus its label. The label is its own text
// element so it stays an exact, readable string for assistive technology.
func badge(c *ui.Context, key string, k tone, label string) {
	symbol, color := k.look(c)
	t := c.Theme()
	ui.Row(c).Gap(t.Space(1.5)).AlignSelf(ui.Start).Padding(t.Space(0.5), t.Space(2)).
		Radius(t.Space(3)).Background(color.Alpha(0.12)).Children(func() {
		ui.Text(c, symbol).Bold().TextColor(color)
		ui.Text(c.Key(key), label).Bold().TextColor(color).Role(ui.RoleStatus)
	})
}

// stateCard frames one observed fact; its edge repeats the badge's color.
func stateCard(c *ui.Context, k tone, fn func()) {
	_, color := k.look(c)
	t := c.Theme()
	border, style := t.Border, ui.BorderSolid
	switch k {
	case toneAttention:
		border = color.Alpha(0.55)
	case toneManual:
		// Dashed, so "nobody checked this" never reads as a checked result.
		border, style = color.Alpha(0.7), ui.BorderDashed
	}
	ui.Column(c).FillWidth().Gap(t.Space(1.5)).Padding(t.Space(3)).Radius(t.Radius+1).
		Background(cardFill(c)).Border(1, border).BorderStyle(style).Children(fn)
}

// meter draws determinate progress. The adjacent "Progress: n / total" text
// carries the value for assistive technology.
func meter(c *ui.Context, fraction float32) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(6).Radius(3).Background(t.Border).Children(func() {
		ui.Column(c).WidthPercent(100 * max(0, min(fraction, 1))).Height(6).Radius(3).Background(t.Accent)
	})
}

// stepper shows where the four stages stand. The current one is filled and
// bold, and the heading below names it in words.
func stepper(c *ui.Context, current flowStage, titles []string) {
	t := c.Theme()
	ui.Row(c).FillWidth().Wrap().Gap(t.Space(2)).Children(func() {
		for i, title := range titles {
			stage := flowStage(i)
			ui.Row(c).Gap(t.Space(1.5)).Children(func() {
				fill, fg, ring := ui.Transparent, t.TextMuted, t.Border
				switch {
				case stage == current:
					fill, fg, ring = t.Accent, t.AccentText, t.Accent
				case stage < current:
					fg, ring = t.Text, t.TextMuted
				}
				ui.Column(c).Size(20, 20).Radius(10).Background(fill).Border(1.5, ring).Center().Children(func() {
					ui.Text(c, string(rune('1'+i))).FontSize(t.Rem(0.82)).Bold().TextColor(fg)
				})
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

func manualBadge(c *ui.Context) {
	badge(c, "startup-security-badge", toneManual, locales.CheckManual())
}

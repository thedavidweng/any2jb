package ui

import (
	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

func (a *app) manualView(c *ui.Context, s session.Snapshot) {
	if a.Manual == nil {
		return
	}
	p := a.Manual.Prompt()
	if p.Kind == "" || p.RunID != s.RunID {
		return
	}
	consoleStepView(c, locales.ManualProfile(s.Profile.ID, s.Edition), appstate.ManualInstruction(p.Kind))
	if p.Text != "" {
		text := p.Text
		height := float32(64)
		if p.Kind == "bootstrap" {
			height = 160
		}
		ui.TextArea(c.Key("manual-text"), &text).Label(locales.ConsoleText()).ReadOnly(true).Height(height)
		if inline(ui.Button(c.Key("manual-copy"), locales.CopyConsoleText())).Clicked() {
			c.Services().WriteClipboard(p.Text)
		}
	}
	if inline(ui.PrimaryButton(c.Key("manual-acknowledge"), locales.ManualAcknowledge())).Clicked() {
		a.Manual.Acknowledge(p.RunID, p.Sequence)
	}
}

func usbPromptView(c *ui.Context, p *appstate.USBPrompt) {
	step, profile, edition, id, pending := p.CurrentWithIdentity(c.Services().Invalidate)
	if !pending {
		return
	}
	consoleStepView(c, locales.UsbProfile(profile, edition), appstate.ConsolePrompt(step))
	if ui.PrimaryButton(c.Key("usb-confirm-console"), locales.UsbConfirmContinue()).Clicked() {
		p.Confirm(id)
	}
}

func consoleStepView(c *ui.Context, title, instruction string) {
	ui.Text(c, title).Bold()
	ui.Text(c, instruction)
}

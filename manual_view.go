package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

func (a *app) manualView(c *ui.Context, s session.Snapshot) {
	if a.manual == nil {
		return
	}
	p := a.manual.Prompt()
	if p.Kind == "" || p.RunID != s.RunID {
		return
	}
	consoleStepView(c, locales.ManualProfile(s.Profile.ID, s.Edition), manualInstruction(p.Kind))
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
		a.manual.Acknowledge(p.RunID, p.Sequence)
	}
}

func manualInstruction(kind string) string {
	return map[string]string{
		"slot": locales.ManualSlot(), "bootstrap": locales.ManualBootstrap(),
		"direct": locales.ManualDirect(), "save": locales.ManualSave(),
		"load": locales.ManualLoad(), "load-confirm": locales.ManualLoadConfirm(),
		"run": locales.ManualRun(), "remove": locales.ManualRemove(), "start": locales.ManualStart(),
	}[kind]
}

func consoleStepView(c *ui.Context, title, instruction string) {
	ui.Text(c, title).Bold()
	ui.Text(c, instruction)
}

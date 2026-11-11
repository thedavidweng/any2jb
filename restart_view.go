package main

import (
	"context"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

type restartUI struct {
	busy      bool
	errorText string
	update    func(func())
}

// Both console routes use this deliberate confirmation and the same controller
// command. Lease checks run off the UI thread, and failures leave recovery gated.
func (a *app) restartView(c *ui.Context, s session.Snapshot) {
	p := &a.restart
	if s.Stage != session.Indeterminate {
		return
	}
	ui.Text(c, locales.RestartConsole()).Bold()
	if s.ReleaseUnconfirmed {
		ui.Text(c, localize(session.ReleaseUnconfirmedWarning))
	}
	if p.errorText != "" {
		ui.Text(c, localize(p.errorText))
	}
	if ui.Button(c.Key("console-restart"), locales.ConsoleRestartConfirmed()).
		Disabled(s.Busy || p.busy || p.update == nil).Clicked() {
		p.busy, p.errorText = true, ""
		controller, update := a.controller, p.update
		go func() {
			_, err := controller.ConfirmConsoleRestart(context.Background(), true)
			update(func() {
				p.busy = false
				if err != nil {
					p.errorText = err.Error()
				} else {
					a.civSwitchPanel.prerequisites.LobbyChatFocused = false
				}
			})
		}()
	}
}

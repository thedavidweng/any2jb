package main

import (
	"context"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
)

type restartUI struct {
	completion chan error
	errorText  string
}

// Both console routes use this deliberate confirmation and the same controller
// command. Lease checks run off the UI thread, and failures leave recovery gated.
func (a *app) restartView(c *ui.Context, s session.Snapshot) {
	p := &a.restart
	if p.completion != nil {
		select {
		case err := <-p.completion:
			p.completion, p.errorText = nil, ""
			if err != nil {
				p.errorText = err.Error()
			} else {
				a.civSwitchPanel.prerequisites.LobbyChatFocused = false
			}
		default:
		}
	}
	if s.Stage != session.Indeterminate {
		return
	}
	ui.Text(c, locales.RestartConsole()).Bold()
	if p.errorText != "" {
		ui.Text(c, p.errorText)
	}
	if ui.Button(c.Key("console-restart"), locales.ConsoleRestartConfirmed()).
		Disabled(s.Busy || p.completion != nil).Clicked() {
		p.completion, p.errorText = make(chan error, 1), ""
		ch, controller, services := p.completion, a.controller, c.Services()
		go func() {
			_, err := controller.ConfirmConsoleRestart(context.Background(), true)
			ch <- err
			services.Invalidate()
		}()
	}
}

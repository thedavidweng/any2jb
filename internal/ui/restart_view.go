package ui

import (
	"context"

	"github.com/egoist/mygo/ui"
	appstate "github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

type restartUI struct {
	busy   bool
	err    error
	update func(func())
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
		ui.Text(c, locales.DynReleaseUnconfirmed()).Bold()
	}
	if p.err != nil {
		ui.Text(c, appstate.PresentError(p.err)).TextColor(c.Theme().Danger)
	}
	if inline(ui.PrimaryButton(c.Key("console-restart"), locales.ConsoleRestartConfirmed())).
		Disabled(s.Busy || p.busy || p.update == nil).Clicked() {
		p.busy, p.err = true, nil
		controller, update := a.Controller, p.update
		go func() {
			_, err := controller.ConfirmConsoleRestart(context.Background(), true)
			update(func() {
				p.busy = false
				if err != nil {
					p.err = err
				} else {
					a.civSwitchPanel.prerequisites.LobbyChatFocused = false
				}
			})
		}()
	}
}

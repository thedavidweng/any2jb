package main

import (
	"context"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/driver"
	"github.com/thedavidweng/any2jb/locales"
)

type driverPanel struct {
	gate          *driver.Gate
	open, confirm bool
	busy          bool
	result        driver.Result
	failed        bool
	update        func(func())
}

func (a *app) driverView(c *ui.Context) {
	p := &a.driverPanel
	ui.Text(c, locales.DriverTitle()).FontSize(20).Bold()
	ui.Text(c, locales.DriverDisclosure()).Bold()
	ui.Text(c, locales.DriverSigning())
	ui.Text(c, locales.DriverRecovery())
	ui.Text(c, locales.DriverApproval())
	ui.Text(c, locales.DriverRestart())
	ui.Text(c, locales.DriverNoSecurityChange())
	if p.gate.DeveloperSource() {
		ui.Text(c, locales.DriverDeveloper()).Bold()
	}
	availability := p.gate.Availability()
	if availability != nil {
		ui.Text(c, localize(availability.Error()))
	}
	if p.failed {
		if p.result.Attempted {
			ui.Text(c, locales.DriverInstallError())
		} else {
			ui.Text(c, locales.DriverVerifyError())
		}
	}
	if p.result.RestartRequired {
		ui.Text(c, locales.DriverRestart()).Bold()
	}
	busy := p.busy
	if busy {
		ui.Text(c, locales.DriverInstalling())
	}
	if p.confirm {
		ui.Text(c, locales.DriverConfirmTitle()).Bold()
		ui.Text(c, locales.DriverConfirmText())
		if ui.PrimaryButton(c.Key("driver-confirm"), locales.DriverConfirm()).Disabled(busy || availability != nil || p.result.Attempted || p.update == nil).Clicked() {
			p.confirm, p.busy = false, true
			gate, update := p.gate, p.update
			go func() {
				result, err := gate.Install(context.Background(), true)
				update(func() {
					p.busy, p.result, p.failed = false, result, err != nil
					if result.Attempted && a.controller != nil {
						a.command(a.controller.Inspect())
					}
				})
			}()
		}
		if ui.Button(c.Key("driver-back"), locales.DriverBack()).Disabled(busy).Clicked() {
			p.confirm = false
		}
	} else if ui.Button(c.Key("driver-approve"), locales.DriverApprove()).Disabled(busy || availability != nil || p.result.Attempted).Clicked() {
		p.confirm = true
	}
	if ui.Button(c.Key("driver-decline"), locales.DriverDecline()).Disabled(busy).Clicked() {
		p.open, p.confirm = false, false
	}
	if ui.Button(c.Key("driver-manual"), locales.DriverManual()).Disabled(busy).Clicked() {
		previous := a.preferences.Transport
		a.preferences.Transport = "manual"
		if err := a.applyPreferences(); err != nil {
			a.preferences.Transport = previous
			a.preferenceError = err.Error()
		} else {
			p.open, p.confirm = false, false
		}
	}
}

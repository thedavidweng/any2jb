package main

import (
	"context"
	"errors"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/thedavidweng/any2jb/locales"
	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

type usbPrompt struct {
	mu               sync.Mutex
	step             transport.ConsoleStep
	sequence         uint64
	pending          chan struct{}
	notify           func()
	profile, edition string
}

func (p *usbPrompt) Wait(ctx context.Context, run session.Run, step transport.ConsoleStep) error {
	if consolePrompt(step) == "" {
		return errors.New("unsupported USB console confirmation")
	}
	p.mu.Lock()
	if p.pending != nil {
		p.mu.Unlock()
		return errors.New("USB confirmation already pending")
	}
	done := make(chan struct{})
	p.pending = done
	p.step = step
	p.profile, p.edition = run.Profile.ID, run.Edition
	p.sequence++
	notify := p.notify
	p.mu.Unlock()
	if notify != nil {
		notify()
	}
	defer func() {
		p.mu.Lock()
		if p.pending == done {
			p.pending = nil
		}
		notify := p.notify
		p.mu.Unlock()
		if notify != nil {
			notify()
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (p *usbPrompt) view(c *ui.Context) {
	p.mu.Lock()
	p.notify = c.Services().Invalidate
	step, id, pending := p.step, p.sequence, p.pending != nil
	profile, edition := p.profile, p.edition
	p.mu.Unlock()
	if !pending {
		return
	}
	consoleStepView(c, locales.UsbProfile(profile, edition), consolePrompt(step))
	if ui.PrimaryButton(c.Key("usb-confirm-console"), locales.UsbConfirmContinue()).Clicked() {
		p.confirm(id)
	}
}

// current reports the pending console step and its sequence.
func (p *usbPrompt) current() (transport.ConsoleStep, uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.step, p.sequence, p.pending != nil
}

// confirm releases only the step it was shown for, never a later one.
func (p *usbPrompt) confirm(id uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == nil || p.sequence != id {
		return false
	}
	close(p.pending)
	p.pending = nil
	return true
}

func consolePrompt(step transport.ConsoleStep) string {
	switch step {
	case transport.EditorFocus:
		return locales.UsbConfirmEditor()
	case transport.LoadCommand:
		return locales.UsbConfirmLoad()
	case transport.LoadPrompt:
		return locales.UsbConfirmLoadPrompt()
	case transport.RunCommand:
		return locales.UsbConfirmRun()
	case transport.RemoveCommand:
		return locales.UsbConfirmRemove()
	case transport.StartCommand:
		return locales.UsbConfirmStart()
	case transport.SaveCommand:
		return locales.UsbConfirmSave()
	}
	return ""
}

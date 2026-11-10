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
	mu       sync.Mutex
	step     transport.ConsoleStep
	sequence uint64
	pending  chan struct{}
	notify   func()
}

func (p *usbPrompt) Wait(ctx context.Context, _ session.Run, step transport.ConsoleStep) error {
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
		return ctx.Err()
	}
}

func (p *usbPrompt) view(c *ui.Context) {
	p.mu.Lock()
	p.notify = c.Services().Invalidate
	step, id, pending := p.step, p.sequence, p.pending
	p.mu.Unlock()
	if pending == nil {
		return
	}
	ui.Text(c, consolePrompt(step)).Bold()
	if ui.PrimaryButton(c.Key("usb-confirm-console"), locales.UsbConfirmContinue()).Clicked() {
		p.mu.Lock()
		if p.pending == pending && p.sequence == id {
			close(pending)
			p.pending = nil
		}
		p.mu.Unlock()
	}
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

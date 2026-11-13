package transport

import (
	"context"
	"errors"

	"github.com/thedavidweng/any2jb/internal/session"
)

// Dispatcher never silently substitutes manual input for a USB operation.
// Inspect is the same read-only prerequisite adapter used by the controller.
type Dispatcher struct {
	manual  *Manual
	usbHost USBKeyboard
	inspect func(context.Context, session.Run) ([]session.Check, error)
}

func NewDispatcher(manual *Manual, usbHost USBKeyboard, inspect func(context.Context, session.Run) ([]session.Check, error)) *Dispatcher {
	return &Dispatcher{manual: manual, usbHost: usbHost, inspect: inspect}
}

func (d *Dispatcher) Manual() *Manual  { return d.manual }
func (d *Dispatcher) USB() USBKeyboard { return d.usbHost }

func keyboardOnly(run session.Run) bool {
	c := run.Transport.Capabilities
	return c.Keyboard && !c.Manual && !c.Gamepad && !c.ExternalPico
}

func (d *Dispatcher) usb(ctx context.Context, run session.Run) error {
	if !keyboardOnly(run) || d.usbHost == nil {
		return errors.New("USB input unavailable: this transport requires its own adapter")
	}
	if d.inspect == nil {
		return errors.New("USB input unavailable: prerequisite inspection is not configured")
	}
	checks, err := d.inspect(ctx, run)
	if err != nil {
		return err
	}
	if err := session.KeyboardPrerequisites(checks, d.usbHost.SelectedKeyboardController()); err != nil {
		return err
	}
	return d.usbHost.Check(ctx, run)
}

func (d *Dispatcher) Send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) error {
	if run.Transport.Capabilities.Manual {
		if d.manual == nil {
			return errors.New("guided manual input unavailable: adapter is not configured")
		}
		return d.manual.Send(ctx, run, action, host, report)
	}
	if err := d.usb(ctx, run); err != nil {
		return err
	}
	return d.usbHost.Send(ctx, run, action, host, report)
}

func (d *Dispatcher) Release(ctx context.Context, run session.Run) error {
	if run.Transport.Capabilities.Manual && d.manual != nil {
		return d.manual.Release(ctx, run)
	}
	if d.usbHost != nil {
		return d.usbHost.Release(ctx, run)
	}
	return nil
}

func (d *Dispatcher) OpenKeyboardSession(ctx context.Context, run session.Run, report session.Report) (session.Keyboard, error) {
	if err := d.usb(ctx, run); err != nil {
		return nil, err
	}
	return d.usbHost.OpenKeyboardSession(ctx, run, report)
}

func (d *Dispatcher) SelectedKeyboardController() string {
	if d.usbHost == nil {
		return ""
	}
	return d.usbHost.SelectedKeyboardController()
}

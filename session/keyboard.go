package session

import (
	"context"
	"errors"
	"time"
)

// Keyboard is one controller-owned live input lease.
type Keyboard interface {
	Send(context.Context, [8]byte) error
	Close(context.Context) error
}

type KeyboardTransport interface {
	OpenKeyboardSession(context.Context, Run, Report) (Keyboard, error)
	SelectedKeyboardController() string
}

// StartKeyboard owns the whole live mode, including inspection and teardown.
// Available controller discovery permits an enumeration attempt, not key input:
// OpenKeyboardSession must wait for real helper readiness before returning.
func (c *SessionController) StartKeyboard() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Idle, NeedsSetup, Ready, Finished, Failed) || c.s.SideEffects {
		return c.reject(InvalidState)
	}
	caps := c.s.Transport.Capabilities
	input, ok := c.env.Input.(KeyboardTransport)
	if !ok || !caps.Keyboard || caps.Manual || caps.Gamepad || caps.ExternalPico || c.s.Profile.CivSwitch {
		return c.configurationError(KeyboardUnavailable, "Live keyboard forwarding is unavailable: select built-in keyboard HID with a supervised keyboard adapter.")
	}
	queue, stop := make(chan [8]byte, 64), make(chan struct{})
	c.keyboard, c.keyboardStop = queue, stop
	c.s.KeyboardOpen = false
	return c.begin(Inputting, true, func(ctx context.Context, run Run) (err error) {
		var keyboard Keyboard
		defer func() {
			if keyboard != nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				err = errors.Join(err, keyboard.Close(cleanup))
				cancel()
			}
			c.mu.Lock()
			c.keyboard, c.keyboardStop = nil, nil
			c.s.KeyboardOpen = false
			c.mu.Unlock()
		}()
		if err := c.inspectKeyboard(ctx, run, input); err != nil {
			return err
		}
		keyboard, err = input.OpenKeyboardSession(ctx, run, c.report)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.s.KeyboardOpen = true
		c.emit(Info, "", "keyboard_ready")
		c.mu.Unlock()
		count := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-stop:
				return nil
			case report := <-queue:
				if err := keyboard.Send(ctx, report); err != nil {
					return err
				}
				count++
				c.report(Progress{Current: count, SideEffect: true})
			}
		}
	}), nil
}

func (c *SessionController) ForwardKeyboardReport(report [8]byte) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keyboard == nil || !c.s.KeyboardOpen || c.requested != "" {
		return c.reject(InvalidState)
	}
	if !ValidKeyboardReport(report) {
		c.closeKeyboard()
		return c.reject(InvalidConfiguration)
	}
	select {
	case c.keyboard <- report:
		// Mark before publication to the writer, not after an acknowledgement.
		c.s.SideEffects = true
		return c.snapshot(), nil
	default:
		c.closeKeyboard()
		return c.reject(AdapterFailure)
	}
}

// keyboardCloseBound exceeds the transport's own neutral-release and exit
// bound, so cancellation only interrupts a writer that cannot finish.
const keyboardCloseBound = 5 * time.Second

// Called with mu held. The writer stops after its current report and closes
// the lease orderly: acknowledged neutral report, EOF, then exit.
func (c *SessionController) closeKeyboard() {
	c.requested = Ready
	if c.s.SideEffects {
		c.requested = Indeterminate
	}
	close(c.keyboardStop)
	time.AfterFunc(keyboardCloseBound, c.cancel)
}

func (c *SessionController) CloseKeyboard() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keyboard == nil || c.requested != "" {
		return c.reject(InvalidState)
	}
	c.closeKeyboard()
	c.emit(Info, "", "Keyboard closing; waiting for neutral release.")
	return c.snapshot(), nil
}

// ValidKeyboardReport matches the native helper's packed boot-keyboard format.
func ValidKeyboardReport(report [8]byte) bool {
	if report[1] != 0 {
		return false
	}
	empty := false
	for i, key := range report[2:] {
		if key == 0 {
			empty = true
			continue
		}
		if empty || key < 4 || key > 0x73 {
			return false
		}
		for _, previous := range report[2 : 2+i] {
			if previous == key {
				return false
			}
		}
	}
	return true
}

package transport

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/thedavidweng/any2jb/session"
)

// Keyboard owns one supervised helper. FocusLost and Close release neutral and
// wait for teardown; a closed keyboard can never resume without a fresh open.
type Keyboard struct{ p *helperProcess }

func (d *Darwin) OpenKeyboard(ctx context.Context, run session.Run, report session.Report) (*Keyboard, error) {
	p, err := d.start(ctx, run)
	if err != nil {
		return nil, err
	}
	line, err := p.next(ctx)
	if err != nil || line != "MAC2JB_KEYBOARD_READY" {
		cleanup, cancel := context.WithTimeout(context.Background(), 3e9)
		defer cancel()
		return nil, errors.Join(errors.New("USB keyboard did not enumerate"), err, p.close(cleanup))
	}
	controller, _ := d.settings()
	d.mu.Lock()
	d.enumerated = controller
	d.mu.Unlock()
	report(session.Progress{KeyboardController: controller, Processes: []session.Process{{PID: p.cmd.Process.Pid, Name: "keyboard"}}})
	return &Keyboard{p}, nil
}

func (d *Darwin) OpenKeyboardSession(ctx context.Context, run session.Run, report session.Report) (session.Keyboard, error) {
	return d.OpenKeyboard(ctx, run, report)
}

func (d *Darwin) SelectedKeyboardController() string {
	controller, _ := d.settings()
	return controller
}

func (p *helperProcess) write(ctx context.Context, report [8]byte) error {
	if !session.ValidKeyboardReport(report) {
		return errors.New("invalid packed 8-byte keyboard report")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := p.input.Write(report[:]); err != nil {
		return errors.New("USB keyboard write failed")
	}
	line, err := p.next(ctx)
	if err != nil {
		return err
	}
	if line != "MAC2JB_KEYBOARD_SENT "+strconv.Itoa(p.count+1) {
		return fmt.Errorf("invalid keyboard acknowledgement")
	}
	p.count++
	return nil
}

func (k *Keyboard) Send(ctx context.Context, report [8]byte) error {
	p := k.p
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("keyboard is closed")
	}
	err := p.write(ctx, report)
	p.mu.Unlock()
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 3e9)
		defer cancel()
		return errors.Join(err, p.close(cleanup))
	}
	return nil
}
func (k *Keyboard) FocusLost(ctx context.Context) error { return k.Close(ctx) }
func (k *Keyboard) Close(ctx context.Context) error     { return k.p.close(ctx) }

// Paste sends bounded SmileBASIC text without adding a trailing Enter.
func (k *Keyboard) Paste(ctx context.Context, text string) error {
	if len(text) == 0 || len(text) > 16*1024 {
		return errors.New("paste requires 1..16384 bytes")
	}
	return k.p.owner.typeText(ctx, k, text)
}

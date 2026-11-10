package transport

import (
	"context"
	"errors"

	"github.com/thedavidweng/any2jb/session"
)

// Windows reserves Phase 3 external Pico input; ordinary USB-C is not a gadget.
type Windows struct{}

func NewWindows() *Windows { return &Windows{} }

func (*Windows) Send(context.Context, session.Run, session.InputAction, session.PreparedHost, session.Report) error {
	return errors.New("Windows external Pico HID requires Phase 3 integration and compatible hardware; use Guided manual launch")
}
func (*Windows) Release(context.Context, session.Run) error { return nil }

var _ InputTransport = (*Windows)(nil)

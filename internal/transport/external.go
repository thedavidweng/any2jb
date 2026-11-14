package transport

import (
	"context"
	"errors"

	"github.com/thedavidweng/any2jb/internal/session"
)

// Windows has no USB device-mode input; ordinary USB-C is not a gadget.
type Windows struct{}

func NewWindows() *Windows { return &Windows{} }

func (*Windows) Send(context.Context, session.Run, session.InputAction, session.PreparedHost, session.Report) error {
	return errors.New("USB input is unsupported on Windows; use Guided manual launch")
}
func (*Windows) Release(context.Context, session.Run) error { return nil }

var _ InputTransport = (*Windows)(nil)

package transport

import (
	"context"
	"errors"

	"github.com/thedavidweng/any2jb/session"
)

// Darwin reserves the supervised libusbd helper boundary for ticket 12.
type Darwin struct{}

func NewDarwin() *Darwin { return &Darwin{} }

func (*Darwin) Send(context.Context, session.Run, session.InputAction, session.PreparedHost, session.Report) error {
	return errors.New("Darwin libusbd input is unavailable until verified native helpers are integrated; use Guided manual launch")
}
func (*Darwin) Release(context.Context, session.Run) error { return nil }

var _ InputTransport = (*Darwin)(nil)

package transport

import (
	"context"
	"errors"

	"github.com/thedavidweng/any2jb/session"
)

// Linux is the Phase 2 ConfigFS boundary, not USB host-mode emulation.
type Linux struct{}

func NewLinux() *Linux { return &Linux{} }

func (*Linux) Send(context.Context, session.Run, session.InputAction, session.PreparedHost, session.Report) error {
	return errors.New("Linux ConfigFS HID requires Phase 2 integration and real UDC-capable hardware; use Guided manual launch")
}
func (*Linux) Release(context.Context, session.Run) error { return nil }

var _ InputTransport = (*Linux)(nil)

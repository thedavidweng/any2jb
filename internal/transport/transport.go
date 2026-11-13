// Package transport is the platform input boundary. Stubs are deliberately
// buildable on every target; compiling a backend does not qualify its hardware.
package transport

import (
	"context"

	"github.com/thedavidweng/any2jb/internal/session"
)

// InputTransport uses the controller's single authoritative input contract.
type InputTransport = session.InputTransport

// Mock injects behavior only at the public transport boundary.
type Mock struct {
	SendFunc    func(context.Context, session.Run, session.InputAction, session.PreparedHost, session.Report) error
	ReleaseFunc func(context.Context, session.Run) error
}

func (m Mock) Send(ctx context.Context, run session.Run, action session.InputAction, host session.PreparedHost, report session.Report) error {
	return m.SendFunc(ctx, run, action, host, report)
}
func (m Mock) Release(ctx context.Context, run session.Run) error { return m.ReleaseFunc(ctx, run) }

var (
	_ InputTransport = (*Manual)(nil)
	_ InputTransport = Mock{}
)

package session

import "context"

// MarkInterrupted records an unadopted workspace/lease found on app relaunch.
// It never reads stale verification or pairing artifacts.
func (c *SessionController) MarkInterrupted() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Idle) {
		return c.reject(InvalidState)
	}
	c.s.Outcome = Outcome{}
	c.s.Recovery = RestartConsole
	c.transition(Indeterminate)
	return c.snapshot(), nil
}

// ReleaseInterrupted is called only after the user confirms a console restart.
// It leaves the transition out of Indeterminate to the confirmation command.
func (c *SessionController) ReleaseInterrupted(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Indeterminate) {
		return &Rejection{Reason: InvalidState, Stage: c.s.Stage}
	}
	return c.env.Platform.ReleaseInterrupted(ctx)
}

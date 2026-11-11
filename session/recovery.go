package session

import (
	"context"
	"time"
)

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
	if !c.eligible(Indeterminate) {
		err := &Rejection{Reason: InvalidState, Stage: c.s.Stage}
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	return c.env.Platform.ReleaseInterrupted(ctx)
}

// ConfirmConsoleRestart is the shared SmileBASIC/CivSwitch recovery command.
// Call off the UI thread: releasing a stale lease can perform filesystem I/O.
// It never starts input or reuses readiness and verification from the old run.
func (c *SessionController) ConfirmConsoleRestart(ctx context.Context, confirmed bool) (Snapshot, error) {
	c.mu.Lock()
	if !confirmed || !c.eligible(Indeterminate) {
		s, err := c.reject(InvalidState)
		c.mu.Unlock()
		return s, err
	}
	c.mu.Unlock()
	if err := c.ReleaseInterrupted(ctx); err != nil {
		return c.Snapshot(), err
	}
	c.mu.Lock()
	if !c.eligible(Indeterminate) {
		s, err := c.reject(InvalidState)
		c.mu.Unlock()
		return s, err
	}
	if err := ctx.Err(); err != nil {
		s := c.snapshot()
		c.mu.Unlock()
		return s, err
	}
	run, cleanupNeeded := c.run(), !c.s.CleanupConfirmed && c.s.RunID != ""
	c.s.Busy = true
	c.emit(Info, "", "Console restart confirmed; checking resource release.")
	c.mu.Unlock()
	var err error
	if cleanupNeeded {
		err = c.cleanup(ctx, run)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s.Busy = false
	if err != nil {
		c.emit(Error, AdapterFailure, "Resource cleanup could not be confirmed.")
		return c.snapshot(), err
	}
	c.s.RunID, c.s.SideEffects, c.s.LoaderVerified = "", false, false
	c.s.BootstrapPrepared, c.s.SaveSubmitted = false, false
	c.s.Current, c.s.Total, c.s.EstimatedDuration = 0, 0, 0
	c.s.Remaining, c.s.HasEstimate, c.s.KeyboardOpen = 0, false, false
	c.s.Outcome, c.s.Checks, c.s.Processes = Outcome{}, nil, nil
	c.s.ReleaseUnconfirmed = false
	c.s.Recovery = NoRecovery
	c.started = time.Time{}
	c.transition(Idle)
	return c.snapshot(), nil
}

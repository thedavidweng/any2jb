package session

import (
	"context"
	"errors"
)

// CivSwitchWriter is the gated persistence, Control Center and arbitrary-code
// surface of the CivSwitch route. It is separate from the route adapter: a
// loader verified earlier never authorizes these actions by itself, and each
// one re-probes the loader and carries the user's explicit confirmation.
type CivSwitchWriter interface {
	InstallBootTrigger(ctx context.Context, address string, confirmedSaveReplacement bool, report Report) (string, error)
	SendJavaScript(ctx context.Context, address, file string, confirmedArbitraryCode bool, report Report) error
	StageJavaScript(ctx context.Context, address, file string, confirmedArbitraryCode bool, report Report) error
	ControlCenter(ctx context.Context, address string, observe CivSwitchObserver) error
}

// CivSwitchObservation carries fixed Control Center facts and retained export
// paths. Console and loader text is never copied into it.
type CivSwitchObservation struct {
	Ready      bool
	ExportPath string
}

type CivSwitchObserver func(CivSwitchObservation)

// CivSwitchState records what the gated actions established. Its paths are
// retained outside the session workspace and survive cleanup.
type CivSwitchState struct {
	ControlCenterLive bool
	TriggerInstalled  bool
	BackupPath        string
	ExportPath        string
}

// civSwitchDependent gates every action that depends on the loader: the route
// must be the verified CivSwitch one, with no other operation running.
func (c *SessionController) civSwitchDependent(fn func(context.Context, CivSwitchWriter, string) error) (Snapshot, error) {
	if !c.s.CivSwitch || !c.s.LoaderVerified || !c.eligible(Verified) {
		return c.reject(InvalidState)
	}
	if reason := c.civSwitchUnavailable(); reason != "" {
		return c.configurationError(Unsupported, reason)
	}
	writer, ok := c.env.CivSwitch.(CivSwitchWriter)
	if !ok {
		return c.configurationError(Unsupported, "CivSwitch persistence and Control Center actions are not staged.")
	}
	address := c.s.ConsoleIPv4
	return c.begin(Verified, false, func(ctx context.Context, _ Run) error {
		return fn(ctx, writer, address)
	}), nil
}

// InstallCivSwitchTrigger writes the persistent boot trigger over the game's
// single-player saves. The adapter backs those saves up and verifies the backup
// first; this confirmation is the user's acceptance of the replacement.
func (c *SessionController) InstallCivSwitchTrigger(confirmedSaveReplacement bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !confirmedSaveReplacement {
		return c.configurationError(InvalidConfiguration, "Installing the CivSwitch boot trigger replaces the game's single-player save files. Confirm the replacement; a verified backup is taken first.")
	}
	return c.civSwitchDependent(func(ctx context.Context, w CivSwitchWriter, address string) error {
		backup, err := w.InstallBootTrigger(ctx, address, true, c.report)
		c.mu.Lock()
		defer c.mu.Unlock()
		if backup != "" {
			c.s.CivSwitchActions.BackupPath = backup
		}
		if err != nil {
			return err
		}
		c.s.CivSwitchActions.TriggerInstalled = true
		return nil
	})
}

// SendCivSwitchJavaScript and StageCivSwitchJavaScript are arbitrary code
// execution in the loader's context, which no prerequisite confirmation covers.
func (c *SessionController) SendCivSwitchJavaScript(file string, confirmedArbitraryCode bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.arbitraryCode(file, confirmedArbitraryCode); err != nil {
		return c.configurationError(InvalidConfiguration, err.Error())
	}
	return c.civSwitchDependent(func(ctx context.Context, w CivSwitchWriter, address string) error {
		return w.SendJavaScript(ctx, address, file, true, c.report)
	})
}

func (c *SessionController) StageCivSwitchJavaScript(file string, confirmedArbitraryCode bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.arbitraryCode(file, confirmedArbitraryCode); err != nil {
		return c.configurationError(InvalidConfiguration, err.Error())
	}
	return c.civSwitchDependent(func(ctx context.Context, w CivSwitchWriter, address string) error {
		return w.StageJavaScript(ctx, address, file, true, c.report)
	})
}

func (c *SessionController) arbitraryCode(file string, confirmed bool) error {
	if !confirmed {
		return errors.New("Sending or staging a JavaScript file executes arbitrary code on the console. Confirm that you reviewed and trust this file.")
	}
	if file == "" {
		return errors.New("Choose the JavaScript file to run in the CivSwitch loader.")
	}
	return nil
}

// StartCivSwitchControlCenter opens the read-only Control Center session. The
// console drives its tools; the host only receives verified save exports.
func (c *SessionController) StartCivSwitchControlCenter() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.civSwitchDependent(func(ctx context.Context, w CivSwitchWriter, address string) error {
		c.mu.Lock()
		c.s.CivSwitchActions.ControlCenterLive = true
		c.mu.Unlock()
		err := w.ControlCenter(ctx, address, c.observeCivSwitch)
		c.mu.Lock()
		c.s.CivSwitchActions.ControlCenterLive = false
		c.mu.Unlock()
		if errors.Is(err, context.Canceled) {
			// A deliberate stop closes the host connection. The loader and the
			// Control Center keep running on the console.
			return nil
		}
		return err
	})
}

// StopCivSwitchControlCenter releases the local connection only.
func (c *SessionController) StopCivSwitchControlCenter() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.s.CivSwitchActions.ControlCenterLive || !c.s.Busy || c.s.SideEffects || c.requested != "" {
		return c.reject(InvalidState)
	}
	c.cancel()
	c.emit(Info, "", "Control Center stop requested; the console keeps its loader.")
	return c.snapshot(), nil
}

func (c *SessionController) observeCivSwitch(o CivSwitchObservation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if o.ExportPath != "" {
		c.s.CivSwitchActions.ExportPath = o.ExportPath
	}
	c.emit(Info, "", "civswitch-control-center")
}

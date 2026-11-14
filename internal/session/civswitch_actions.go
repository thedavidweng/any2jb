package session

import (
	"context"
	"errors"
)

// CivSwitchWriter is the gated persistence and arbitrary-code surface of the
// CivSwitch route. It is separate from the route adapter: a loader verified
// earlier never authorizes these actions by itself, and each one re-probes the
// loader and carries the user's explicit confirmation.
type CivSwitchWriter interface {
	InstallBootTrigger(ctx context.Context, address string, confirmedSaveReplacement bool, report Report) (string, error)
	SendJavaScript(ctx context.Context, address, file string, confirmedArbitraryCode bool, report Report) error
}

// CivSwitchState records what the gated actions established. Its paths are
// retained outside the session workspace and survive cleanup.
type CivSwitchState struct {
	FSHostLive            bool
	FSRunPath             string
	CivHomebrew           CivHomebrewState
	CivHomebrewLive       bool
	CivHomebrewBackupPath string
	CivHomebrewPackage    CivHomebrewApp
	TriggerInstalled      bool
	BackupPath            string
}

// civSwitchDependent gates every action that depends on the loader: the route
// must be the verified CivSwitch one, with no other operation running.
func (c *SessionController) civSwitchDependent(fn func(context.Context, CivSwitchWriter, string) error) (Snapshot, error) {
	if !c.s.CivSwitch || !c.s.LoaderVerified || !c.eligible(Verified) {
		return c.reject(InvalidState)
	}
	if err := c.civSwitchAvailability(); err != nil {
		return c.configurationCause(Unsupported, err)
	}
	writer := c.env.CivSwitch.Writer()
	if writer == nil {
		return c.configurationError(Unsupported, "CivSwitch persistence and JavaScript actions are not staged.")
	}
	return c.civSwitchRun(func(ctx context.Context, address string) error {
		return fn(ctx, writer, address)
	}), nil
}

// civSwitchRun starts one loader-dependent action on the verified console. The
// caller has already passed the route gates.
func (c *SessionController) civSwitchRun(fn func(context.Context, string) error) Snapshot {
	address := c.s.ConsoleIPv4
	return c.begin(Verified, false, func(ctx context.Context, _ Run) error {
		return fn(ctx, address)
	})
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

// SendCivSwitchJavaScript is arbitrary code execution in the loader's context,
// which no prerequisite confirmation covers.
func (c *SessionController) SendCivSwitchJavaScript(file string, confirmedArbitraryCode bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.arbitraryCode(file, confirmedArbitraryCode); err != nil {
		return c.configurationCause(InvalidConfiguration, err)
	}
	return c.civSwitchDependent(func(ctx context.Context, w CivSwitchWriter, address string) error {
		return w.SendJavaScript(ctx, address, file, true, c.report)
	})
}

func (c *SessionController) arbitraryCode(file string, confirmed bool) error {
	if !confirmed {
		return errors.New("Sending a JavaScript file executes arbitrary code on the console. Confirm that you reviewed and trust this file.")
	}
	if file == "" {
		return errors.New("Choose the JavaScript file to run in the CivSwitch loader.")
	}
	return nil
}

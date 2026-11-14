package session

import (
	"context"
	"errors"
	"slices"
)

// RetainedSession is a discovered local verified held session. Listing one is
// not proof that it can still be used; Attach revalidates its live evidence.
type RetainedSession struct{ ID, Target, Description string }

// RetainedAdapter owns discovery of and attachment to an existing verified held
// session on this machine. Attach must revalidate the live lease, and Detach
// must leave the original host running. Watch reports a lost lease; neither
// Attach nor Watch may reconnect after a loss.
type RetainedAdapter interface {
	Discover(context.Context) ([]RetainedSession, error)
	Attach(context.Context, Run, string) (Outcome, error)
	Detach(context.Context, Run) error
	Watch(context.Context, Run) error
}

// DiscoverRetained is read-only: it neither claims nor changes a lease.
func (c *SessionController) DiscoverRetained(ctx context.Context) ([]RetainedSession, error) {
	c.mu.Lock()
	adapter := c.env.Retained
	c.mu.Unlock()
	if adapter == nil {
		return nil, errors.New("retained-session discovery is unavailable in this build")
	}
	return adapter.Discover(ctx)
}

// AttachRetained connects to an existing verified session instead of launching
// a new one. It never prepares a host or sends console input.
func (c *SessionController) AttachRetained(id string) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.s.CivSwitch || !c.eligible(Idle, NeedsSetup, Ready, Finished, Failed) {
		return c.reject(InvalidState)
	}
	if !c.s.Profile.RetainsRPC {
		return c.configurationError(Unsupported, "This host does not retain an RPC session. Choose a retained-RPC host to attach.")
	}
	if c.env.Retained == nil {
		return c.configurationError(Unsupported, "Attaching to an existing session is unavailable in this build.")
	}
	if id == "" {
		return c.reject(InvalidConfiguration)
	}
	return c.begin(Verifying, true, func(ctx context.Context, run Run) error {
		outcome, err := c.env.Retained.Attach(ctx, run, id)
		if err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		c.s.Outcome = outcome
		if !outcome.valid(true) {
			c.emit(Error, VerificationIncomplete, "The attached session did not prove a live retained lease.")
			return errors.New("incomplete retained evidence")
		}
		// From here the lease belongs to a live console session, so losing it
		// leaves console state unknown.
		c.s.SideEffects = true
		c.s.Attached = true
		c.transition(RetainedRPC)
		return nil
	}), nil
}

// DetachRetained releases this app's attachment only. The original host keeps
// running and only its owner can finish it.
func (c *SessionController) DetachRetained() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.s.Attached || !c.eligible(RetainedRPC) {
		return c.reject(InvalidState)
	}
	if c.monitorCancel != nil {
		c.monitorCancel()
		c.monitorCancel = nil
	}
	return c.begin(RetainedRPC, false, func(ctx context.Context, run Run) error {
		if err := errors.Join(c.env.Tools.Stop(ctx, run), c.env.Retained.Detach(ctx, run)); err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.s.Attached, c.s.SideEffects = false, false
		c.s.Outcome = Outcome{}
		c.s.Checks = nil
		c.emit(Info, "", "Disconnected. The original host is still running on the console.")
		c.transition(Idle)
		return nil
	}), nil
}

// detach is the cleanup path of an attached session: it stops owned tool work
// without touching the host another app owns.
func (c *SessionController) detach(ctx context.Context, run Run) error {
	err := errors.Join(c.env.Tools.Stop(ctx, run), c.env.Retained.Detach(ctx, run))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s.Attached = false
	c.s.Processes = nil
	if err == nil {
		c.s.CleanupConfirmed = true
	}
	return err
}

// ToolSaveGate validates a save-data operation across the session boundary
// without coupling the session state machine to save-archive formats.
type ToolSaveGate interface {
	ValidateToolSave(tool string) error
}

// ToolRequest carries user input for one retained-session tool. A parameter is
// neither a capability nor consent; both the session controller and the tool
// runner validate it before any transfer or console change.
type ToolRequest struct {
	Tool                 string
	ConsoleIPv4          string
	Save                 ToolSaveGate
	Modules              []string
	SDRomFolder          string
	AppFolder            string
	ModsFolder           string
	CustomHomebrewFolder string
	StremioQuality       string
	StremioAddonURLs     []string
	CodeConsent          bool
}

// ValidateCodeConsent is shared by the controller and retained runner so
// bypassing the view cannot authorize user-supplied code.
func (r ToolRequest) ValidateCodeConsent() error {
	if r.Tool == "modloader" && !r.CodeConsent {
		return errors.New("Confirm running mods on the console before installing them.")
	}
	if (r.Tool == "custom-homebrew" || (r.Tool == "native-hblauncher" || r.Tool == "mintbrew") && r.AppFolder != "") && !r.CodeConsent {
		return errors.New("Confirm running user-provided homebrew code on the console before launching it.")
	}
	return nil
}

// Valid reports whether the complete verification contract is met, including
// the retained-lease predicates for a held session.
func (o Outcome) Valid(held bool) bool { return o.valid(held) }

// RunToolWith starts a tool the verified profile enables, with validated input.
func (c *SessionController) RunToolWith(request ToolRequest) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(RetainedRPC) {
		return c.reject(InvalidState)
	}
	if !slices.Contains(c.s.Profile.Tools, request.Tool) {
		return c.reject(Unsupported)
	}
	if c.env.Tools == nil {
		return c.reject(Unsupported)
	}
	if request.ConsoleIPv4 != "" && !validConsoleIPv4(request.ConsoleIPv4) {
		return c.configurationError(InvalidConfiguration, "Enter the console's IPv4 address on the same reachable LAN.")
	}
	if request.Save != nil {
		if err := request.Save.ValidateToolSave(request.Tool); err != nil {
			return c.configurationCause(InvalidConfiguration, err)
		}
	}
	if err := request.ValidateCodeConsent(); err != nil {
		return c.configurationCause(InvalidConfiguration, err)
	}
	if err := c.env.Tools.Validate(request); err != nil {
		return c.configurationCause(InvalidConfiguration, err)
	}
	return c.begin(RunningTool, false, func(ctx context.Context, run Run) error {
		if err := c.env.Tools.RunWith(ctx, run, request, c.report); err != nil {
			return err
		}
		return c.stage(ctx, RetainedRPC, true)
	}), nil
}

// StopTool ends the running tool's own work. The retained session stays.
func (c *SessionController) StopTool() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(RetainedRPC) {
		return c.reject(InvalidState)
	}
	return c.begin(RunningTool, false, func(ctx context.Context, run Run) error {
		if err := c.env.Tools.Stop(ctx, run); err != nil {
			return err
		}
		return c.stage(ctx, RetainedRPC, true)
	}), nil
}

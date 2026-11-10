package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/thedavidweng/any2jb/savekit"
)

// RetainedSession is a discovered local verified held session. Listing one is
// not proof that it can still be used; Attach revalidates its live evidence.
type RetainedSession struct{ ID, Description string }

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

// ToolRequest carries validated user input for one tool: the console address a
// read-only export needs, one read-only SaveKit operation or the homebrew
// launch configuration. A parameter is
// neither a capability nor consent.
type ToolRequest struct {
	Tool        string
	ConsoleIPv4 string
	SaveKit     *savekit.Request
	// Modules selects IZlauncher module ids from the staged catalog, SDRomFolder
	// is the native launcher's read-only console ROM folder, and AppFolder a
	// Mac folder of previously built native apps. The tool host re-validates
	// each against staged, digest-pinned sources.
	Modules     []string
	SDRomFolder string
	AppFolder   string
	// ModsFolder is the modloader's Mac mod tree and CustomHomebrewFolder a
	// built custom homebrew app; both are layout-checked before any transfer.
	ModsFolder           string
	CustomHomebrewFolder string
}

// validateHomebrew checks the optional homebrew launch parameters. They cross
// a trust boundary, so the tool host rechecks every one of them.
func (r ToolRequest) validateHomebrew() error {
	if len(r.Modules) > 0 && r.Tool != "izlauncher" {
		return errors.New("a module selection belongs to the IZlauncher")
	}
	if r.SDRomFolder != "" && r.Tool != "native-hblauncher" {
		return errors.New("an SD ROM folder belongs to the native launcher")
	}
	if r.AppFolder != "" && r.Tool != "native-hblauncher" {
		return errors.New("a native app folder belongs to the native launcher")
	}
	if (r.ModsFolder != "") != (r.Tool == "modloader") {
		return errors.New("the modloader needs exactly one mod folder")
	}
	if (r.CustomHomebrewFolder != "") != (r.Tool == "custom-homebrew") {
		return errors.New("custom homebrew needs exactly one built app folder")
	}
	if r.ModsFolder != "" {
		if err := validateModsFolder(r.ModsFolder); err != nil {
			return err
		}
	}
	if r.CustomHomebrewFolder != "" {
		if err := validateCustomHomebrew(r.CustomHomebrewFolder); err != nil {
			return err
		}
	}
	if len(r.Modules) > 15 {
		return errors.New("choose up to 15 distinct compatible modules")
	}
	seen := map[string]bool{}
	for _, id := range r.Modules {
		if seen[id] || !validModuleID(id) {
			return errors.New("choose up to 15 distinct compatible modules")
		}
		seen[id] = true
	}
	if r.SDRomFolder != "" && !validSDRomFolder(r.SDRomFolder) {
		return errors.New("enter an absolute SD folder under /sd without a trailing slash")
	}
	if r.AppFolder != "" && !validAppFolder(r.AppFolder) {
		return errors.New("choose an existing absolute folder on this Mac")
	}
	return nil
}

func (r ToolRequest) needsParameterized() bool {
	return r.ConsoleIPv4 != "" || r.SaveKit != nil || len(r.Modules) > 0 || r.SDRomFolder != "" || r.AppFolder != "" ||
		r.ModsFolder != "" || r.CustomHomebrewFolder != ""
}

// validModuleID mirrors the staged IZlauncher catalog's id shape.
func validModuleID(id string) bool {
	if len(id) < 1 || len(id) > 48 {
		return false
	}
	for i, c := range id {
		switch {
		case c >= 'a' && c <= 'z' || c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// validSDRomFolder mirrors the read-only SD export's folder rules: absolute
// under /sd, no trailing slash, no dot segments or control characters.
func validSDRomFolder(value string) bool {
	if len(value) > 240 || !strings.HasPrefix(value, "/sd/") || strings.HasSuffix(value, "/") {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return !strings.ContainsAny(value, "\x00\r\n")
}

func validAppFolder(value string) bool {
	if !filepath.IsAbs(value) || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	info, err := os.Stat(value)
	return err == nil && info.IsDir()
}

// ParameterizedToolRunner is implemented by tool adapters that accept validated
// user input. Adapters without it only receive the tool identity.
type ParameterizedToolRunner interface {
	RunWith(context.Context, Run, ToolRequest, Report) error
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
	if request.ConsoleIPv4 != "" && !validConsoleIPv4(request.ConsoleIPv4) {
		return c.configurationError(InvalidConfiguration, "Enter the console's IPv4 address on the same reachable LAN.")
	}
	if request.SaveKit != nil {
		if err := request.SaveKit.Validate(); err != nil {
			return c.configurationError(InvalidConfiguration, err.Error())
		}
	}
	if err := request.validateHomebrew(); err != nil {
		return c.configurationError(InvalidConfiguration, err.Error())
	}
	runner, parameterized := c.env.Tools.(ParameterizedToolRunner)
	if request.needsParameterized() && !parameterized {
		return c.configurationError(Unsupported, "This build's tool adapter does not accept tool parameters.")
	}
	return c.begin(RunningTool, false, func(ctx context.Context, run Run) error {
		var err error
		if parameterized {
			err = runner.RunWith(ctx, run, request, c.report)
		} else {
			err = c.env.Tools.Run(ctx, run, request.Tool, c.report)
		}
		if err != nil {
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

package session

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

// CivSwitchAdapter owns a composite keyboard/gamepad writer and an independent
// loader probe. It never supplies an ns2jb Outcome or a retained RPC lease.
type CivSwitchAdapter interface {
	Prepare(context.Context) (time.Duration, error)
	Transfer(context.Context, Report) error
	Probe(context.Context, string, bool) error
	Release(context.Context) error
}

// These are explicit console-side confirmations, not facts inferred from the OS.
type CivSwitchPrerequisites struct {
	PhysicalSwitch1Cartridge100 bool
	EnglishUS                   bool
	SameReachableNetwork        bool
	LobbyChatFocused            bool
	ConsoleIPv4                 string
}

// SetInvalidator installs a nonblocking redraw request. The callback must not
// call back into the controller; views pass their persistent Services.Invalidate.
func (c *SessionController) SetInvalidator(invalidate func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidate = invalidate
}

// CivSwitchUnavailable is shown even when no adapter was composed.
func (c *SessionController) CivSwitchUnavailable() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.civSwitchUnavailable()
}

func (c *SessionController) civSwitchUnavailable() string {
	if c.env.CivSwitch == nil {
		return "CivSwitch components are not staged; this path is unavailable."
	}
	if gate, ok := c.env.CivSwitch.(interface{ Availability() error }); ok {
		if err := gate.Availability(); err != nil {
			return err.Error()
		}
	}
	return ""
}

func validConsoleIPv4(address string) bool {
	ip, err := netip.ParseAddr(address)
	return err == nil && ip.Is4() && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() && !ip.IsLoopback()
}

func (c *SessionController) StartCivSwitch(p CivSwitchPrerequisites) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Idle, NeedsSetup, Ready, Finished, Failed) {
		return c.reject(InvalidState)
	}
	if !p.PhysicalSwitch1Cartridge100 || !p.EnglishUS || !p.SameReachableNetwork ||
		!p.LobbyChatFocused || !validConsoleIPv4(p.ConsoleIPv4) {
		return c.configurationError(InvalidConfiguration, "Confirm physical Switch 1 Civilization VII cartridge 1.0.0, English (US), the same reachable LAN, and focused lobby chat; enter the console IPv4.")
	}
	if !c.s.Transport.Capabilities.Keyboard || !c.s.Transport.Capabilities.Gamepad {
		return c.configurationError(Unsupported, "CivSwitch requires composite keyboard + gamepad HID; keyboard-only input is not equivalent.")
	}
	if reason := c.civSwitchUnavailable(); reason != "" {
		return c.configurationError(Unsupported, reason)
	}
	// Local readiness facts (including the driver gate) must be checked before
	// starting a writer. The separate path does not consume SmileBASIC checks.
	c.s.CivSwitch, c.s.LoaderVerified, c.s.ConsoleIPv4 = true, false, p.ConsoleIPv4
	return c.begin(CivSwitchFetching, true, func(ctx context.Context, run Run) error {
		estimate, err := c.env.CivSwitch.Prepare(ctx)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.s.EstimatedDuration = estimate
		c.mu.Unlock()
		if err := c.stage(ctx, Inputting, true); err != nil {
			return err
		}
		if err := c.env.CivSwitch.Transfer(ctx, c.report); err != nil {
			return err
		}
		if err := c.stage(ctx, Verifying, true); err != nil {
			return err
		}
		return c.probeCivSwitch(ctx, p.ConsoleIPv4, false)
	}), nil
}

// ConnectCivSwitch probes an already-open loader without staging or typing.
func (c *SessionController) ConnectCivSwitch(address string) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Idle, NeedsSetup, Ready, Finished, Failed) {
		return c.reject(InvalidState)
	}
	if !validConsoleIPv4(address) {
		return c.configurationError(InvalidConfiguration, "Enter the CivSwitch console IPv4 on the same reachable LAN.")
	}
	if reason := c.civSwitchUnavailable(); reason != "" {
		return c.configurationError(Unsupported, reason)
	}
	c.s.CivSwitch, c.s.LoaderVerified, c.s.ConsoleIPv4 = true, false, address
	c.s.EstimatedDuration = 0
	return c.begin(Verifying, true, func(ctx context.Context, _ Run) error {
		return c.probeCivSwitch(ctx, address, true)
	}), nil
}

// Recheck never replays the chat payload after partial console side effects.
func (c *SessionController) RecheckCivSwitch() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.s.CivSwitch || c.env.CivSwitch == nil || !c.eligible(Indeterminate, Failed, Verified) {
		return c.reject(InvalidState)
	}
	c.s.LoaderVerified = false
	address := c.s.ConsoleIPv4
	return c.begin(Verifying, false, func(ctx context.Context, _ Run) error {
		return c.probeCivSwitch(ctx, address, true)
	}), nil
}

func (c *SessionController) probeCivSwitch(ctx context.Context, address string, once bool) error {
	if err := c.env.CivSwitch.Probe(ctx, address, once); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.s.LoaderVerified = true
	c.transition(Verified)
	return nil
}

func (c *SessionController) releaseCivSwitch(ctx context.Context) error {
	if c.env.CivSwitch == nil {
		return errors.New("CivSwitch adapter unavailable")
	}
	return c.env.CivSwitch.Release(ctx)
}

// ConfirmCivSwitchRestart is explicit recovery after the writer has exited.
// It does not send input or start another attempt.
func (c *SessionController) ConfirmCivSwitchRestart(confirmed bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !confirmed || !c.s.CivSwitch || !c.eligible(Indeterminate) {
		return c.reject(InvalidState)
	}
	c.s.SideEffects, c.s.LoaderVerified = false, false
	c.s.Current, c.s.Total, c.s.EstimatedDuration = 0, 0, 0
	c.s.Recovery = NoRecovery
	c.transition(Idle)
	return c.snapshot(), nil
}

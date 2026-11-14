package session

import (
	"context"
	"crypto/rand"
	"errors"
	"net/netip"
	"slices"
	"sync"
	"time"
)

type SessionController struct {
	mu            sync.Mutex
	env           Environment
	s             Snapshot
	events        []Event
	started       time.Time
	cancel        context.CancelFunc
	requested     Stage
	monitorCancel context.CancelFunc
	keyboard      chan [8]byte
	keyboardStop  chan struct{}
	changes       chan struct{}
}

func New(env Environment, profile Profile) *SessionController {
	profile.Tools = slices.Clone(profile.Tools)
	profile.Editions = slices.Clone(profile.Editions)
	return &SessionController{env: env, changes: make(chan struct{}, 1), s: Snapshot{
		Stage: Idle, Profile: profile,
		Edition:   "full",
		Transport: Transport{Mode: "manual", Capabilities: Capabilities{Manual: true}},
	}}
}

func (c *SessionController) snapshot() Snapshot {
	s := c.s
	s.Checks = slices.Clone(s.Checks)
	s.Processes = slices.Clone(s.Processes)
	s.Profile.Tools = slices.Clone(s.Profile.Tools)
	s.Profile.Editions = slices.Clone(s.Profile.Editions)
	return s
}

func (c *SessionController) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot()
}

// Changes wakes the single window consumer. Read Snapshot after each wake;
// coalescing never loses state or the ordered Events history.
func (c *SessionController) Changes() <-chan struct{} { return c.changes }

// Events returns all observations after sequence, without dropping slow UI updates.
func (c *SessionController) Events(sequence uint64) []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := slices.IndexFunc(c.events, func(e Event) bool { return e.Sequence > sequence })
	if i < 0 {
		return nil
	}
	return slices.Clone(c.events[i:])
}

func (c *SessionController) emit(severity Severity, reason Reason, message string) {
	elapsed := time.Duration(0)
	if !c.started.IsZero() {
		elapsed = time.Since(c.started)
	}
	c.s.LastMessage = message
	c.events = append(c.events, Event{
		RunID: c.s.RunID, Sequence: uint64(len(c.events) + 1),
		Stage: c.s.Stage, Elapsed: elapsed, Severity: severity, Recovery: c.s.Recovery,
		Reason: reason, Message: message,
	})
	select {
	case c.changes <- struct{}{}:
	default:
	}
}

func (c *SessionController) reject(reason Reason) (Snapshot, error) {
	err := &Rejection{Reason: reason, Stage: c.s.Stage}
	c.emit(Warning, reason, err.Error())
	return c.snapshot(), err
}

func (c *SessionController) eligible(stages ...Stage) bool {
	return !c.s.Busy && slices.Contains(stages, c.s.Stage)
}

func (c *SessionController) run() Run {
	return Run{
		ID: c.s.RunID, Profile: c.snapshot().Profile, Edition: c.s.Edition,
		LANIPv4: c.s.LANIPv4, Transport: c.s.Transport,
	}
}

func (c *SessionController) transition(stage Stage) {
	c.s.Stage = stage
	c.emit(Info, "", string(stage))
}

func (c *SessionController) report(p Progress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s.SideEffects = c.s.SideEffects || p.SideEffect
	c.s.Current, c.s.Total = p.Current, p.Total
	c.s.Remaining, c.s.HasEstimate = p.Remaining, p.HasEstimate
	if p.KeyboardController != "" {
		c.s.Checks = ObserveKeyboardEnumeration(c.s.Checks, c.s.Transport.Controller, p.KeyboardController)
	}
	if p.Processes != nil {
		c.s.Processes = slices.Clone(p.Processes)
	}
	// Adapter display text may contain pairing secrets. Only typed progress enters
	// the public event stream; adapters own redacted diagnostics.
	c.emit(Info, "", "progress")
}

func (c *SessionController) stage(ctx context.Context, stage Stage, sideEffect bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.s.SideEffects = c.s.SideEffects || sideEffect
	c.transition(stage)
	return nil
}

// begin runs adapters off the UI thread. Cancellation waits for the operation to
// unwind before releasing input/children, so cleanup cannot race an active writer.
func (c *SessionController) begin(stage Stage, fresh bool, fn func(context.Context, Run) error) Snapshot {
	if fresh {
		c.s.RunID = rand.Text()
		c.started = time.Now()
		c.s.SideEffects = false
		c.s.Outcome = Outcome{}
		c.s.ReleaseUnconfirmed = false
		c.s.CleanupConfirmed = false
		c.s.Current, c.s.Total = 0, 0
		c.s.Remaining, c.s.HasEstimate = 0, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel, c.requested = cancel, ""
	c.s.Busy = true
	c.s.Recovery = NoRecovery
	c.transition(stage)
	run := c.run()
	go func() {
		defer cancel()
		err := fn(ctx, run)
		c.mu.Lock()
		requested := c.requested
		if err == nil && requested == "" {
			c.s.Busy = false
			c.cancel = nil
			if c.s.Stage == RetainedRPC && c.monitorCancel == nil {
				c.watchHost(run)
			}
			c.emit(Info, "", c.s.LastMessage)
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		if err != nil || requested != "" {
			timeout := 5 * time.Second
			if run.Profile.RetainsRPC {
				timeout = 305 * time.Second
			}
			cleanupCtx, done := context.WithTimeout(context.Background(), timeout)
			cleanupErr := c.cleanup(cleanupCtx, run)
			done()
			c.mu.Lock()
			requested = c.requested
			if c.s.SideEffects || cleanupErr != nil || requested == Indeterminate {
				c.s.Recovery = RestartConsole
				// A required console restart invalidates the old run's verification.
				c.s.Outcome = Outcome{}
				c.transition(Indeterminate)
			} else if requested == Ready {
				c.s.Recovery = RetrySafe
				c.transition(Ready)
			} else {
				c.s.Recovery = RetrySafe
				c.transition(Failed)
			}
			if err != nil && requested == "" {
				c.emit(Error, AdapterFailure, "Operation failed; see redacted diagnostics.")
			}
			if cleanupErr != nil {
				c.emit(Error, AdapterFailure, "Resource cleanup could not be confirmed.")
			}
			// A key may be stuck down on the console. The instruction is
			// emitted on cancel/stop paths too, after the redacted messages.
			if errors.Is(err, ErrReleaseUnconfirmed) || errors.Is(cleanupErr, ErrReleaseUnconfirmed) {
				c.s.ReleaseUnconfirmed = true
				c.emit(Error, AdapterFailure, ReleaseUnconfirmedWarning)
			}
			c.mu.Unlock()
		}
		c.mu.Lock()
		c.s.Busy = false
		c.cancel = nil
		c.emit(Info, "", c.s.LastMessage)
		c.mu.Unlock()
	}()
	return c.snapshot()
}

// Called with mu held. Finish disarms this watcher before asking the host to
// exit; any other exit invalidates the retained lease, including during a tool.
func (c *SessionController) watchHost(run Run) {
	ctx, cancel := context.WithCancel(context.Background())
	c.monitorCancel = cancel
	wait := c.env.Host.Wait
	if c.s.Attached {
		wait = c.env.Retained.Watch
	}
	go func() {
		_ = wait(ctx, run)
		c.mu.Lock()
		defer c.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		cancel()
		c.monitorCancel = nil
		if c.s.Busy {
			c.requested = Indeterminate
			c.cancel()
			return
		}
		c.begin(c.s.Stage, false, func(context.Context, Run) error {
			return errors.New("retained host exited unexpectedly")
		})
	}()
}

func (c *SessionController) cleanup(ctx context.Context, run Run) error {
	c.mu.Lock()
	civSwitch, attached := c.s.CivSwitch, c.s.Attached
	if c.monitorCancel != nil {
		c.monitorCancel()
		c.monitorCancel = nil
	}
	c.mu.Unlock()
	if civSwitch {
		return c.releaseCivSwitch(ctx)
	}
	if attached {
		return c.detach(ctx, run)
	}
	err := errors.Join(c.env.Input.Release(ctx, run), c.env.Tools.Stop(ctx, run), c.env.Host.Cleanup(ctx, run))
	if err == nil {
		c.mu.Lock()
		c.s.Processes = nil
		c.s.CleanupConfirmed = true
		c.mu.Unlock()
	}
	return err
}

func (c *SessionController) Inspect() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.s.CivSwitch || !c.eligible(Idle, Ready, NeedsSetup, Finished, Failed) {
		return c.reject(InvalidState)
	}
	override := c.s.LANOverride
	return c.begin(Inspecting, true, func(ctx context.Context, run Run) error {
		run.LANIPv4 = override
		checks, err := c.env.Platform.Inspect(ctx, run)
		if err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		c.s.Checks = slices.Clone(checks)
		c.s.LANIPv4 = override
		stage := Ready
		if len(checks) == 0 {
			stage = NeedsSetup
		}
		for _, check := range checks {
			if check.ID == "network" && check.State == CheckVerified && check.LANIPv4 != "" {
				c.s.LANIPv4 = check.LANIPv4
			}
			if check.Blocks(run.Transport) {
				stage = NeedsSetup
			}
		}
		if !run.Transport.Capabilities.supports(run.Profile.RequiredTransport) {
			stage = NeedsSetup
		}
		if stage == NeedsSetup {
			c.s.Recovery = CorrectSetup
		}
		c.transition(stage)
		return nil
	}), nil
}

func (c *SessionController) PrepareBootstrap() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.s.CivSwitch || !c.eligible(NeedsSetup, Ready) {
		return c.reject(InvalidState)
	}
	if !c.s.Transport.Capabilities.supports(c.s.Profile.RequiredTransport) {
		return c.reject(Unsupported)
	}
	c.s.BootstrapPrepared, c.s.BootstrapSaved = false, false
	c.s.SavedBootstrap = BootstrapRecord{}
	c.s.SaveSubmitted = false
	return c.begin(BootstrapSetup, true, func(ctx context.Context, run Run) error {
		if err := c.stage(ctx, BootstrapSetup, false); err != nil {
			return err
		}
		if run.Transport.Capabilities.Keyboard && !run.Transport.Capabilities.Manual {
			input, ok := c.env.Input.(KeyboardTransport)
			if !ok {
				return errors.New("USB bootstrap adapter is unavailable")
			}
			if err := c.inspectKeyboard(ctx, run, input); err != nil {
				return err
			}
		}
		if err := c.env.Input.Send(ctx, run, Bootstrap, PreparedHost{}, c.report); err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		c.s.BootstrapPrepared = true
		c.emit(Info, "", "Await explicit console save confirmation.")
		return nil
	}), nil
}

// SaveBootstrap(false) submits SAVE once. SaveBootstrap(true) then records the
// user's explicit confirmation that the console saved the bootstrap.
func (c *SessionController) SaveBootstrap(confirmed bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(BootstrapSetup) || !c.s.BootstrapPrepared {
		return c.reject(InvalidState)
	}
	if confirmed {
		if !c.s.SaveSubmitted {
			return c.reject(InvalidState)
		}
		return c.begin(BootstrapSetup, false, func(ctx context.Context, run Run) error {
			if err := c.cleanup(ctx, run); err != nil {
				return err
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return err
			}
			c.s.BootstrapSaved = true
			c.s.SavedBootstrap = BootstrapRecord{
				ProfileID: run.Profile.ID, Revision: run.Profile.Revision,
				SourceSHA256: run.Profile.SourceSHA256, BootstrapSHA256: run.Profile.BootstrapSHA256,
				SaveName: run.Profile.SaveName, Edition: run.Edition, Confirmed: true, SavedAt: time.Now().UTC(),
			}
			c.s.SideEffects = false
			c.transition(Ready)
			return nil
		}), nil
	}
	if c.s.SaveSubmitted {
		return c.reject(InvalidState)
	}
	return c.begin(BootstrapSetup, false, func(ctx context.Context, run Run) error {
		if err := c.env.Input.Send(ctx, run, Save, PreparedHost{}, c.report); err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		c.s.SaveSubmitted = true
		c.emit(Info, "", "Confirm the console completed SAVE, including any overwrite prompt.")
		return nil
	}), nil
}

func (c *SessionController) Start() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Ready) {
		return c.reject(InvalidState)
	}
	if !c.s.Transport.Capabilities.supports(c.s.Profile.RequiredTransport) {
		return c.reject(Unsupported)
	}
	if c.s.Profile.BootstrapSHA256 != "" && !c.s.BootstrapSaved && c.s.Stage == Ready {
		return c.configurationError(BootstrapUnconfirmed, "Prepare and save the matching bootstrap, or confirm its exact host and edition, before launching.")
	}
	if !c.s.CanStart() {
		return c.reject(InvalidConfiguration)
	}
	override := c.s.LANOverride
	return c.begin(PreparingHost, true, func(ctx context.Context, run Run) error {
		// Re-observe the selected address before any host or console side effect.
		run.LANIPv4 = override
		checks, err := c.env.Platform.Inspect(ctx, run)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.s.Checks = slices.Clone(checks)
		c.s.LANIPv4 = override
		for _, check := range checks {
			if check.ID == "network" && check.State == CheckVerified && check.LANIPv4 != "" {
				c.s.LANIPv4 = check.LANIPv4
			}
		}
		run.LANIPv4 = c.s.LANIPv4
		c.mu.Unlock()
		if len(checks) == 0 || slices.ContainsFunc(checks, func(check Check) bool { return check.Blocks(run.Transport) }) {
			return errors.New("readiness changed before launch")
		}
		host, err := c.env.Host.Prepare(ctx, run, c.report)
		if err != nil {
			return err
		}
		if !host.Listening || !host.PairingValid {
			return errors.New("host not ready")
		}
		if err := c.stage(ctx, WaitingForInput, false); err != nil {
			return err
		}
		stage := Inputting
		if run.Transport.Capabilities.Manual {
			stage = WaitingForConsole
		}
		if err := c.stage(ctx, stage, true); err != nil {
			return err
		}
		if err := c.env.Input.Send(ctx, run, Launch, host, c.report); err != nil {
			return err
		}
		if err := c.stage(ctx, RunningExploit, true); err != nil {
			return err
		}
		outcome, err := c.env.Host.Run(ctx, run, host, c.report)
		if err != nil {
			return err
		}
		if err := c.stage(ctx, Verifying, true); err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		c.s.Outcome = outcome
		if !outcome.valid(run.Profile.RetainsRPC) {
			c.emit(Error, VerificationIncomplete, "The complete verification contract was not met.")
			return errors.New("incomplete verification")
		}
		c.transition(Verified)
		if run.Profile.RetainsRPC {
			c.transition(RetainedRPC)
		}
		return nil
	}), nil
}

func (c *SessionController) Cancel() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.s.Busy || c.s.SideEffects || c.requested != "" ||
		(!slices.Contains([]Stage{Inspecting, PreparingHost, CivSwitchFetching, WaitingForInput, BootstrapSetup}, c.s.Stage) &&
			!(c.s.CivSwitch && c.s.Stage == Verifying)) {
		return c.reject(InvalidState)
	}
	c.requested = Ready
	c.cancel()
	c.emit(Info, "", "Cancellation requested; waiting for cleanup.")
	return c.snapshot(), nil
}

func (c *SessionController) Stop() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.requested != "" || !c.s.SideEffects ||
		!slices.Contains([]Stage{BootstrapSetup, PreparingHost, CivSwitchFetching, WaitingForInput, Inputting, WaitingForConsole, RunningExploit, Verifying, RunningTool}, c.s.Stage) {
		return c.reject(InvalidState)
	}
	if c.keyboard != nil {
		c.closeKeyboard()
		c.emit(Warning, "", "Stop requested; console restart required.")
		return c.snapshot(), nil
	}
	if c.s.Busy {
		c.requested = Indeterminate
		c.cancel()
		c.emit(Warning, "", "Stop requested; console restart required.")
		return c.snapshot(), nil
	}
	return c.begin(c.s.Stage, false, func(ctx context.Context, run Run) error { return errors.New("stopped") }), nil
}

func (c *SessionController) Finish() (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Verified, RetainedRPC) {
		return c.reject(InvalidState)
	}
	if c.s.Attached {
		return c.configurationError(InvalidState, "Disconnect instead: only the app that launched this session can finish it.")
	}
	if c.monitorCancel != nil {
		c.monitorCancel()
		c.monitorCancel = nil
	}
	return c.begin(c.s.Stage, false, func(ctx context.Context, run Run) error {
		if c.Snapshot().CivSwitch {
			if err := c.releaseCivSwitch(ctx); err != nil {
				return err
			}
			c.mu.Lock()
			c.s.SideEffects, c.s.LoaderVerified = false, false
			c.transition(Finished)
			c.mu.Unlock()
			return nil
		}
		outcome, err := c.env.Host.Finish(ctx, run)
		if err != nil {
			return err
		}
		if !outcome.valid(false) {
			return errors.New("finish cleanup not verified")
		}
		if err := c.cleanup(ctx, run); err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.s.Outcome = outcome
		c.s.SideEffects = false
		c.transition(Finished)
		return nil
	}), nil
}

func (c *SessionController) RunTool(tool string) (Snapshot, error) {
	return c.RunToolWith(ToolRequest{Tool: tool})
}

func (c *SessionController) SelectProfile(profile Profile, edition string) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Idle, NeedsSetup, Ready, Finished, Failed) {
		return c.reject(InvalidState)
	}
	if profile.ID == "" || profile.Revision == "" || (edition != "full" && edition != "demo" && edition != "civswitch") {
		return c.reject(InvalidConfiguration)
	}
	if len(profile.Editions) > 0 && !slices.Contains(profile.Editions, edition) {
		return c.configurationError(EditionUnsupported, "Select a supported SmileBASIC edition for "+profile.ID+"; use full SmileBASIC 4.4.9 or choose a demo-capable host.")
	}
	profile.Tools = slices.Clone(profile.Tools)
	profile.Editions = slices.Clone(profile.Editions)
	c.s.Profile, c.s.Edition = profile, edition
	c.s.CivSwitch, c.s.LoaderVerified = false, false
	c.s.ConsoleIPv4, c.s.EstimatedDuration = "", 0
	c.s.BootstrapSaved, c.s.BootstrapPrepared = false, false
	c.s.SavedBootstrap = BootstrapRecord{}
	c.s.SaveSubmitted = false
	c.s.Checks = nil
	c.transition(Idle)
	return c.snapshot(), nil
}

func (c *SessionController) configurationError(reason Reason, instruction string) (Snapshot, error) {
	err := &Rejection{Reason: reason, Stage: c.s.Stage, Instruction: instruction}
	c.emit(Warning, reason, instruction)
	return c.snapshot(), err
}

func (c *SessionController) configurationCause(reason Reason, cause error) (Snapshot, error) {
	instruction := cause.Error()
	err := &Rejection{Reason: reason, Stage: c.s.Stage, Instruction: instruction, Cause: cause}
	c.emit(Warning, reason, instruction)
	return c.snapshot(), err
}

// UseSavedBootstrap accepts only a recorded successful setup or explicit console
// confirmation for this exact host identity. Equal bootstrap bytes are not enough.
func (c *SessionController) UseSavedBootstrap(record BootstrapRecord) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Idle, NeedsSetup, Ready, Finished, Failed) {
		return c.reject(InvalidState)
	}
	p := c.s.Profile
	if !record.Confirmed || record.ProfileID != p.ID || record.Revision != p.Revision ||
		record.SourceSHA256 != p.SourceSHA256 || record.BootstrapSHA256 != p.BootstrapSHA256 ||
		record.SaveName != p.SaveName || record.Edition != c.s.Edition {
		c.s.BootstrapSaved = false
		c.s.SavedBootstrap = BootstrapRecord{}
		return c.configurationError(BootstrapMismatch, "Prepare and save the matching bootstrap for "+p.ID+" ("+c.s.Edition+") as "+p.SaveName+", then confirm the console save before launching.")
	}
	c.s.BootstrapSaved = true
	c.s.SavedBootstrap = record
	return c.snapshot(), nil
}

func (c *SessionController) ConfigureTransport(transport Transport, lanIPv4 string) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.eligible(Idle, NeedsSetup, Ready, Finished, Failed) {
		return c.reject(InvalidState)
	}
	ip, err := netip.ParseAddr(lanIPv4)
	if (lanIPv4 != "" && (err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast())) ||
		transport.Mode == "" || (transport.Capabilities.Manual && (transport.Capabilities.Keyboard || transport.Capabilities.Gamepad)) ||
		(!transport.Capabilities.Manual && !transport.Capabilities.Keyboard) {
		return c.reject(InvalidConfiguration)
	}
	c.s.Transport, c.s.LANIPv4, c.s.LANOverride = transport, lanIPv4, lanIPv4
	c.s.Checks = nil
	c.transition(Idle)
	return c.snapshot(), nil
}

// ExportDiagnostics is read-only and operates on a copy. The platform adapter
// must redact its own collected data and must not include credentials/pairing.
func (c *SessionController) ExportDiagnostics(ctx context.Context) (string, error) {
	return c.env.Platform.ExportDiagnostics(ctx, c.Snapshot())
}

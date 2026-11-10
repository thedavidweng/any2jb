// Package session owns the session state. Adapters report observations; only the
// controller may decide whether commands are eligible and a run is verified.
package session

import (
	"context"
	"slices"
	"time"
)

type Stage string

const (
	Idle              Stage = "idle"
	Inspecting        Stage = "inspecting"
	NeedsSetup        Stage = "needs_setup"
	Ready             Stage = "ready"
	BootstrapSetup    Stage = "bootstrap_setup"
	PreparingHost     Stage = "preparing_host"
	CivSwitchFetching Stage = "civswitch_fetching"
	WaitingForInput   Stage = "waiting_for_input"
	Inputting         Stage = "inputting"
	WaitingForConsole Stage = "waiting_for_console"
	RunningExploit    Stage = "running_exploit"
	Verifying         Stage = "verifying"
	Verified          Stage = "verified"
	RetainedRPC       Stage = "retained_rpc"
	RunningTool       Stage = "running_tool"
	Finished          Stage = "finished"
	Failed            Stage = "failed"
	Indeterminate     Stage = "indeterminate_console_restart_required"
)

type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

type Recovery string

const (
	NoRecovery     Recovery = "none"
	CorrectSetup   Recovery = "correct_setup"
	RetrySafe      Recovery = "retry_safe"
	RestartConsole Recovery = "restart_console"
)

type Reason string

const (
	InvalidState           Reason = "invalid_state"
	Unsupported            Reason = "unsupported_capability"
	InvalidConfiguration   Reason = "invalid_configuration"
	AdapterFailure         Reason = "adapter_failure"
	VerificationIncomplete Reason = "verification_incomplete"
	EditionUnsupported     Reason = "edition_unsupported"
	BootstrapMismatch      Reason = "bootstrap_mismatch"
	BootstrapUnconfirmed   Reason = "bootstrap_unconfirmed"
)

// Profile is the subset needed by orchestration, not a second source manifest.
// The manifest adapter supplies this pinned identity and capability projection.
type Profile struct {
	ID                 string
	Revision           string
	CivSwitch          bool
	RetainsRPC         bool
	RequiredTransport  Capabilities
	Tools              []string
	Editions           []string
	SourceSHA256       string
	BootstrapSHA256    string
	SaveName           string
	StartArgumentCount int
	StartPort          int
}
type Capabilities struct {
	Manual       bool
	Keyboard     bool
	Gamepad      bool
	ExternalPico bool
}

func (c Capabilities) supports(r Capabilities) bool {
	return (!r.Manual || c.Manual) && (!r.Keyboard || c.Keyboard) &&
		(!r.Gamepad || c.Gamepad) && (!r.ExternalPico || c.ExternalPico)
}

type Transport struct {
	Mode         string
	Capabilities Capabilities
}
type CheckState string

const (
	RequiresAttention CheckState = "requires_attention"
	Available         CheckState = "available_but_not_verified"
	CheckVerified     CheckState = "verified"
)

type Check struct {
	ID         string
	State      CheckState
	Message    string
	NextAction string
	// Empty gates every transport; usb-hid gates built-in USB input only.
	GatesTransport string
	// LANIPv4 records an observed local assignment, not console reachability.
	LANIPv4 string
}

func (c Check) Blocks(transport Transport) bool {
	if c.GatesTransport == "usb-hid" && (transport.Capabilities.Manual || transport.Capabilities.ExternalPico) {
		return false
	}
	return c.State != CheckVerified
}

type Outcome struct {
	Verified        bool
	FSVerified      bool
	Restored        bool
	CleanedUp       bool
	RetainedReady   bool
	CleanupDeferred bool
	DonorCleanedUp  bool
	RPCListening    bool
}

func (o Outcome) valid(held bool) bool {
	if !o.Verified || !o.FSVerified || !o.Restored {
		return false
	}
	if held {
		return o.RetainedReady && o.CleanupDeferred && o.DonorCleanedUp && o.RPCListening
	}
	return o.CleanedUp
}

type Process struct {
	PID  int
	Name string
}
type Snapshot struct {
	RunID             string
	Profile           Profile
	Edition           string
	LANIPv4           string
	LANOverride       string
	Transport         Transport
	Checks            []Check
	Stage             Stage
	Recovery          Recovery
	Current           int
	Total             int
	Outcome           Outcome
	Processes         []Process
	CleanupConfirmed  bool
	Simulated         bool
	Busy              bool
	SideEffects       bool
	BootstrapPrepared bool
	SaveSubmitted     bool
	BootstrapSaved    bool
	LastMessage       string
}

// CanStart is derived from the same copied state used by the controller.
func (s Snapshot) CanStart() bool {
	return s.Stage == Ready && !s.Busy && s.Profile.ID != "" && s.Profile.Revision != "" &&
		(s.Profile.BootstrapSHA256 == "" || s.BootstrapSaved) &&
		(len(s.Profile.Editions) == 0 || slices.Contains(s.Profile.Editions, s.Edition)) &&
		s.Transport.Capabilities.supports(s.Profile.RequiredTransport) && len(s.Checks) > 0 &&
		!slices.ContainsFunc(s.Checks, func(check Check) bool { return check.Blocks(s.Transport) })
}

type Event struct {
	RunID    string
	Sequence uint64
	Stage    Stage
	Elapsed  time.Duration
	Severity Severity
	Recovery Recovery
	Reason   Reason
	Message  string
}
type Rejection struct {
	Reason      Reason
	Stage       Stage
	Instruction string
}

func (e *Rejection) Error() string {
	if e.Instruction != "" {
		return e.Instruction
	}
	return string(e.Reason) + ": " + string(e.Stage)
}

type BootstrapRecord struct {
	ProfileID, Revision, SourceSHA256, BootstrapSHA256, SaveName, Edition string
	Confirmed                                                             bool
}

// Observations never transition the state machine. SideEffect must be reported
// before publishing pairing details, sending keys or otherwise touching console state.
type Progress struct {
	Current    int
	Total      int
	Message    string
	SideEffect bool
	Processes  []Process
}
type Report func(Progress)
type Run struct {
	ID        string
	Profile   Profile
	Edition   string
	LANIPv4   string
	Transport Transport
}
type PreparedHost struct {
	Listening    bool
	PairingValid bool
	// Opaque is private session-bound adapter data, never copied to snapshots/events.
	Opaque any
}
type InputAction string

const (
	Bootstrap InputAction = "bootstrap"
	Save      InputAction = "save"
	Launch    InputAction = "launch"
)

type HostRunner interface {
	Prepare(context.Context, Run, Report) (PreparedHost, error)
	Run(context.Context, Run, PreparedHost, Report) (Outcome, error)
	// Wait observes the owned host after held verification. Any exit before
	// Finish is unexpected; cancellation disarms the watcher.
	Wait(context.Context, Run) error
	Finish(context.Context, Run) (Outcome, error)
	Cleanup(context.Context, Run) error
}
type InputTransport interface {
	Send(context.Context, Run, InputAction, PreparedHost, Report) error
	Release(context.Context, Run) error
}
type CredentialStore interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte) error
	Delete(context.Context, string) error
}
type PlatformServices interface {
	Inspect(context.Context, Run) ([]Check, error)
	ExportDiagnostics(context.Context, Snapshot) (string, error)
	Credentials() CredentialStore
}
type ToolRunner interface {
	Run(context.Context, Run, string, Report) error
	Stop(context.Context, Run) error
}
type Environment struct {
	Host      HostRunner
	Input     InputTransport
	Platform  PlatformServices
	Tools     ToolRunner
	Simulated bool
}

// Package session owns the session state. Adapters report observations; only the
// controller may decide whether commands are eligible and a run is verified.
package session

import (
	"context"
	"errors"
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
	KeyboardUnavailable    Reason = "keyboard_unavailable"
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
	Controller   string
}
type CheckState string

const (
	RequiresAttention CheckState = "requires_attention"
	Available         CheckState = "available_but_not_verified"
	CheckVerified     CheckState = "verified"
)

const (
	CheckNetwork           = "network"
	CheckSIP               = "sip"
	CheckDriverPresent     = "driver-present"
	CheckDriverLoaded      = "driver-loaded"
	CheckDriverRestart     = "driver-restart"
	CheckUSBControllers    = "usb-controllers"
	CheckUSBPlatform       = "usb-platform"
	CheckUSBRuntime        = "usb-runtime"
	CheckRuntime           = "runtime"
	CheckEngine            = "engine"
	CheckInputAdapter      = "input-adapter"
	CheckLinuxUDC          = "linux-udc"
	CheckLinuxConfigFS     = "linux-configfs"
	CheckLinuxGadgetAccess = "linux-gadget-access"

	GateUSBHID = "usb-hid"
)

// Text renders a user-facing message in the active UI or CLI language while
// Check.Message and error.Error() preserve deterministic English for logs,
// JSON snapshots, and diagnostic exports.
type Text interface {
	Local() string
}

type Check struct {
	ID                   string
	State                CheckState
	Message              string
	MessageText          Text `json:"-"`
	NextAction           string
	NextActionText       Text   `json:"-"`
	EnumeratedController string `json:"-"`
	// Empty gates every transport; usb-hid gates built-in USB input only.
	GatesTransport string
	// Controller-specific checks do not gate an unselected USB controller.
	Controller string
	// LANIPv4 records an observed local assignment, not console reachability.
	LANIPv4 string
}

// AppliesTo reports whether this check governs the selected transport and USB controller.
func (c Check) AppliesTo(transport Transport) bool {
	if c.GatesTransport == GateUSBHID && (transport.Capabilities.Manual || transport.Capabilities.ExternalPico) {
		return false
	}
	if c.Controller != "" && c.Controller != transport.Controller {
		return false
	}
	return true
}

// IsDetectionCheck reports whether the check belongs to Stage 1 host/USB
// detection rather than Stage 3 route/profile verification.
func (c Check) IsDetectionCheck() bool {
	return c.ID != CheckRuntime && c.ID != CheckEngine && c.ID != CheckInputAdapter
}

func (c Check) Blocks(transport Transport) bool {
	// An unknown SIP observation is advisory; only observed enabled SIP blocks USB.
	if c.ID == CheckSIP && c.State == Available {
		return false
	}
	return c.AppliesTo(transport) && c.State != CheckVerified
}

func FindCheck(checks []Check, id string) Check {
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	return Check{}
}

func IsLinuxChecks(checks []Check) bool {
	return FindCheck(checks, CheckLinuxUDC).ID != ""
}

// DetectionChecksPass reports whether all Stage 1 detection checks applicable
// to transport are clear of RequiresAttention.
func DetectionChecksPass(checks []Check, transport Transport) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if check.IsDetectionCheck() && check.AppliesTo(transport) && check.State == RequiresAttention {
			return false
		}
	}
	return true
}

// RouteChecksVerified reports whether at least one Stage 3 route check applies
// to transport and every applicable route check is verified.
func RouteChecksVerified(checks []Check, transport Transport) bool {
	rows := 0
	for _, check := range checks {
		if !check.IsDetectionCheck() && check.AppliesTo(transport) {
			if check.State != CheckVerified {
				return false
			}
			rows++
		}
	}
	return rows > 0
}

// USBPrerequisitesReady reports whether all non-controller USB HID checks are
// clear of RequiresAttention.
func USBPrerequisitesReady(checks []Check) bool {
	for _, check := range checks {
		if check.GatesTransport == GateUSBHID && check.State == RequiresAttention && check.ID != CheckUSBControllers {
			return false
		}
	}
	return true
}

type Outcome struct {
	Target          string
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

// ErrReleaseUnconfirmed reports that a neutral key release was not
// acknowledged, so a console key may still be held down.
var ErrReleaseUnconfirmed = errors.New("neutral key release was not confirmed")

// ReleaseUnconfirmedWarning is the fixed instruction for an unconfirmed
// neutral release. The UI localizes it by this exact text.
const ReleaseUnconfirmedWarning = "Neutral release unconfirmed; reconnect the USB cable if a key repeats."

type Process struct {
	PID  int
	Name string
}
type Snapshot struct {
	CivSwitch         bool
	CivSwitchActions  CivSwitchState
	LoaderVerified    bool
	ConsoleIPv4       string
	EstimatedDuration time.Duration
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
	Remaining         time.Duration
	HasEstimate       bool
	KeyboardOpen      bool
	Outcome           Outcome
	// ReleaseUnconfirmed reports an unacknowledged neutral key release.
	ReleaseUnconfirmed bool
	Processes          []Process
	CleanupConfirmed   bool
	Attached           bool
	Busy               bool
	SideEffects        bool
	BootstrapPrepared  bool
	SaveSubmitted      bool
	BootstrapSaved     bool
	SavedBootstrap     BootstrapRecord
	LastMessage        string
}

// CanStart is derived from the same copied state used by the controller.
func (s Snapshot) CanStart() bool {
	return !s.CivSwitch && !s.Profile.CivSwitch && s.Stage == Ready && !s.Busy && s.Profile.ID != "" && s.Profile.Revision != "" &&
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
	Cause       error `json:"-"`
}

func (e *Rejection) Unwrap() error { return e.Cause }

func (e *Rejection) Error() string {
	if e.Instruction != "" {
		return e.Instruction
	}
	return string(e.Reason) + ": " + string(e.Stage)
}

type BootstrapRecord struct {
	ProfileID, Revision, SourceSHA256, BootstrapSHA256, SaveName, Edition string
	Confirmed                                                             bool
	SavedAt                                                               time.Time
}

// Observations never transition the state machine. SideEffect must be reported
// before publishing pairing details, sending keys or otherwise touching console state.
type Progress struct {
	// KeyboardController is emitted only after a real helper READY event.
	KeyboardController string
	// Remaining is helper-measured duration, meaningful only with HasEstimate.
	Remaining   time.Duration
	HasEstimate bool
	Current     int
	Total       int
	Message     string
	SideEffect  bool
	Processes   []Process
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
type PlatformServices interface {
	Inspect(context.Context, Run) ([]Check, error)
	ExportDiagnostics(context.Context, Snapshot) (string, error)
	ReleaseInterrupted(context.Context) error
}
type ToolRunner interface {
	Validate(ToolRequest) error
	RunWith(context.Context, Run, ToolRequest, Report) error
	Stop(context.Context, Run) error
}
type Environment struct {
	CivSwitch CivSwitchAdapter
	Host      HostRunner
	Retained  RetainedAdapter
	Input     InputTransport
	Platform  PlatformServices
	Tools     ToolRunner
}

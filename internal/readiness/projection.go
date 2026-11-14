package readiness

import (
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

// USBRoute describes what the observed checks say about built-in USB input.
type USBRoute int

const (
	USBPending USBRoute = iota
	USBUnavailable
	USBReady
)

// EvaluateUSBRoute classifies built-in USB input readiness from observed checks.
func EvaluateUSBRoute(checks []session.Check, detectionReady, driverAvailable bool) USBRoute {
	if session.IsLinuxChecks(checks) {
		udc := session.FindCheck(checks, session.CheckLinuxUDC)
		cfg := session.FindCheck(checks, session.CheckLinuxConfigFS)
		access := session.FindCheck(checks, session.CheckLinuxGadgetAccess)
		switch {
		case session.FindCheck(checks, session.CheckUSBPlatform).State == session.RequiresAttention,
			udc.State == session.RequiresAttention,
			session.FindCheck(checks, session.CheckUSBRuntime).State == session.RequiresAttention:
			return USBUnavailable
		case cfg.State == session.CheckVerified && access.State == session.CheckVerified && detectionReady:
			return USBReady
		}
		return USBPending
	}
	loaded := session.FindCheck(checks, session.CheckDriverLoaded)
	present := session.FindCheck(checks, session.CheckDriverPresent)
	switch {
	case session.FindCheck(checks, session.CheckUSBPlatform).State == session.RequiresAttention,
		session.FindCheck(checks, session.CheckUSBRuntime).State == session.RequiresAttention,
		!driverAvailable && loaded.State != session.CheckVerified && present.State != session.CheckVerified:
		return USBUnavailable
	case loaded.State == session.CheckVerified && detectionReady:
		return USBReady
	}
	return USBPending
}

// SingleFreeController returns the sole discovered controller not requiring attention,
// or "" when zero or multiple controllers are available.
func SingleFreeController(checks []session.Check) string {
	found := ""
	for _, check := range checks {
		if check.Controller == "" || check.State == session.RequiresAttention {
			continue
		}
		if found != "" {
			return ""
		}
		found = check.Controller
	}
	return found
}

type SetupAction int

const (
	SetupNoAction SetupAction = iota
	SetupShowRecovery
	SetupInstallDriver
	SetupChooseConnection
)

// SetupStep is one row of Stage 1 host readiness: subject, state, detail, and optional action.
type SetupStep struct {
	Key    string
	Title  string
	Status string
	Detail string
	State  session.CheckState
	Action SetupAction
}

// ProjectSetupSteps projects raw readiness checks into the 4 ADR-0004 Stage 1 rows.
func ProjectSetupSteps(s session.Snapshot, route USBRoute) []SetupStep {
	if session.IsLinuxChecks(s.Checks) {
		return projectLinuxSteps(s, route)
	}
	return projectDarwinSteps(s, route)
}

func projectLinuxSteps(s session.Snapshot, route USBRoute) []SetupStep {
	steps := []SetupStep{
		{Key: "mac-network", Title: locales.CheckNetwork()},
		{Key: "mac-allow", Title: locales.LinuxUDC()},
		{Key: "mac-install", Title: locales.LinuxConfigFS()},
		{Key: "mac-connect", Title: locales.MacConnect()},
	}
	status := locales.MacNotChecked()
	if s.Stage == session.Inspecting {
		status = locales.MacChecking()
	}
	for i := range steps {
		steps[i].State, steps[i].Status = session.Available, status
	}
	if len(s.Checks) == 0 || s.Stage == session.Inspecting {
		return steps
	}
	set := func(step *SetupStep, st session.CheckState, label, detail string, action SetupAction) {
		step.State, step.Status, step.Detail, step.Action = st, label, detail, action
	}
	network := session.FindCheck(s.Checks, session.CheckNetwork)
	udc := session.FindCheck(s.Checks, session.CheckLinuxUDC)
	cfg := session.FindCheck(s.Checks, session.CheckLinuxConfigFS)
	access := session.FindCheck(s.Checks, session.CheckLinuxGadgetAccess)
	controllers := session.FindCheck(s.Checks, session.CheckUSBControllers)
	unsupported := session.FindCheck(s.Checks, session.CheckUSBPlatform).State == session.RequiresAttention
	unavailable := route == USBUnavailable
	found := countControllerChecks(s.Checks)

	switch {
	case network.ID == "":
		steps[0].Status = locales.MacNotChecked()
	case network.State == session.CheckVerified:
		set(&steps[0], session.CheckVerified, locales.MacDone(), locales.LinuxNetworkDone(network.LANIPv4), SetupNoAction)
	default:
		set(&steps[0], session.RequiresAttention, locales.MacNeedsAction(), locales.LinuxNetworkAction(), SetupNoAction)
	}

	switch {
	case unsupported:
		steps[1].Status = locales.MacNotAvailable()
	case udc.State == session.CheckVerified:
		set(&steps[1], session.CheckVerified, locales.MacDone(), locales.LinuxUDCDone(), SetupNoAction)
	default:
		set(&steps[1], session.Available, locales.MacNotAvailable(), locales.LinuxUDCNone(), SetupNoAction)
	}

	switch {
	case unsupported || udc.State != session.CheckVerified:
		set(&steps[2], session.Available, locales.MacNotAvailable(), locales.LinuxConfigFSWaiting(), SetupNoAction)
	case unavailable:
		set(&steps[2], session.Available, locales.MacNotAvailable(), locales.MacUsbBlocked(), SetupNoAction)
	case cfg.State == session.CheckVerified && access.State == session.CheckVerified:
		set(&steps[2], session.CheckVerified, locales.MacDone(), locales.LinuxConfigFSDone(), SetupNoAction)
	case cfg.State != session.CheckVerified:
		set(&steps[2], session.RequiresAttention, locales.MacNeedsAction(), locales.LinuxConfigFSAction(), SetupNoAction)
	default:
		set(&steps[2], session.RequiresAttention, locales.MacNeedsAction(), locales.LinuxAccessAction(), SetupNoAction)
	}

	switch {
	case unsupported || udc.State != session.CheckVerified:
		set(&steps[3], session.Available, locales.MacNotAvailable(), locales.LinuxConnectNoUDC(), SetupNoAction)
	case unavailable:
		set(&steps[3], session.Available, locales.MacNotAvailable(), locales.MacUsbBlocked(), SetupNoAction)
	case controllers.State == session.CheckVerified:
		set(&steps[3], session.CheckVerified, locales.MacDone(), locales.LinuxConnectDone(), SetupNoAction)
	case controllers.State == session.Available:
		set(&steps[3], session.Available, locales.MacFound(), locales.MacConnectFound(), SetupChooseConnection)
	case cfg.State != session.CheckVerified || access.State != session.CheckVerified:
		set(&steps[3], session.Available, locales.MacWaiting(), locales.LinuxConnectWaiting(), SetupNoAction)
	case found > 1:
		set(&steps[3], session.RequiresAttention, locales.MacNeedsAction(), locales.MacConnectSeveral(), SetupChooseConnection)
	default:
		set(&steps[3], session.RequiresAttention, locales.MacNeedsAction(), locales.LinuxConnectAction(), SetupNoAction)
	}
	return steps
}

func projectDarwinSteps(s session.Snapshot, route USBRoute) []SetupStep {
	steps := []SetupStep{
		{Key: "mac-network", Title: locales.CheckNetwork()},
		{Key: "mac-allow", Title: locales.MacAllow()},
		{Key: "mac-install", Title: locales.MacInstall()},
		{Key: "mac-connect", Title: locales.MacConnect()},
	}
	status := locales.MacNotChecked()
	if s.Stage == session.Inspecting {
		status = locales.MacChecking()
	}
	for i := range steps {
		steps[i].State, steps[i].Status = session.Available, status
	}
	if len(s.Checks) == 0 || s.Stage == session.Inspecting {
		return steps
	}
	set := func(step *SetupStep, st session.CheckState, label, detail string, action SetupAction) {
		step.State, step.Status, step.Detail, step.Action = st, label, detail, action
	}
	network := session.FindCheck(s.Checks, session.CheckNetwork)
	sip := session.FindCheck(s.Checks, session.CheckSIP)
	present := session.FindCheck(s.Checks, session.CheckDriverPresent)
	loaded := session.FindCheck(s.Checks, session.CheckDriverLoaded)
	restart := session.FindCheck(s.Checks, session.CheckDriverRestart)
	controllers := session.FindCheck(s.Checks, session.CheckUSBControllers)
	unsupported := session.FindCheck(s.Checks, session.CheckUSBPlatform).State == session.RequiresAttention
	unavailable := route == USBUnavailable
	found := countControllerChecks(s.Checks)

	switch {
	case network.ID == "":
		steps[0].Status = locales.MacNotChecked()
	case network.State == session.CheckVerified:
		set(&steps[0], session.CheckVerified, locales.MacDone(), locales.MacNetworkDone(network.LANIPv4), SetupNoAction)
	default:
		set(&steps[0], session.RequiresAttention, locales.MacNeedsAction(), locales.MacNetworkAction(), SetupNoAction)
	}

	// Security is never lowered for a driver this build cannot install.
	switch {
	case unsupported:
		steps[1].Status = locales.MacNotAvailable()
	case unavailable:
		steps[1].Status = locales.MacWaiting()
	case loaded.State == session.CheckVerified:
		set(&steps[1], session.CheckVerified, locales.MacDone(), locales.MacAllowDone(), SetupNoAction)
	case sip.State == session.CheckVerified:
		set(&steps[1], session.Available, locales.MacInProgress(), locales.MacAllowProgress(), SetupShowRecovery)
	case sip.State == session.RequiresAttention:
		set(&steps[1], session.RequiresAttention, locales.MacNeedsAction(), locales.MacAllowAction(), SetupShowRecovery)
	default:
		set(&steps[1], session.Available, locales.MacNotChecked(), locales.MacAllowUnknown(), SetupShowRecovery)
	}

	switch {
	case unsupported:
		steps[2].Status = locales.MacNotAvailable()
	case restart.State == session.RequiresAttention:
		set(&steps[2], session.RequiresAttention, locales.MacNeedsAction(), locales.MacRestart(), SetupNoAction)
	case unavailable && loaded.State != session.CheckVerified:
		set(&steps[2], session.Available, locales.MacNotAvailable(), locales.MacInstallBlocked(), SetupNoAction)
	case unavailable:
		set(&steps[2], session.Available, locales.MacNotAvailable(), locales.MacUsbBlocked(), SetupNoAction)
	case loaded.State == session.CheckVerified:
		set(&steps[2], session.CheckVerified, locales.MacDone(), locales.MacInstallDone(), SetupNoAction)
	case present.State == session.CheckVerified:
		set(&steps[2], session.RequiresAttention, locales.MacNeedsAction(), locales.MacInstallNotRunning(), SetupNoAction)
	case present.ID == "":
		steps[2].Status = locales.MacNotChecked()
	default:
		set(&steps[2], session.RequiresAttention, locales.MacNeedsAction(), locales.MacInstallAction(), SetupInstallDriver)
	}

	// Discovery is never shown as done: only observed keyboard enumeration
	// verifies the connection.
	switch {
	case unsupported:
		set(&steps[3], session.Available, locales.MacNotAvailable(), locales.MacConnectUnsupported(), SetupNoAction)
	case unavailable:
		set(&steps[3], session.Available, locales.MacWaiting(), locales.MacConnectWaiting(), SetupNoAction)
	case controllers.State == session.CheckVerified:
		set(&steps[3], session.CheckVerified, locales.MacDone(), locales.MacConnectDone(), SetupNoAction)
	case controllers.State == session.Available:
		set(&steps[3], session.Available, locales.MacFound(), locales.MacConnectFound(), SetupChooseConnection)
	case loaded.State != session.CheckVerified:
		set(&steps[3], session.Available, locales.MacWaiting(), locales.MacConnectWaiting(), SetupNoAction)
	case found > 1:
		set(&steps[3], session.RequiresAttention, locales.MacNeedsAction(), locales.MacConnectSeveral(), SetupChooseConnection)
	default:
		set(&steps[3], session.RequiresAttention, locales.MacNeedsAction(), locales.MacConnectAction(), SetupNoAction)
	}
	return steps
}

func countControllerChecks(checks []session.Check) int {
	found := 0
	for _, check := range checks {
		if check.Controller != "" {
			found++
		}
	}
	return found
}

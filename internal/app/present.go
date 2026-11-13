package app

import (
	"errors"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/transport"
	"github.com/thedavidweng/any2jb/locales"
)

// CheckName returns the localized subject title for a readiness check.
func CheckName(check session.Check) string {
	switch check.ID {
	case session.CheckNetwork:
		return locales.CheckNetwork()
	case session.CheckSIP:
		return locales.CheckSip()
	case session.CheckDriverPresent:
		return locales.CheckDriverFiles()
	case session.CheckDriverLoaded:
		return locales.CheckDriverLoaded()
	case session.CheckDriverRestart:
		return locales.CheckDriverRestart()
	case session.CheckUSBPlatform:
		return locales.CheckUsbPort()
	case session.CheckUSBControllers:
		return locales.CheckUsbController()
	case session.CheckRuntime:
		return locales.CheckRuntime()
	case session.CheckEngine:
		return locales.CheckHostProgram()
	case session.CheckUSBRuntime:
		return locales.CheckUsbHelpers()
	case session.CheckInputAdapter:
		return locales.CheckTypingMethod()
	case session.CheckLinuxUDC:
		return locales.CheckLinuxUDC()
	case session.CheckLinuxConfigFS:
		return locales.CheckLinuxConfigFS()
	case session.CheckLinuxGadgetAccess:
		return locales.CheckLinuxAccess()
	}
	if check.Controller != "" {
		return locales.CheckUsbController() + " " + check.Controller
	}
	return locales.CheckOther()
}

// CheckStateTitle returns the localized badge label for state.
func CheckStateTitle(state session.CheckState) string {
	switch state {
	case session.CheckVerified:
		return locales.CheckVerified()
	case session.Available:
		return locales.CheckAvailable()
	default:
		return locales.CheckAttention()
	}
}

// StageTitle returns the localized session stage label.
func StageTitle(stage session.Stage) string {
	switch stage {
	case session.Idle:
		return locales.Home()
	case session.Inspecting:
		return locales.CheckingReadiness()
	case session.NeedsSetup:
		return locales.SetupNeedsAttention()
	case session.Ready:
		return locales.ReadyToLaunch()
	case session.BootstrapSetup:
		return locales.BootstrapSetup()
	case session.Verified:
		return locales.VerifiedSession()
	case session.RetainedRPC:
		return locales.RetainedSession()
	case session.Finished:
		return locales.SessionFinished()
	case session.Failed:
		return locales.SessionFailed()
	case session.Indeterminate:
		return locales.RecoveryRequired()
	default:
		return locales.SessionInProgress()
	}
}

// CheckMessage formats check's localized observation message.
func CheckMessage(check session.Check, controller string) string {
	if check.EnumeratedController != "" {
		return locales.UsbEnumerationObserved(check.EnumeratedController)
	}
	if check.State == session.CheckVerified && controller != "" &&
		(check.ID == session.CheckUSBControllers || check.Controller == controller) {
		return locales.UsbEnumerationObserved(controller)
	}
	if check.MessageText != nil {
		return check.MessageText.Local()
	}
	return check.Message
}

// CheckNextAction formats check's localized remediation guidance.
func CheckNextAction(check session.Check) string {
	if check.NextActionText != nil {
		return check.NextActionText.Local()
	}
	return check.NextAction
}

// PresentError formats err using structured catalog messages when available.
func PresentError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, session.ErrCivSwitchUnstaged) {
		return locales.DynCivNotStaged()
	}
	if errors.Is(err, session.ErrReleaseUnconfirmed) {
		return locales.DynReleaseUnconfirmed()
	}
	return locales.LocalizeError(err)
}

// CivSwitchUnavailableLocal returns the localized reason CivSwitch cannot run,
// or empty string when available.
func CivSwitchUnavailableLocal(c *session.SessionController) string {
	if c == nil {
		return ""
	}
	return PresentError(c.CivSwitchAvailability())
}

// KnownRejectionInstruction maps a typed session.Rejection to localized user guidance.
func KnownRejectionInstruction(s session.Snapshot, reject *session.Rejection) string {
	if reject == nil {
		return ""
	}
	switch reject.Reason {
	case session.EditionUnsupported:
		return locales.UnsupportedEdition()
	case session.BootstrapMismatch, session.BootstrapUnconfirmed:
		return locales.MatchingBootstrapRequired(s.Profile.ID, s.Edition, s.Profile.SaveName)
	case session.KeyboardUnavailable:
		return locales.UsbKeyboardUnavailable()
	}
	return ""
}

// ManualInstruction returns the localized console prompt for a guided manual step.
func ManualInstruction(kind string) string {
	return map[string]string{
		"slot": locales.ManualSlot(), "bootstrap": locales.ManualBootstrap(),
		"direct": locales.ManualDirect(), "save": locales.ManualSave(),
		"load": locales.ManualLoad(), "load-confirm": locales.ManualLoadConfirm(),
		"run": locales.ManualRun(), "remove": locales.ManualRemove(), "start": locales.ManualStart(),
	}[kind]
}

// ConsolePrompt returns the localized console prompt for a USB typing pause.
func ConsolePrompt(step transport.ConsoleStep) string {
	switch step {
	case transport.EditorFocus:
		return locales.UsbConfirmEditor()
	case transport.LoadCommand:
		return locales.UsbConfirmLoad()
	case transport.LoadPrompt:
		return locales.UsbConfirmLoadPrompt()
	case transport.RunCommand:
		return locales.UsbConfirmRun()
	case transport.RemoveCommand:
		return locales.UsbConfirmRemove()
	case transport.StartCommand:
		return locales.UsbConfirmStart()
	case transport.SaveCommand:
		return locales.UsbConfirmSave()
	}
	return ""
}

// StartupSecurityApplies names the built-in USB routes whose driver needs
// startup security the app can only describe, never observe or change.
func StartupSecurityApplies(t session.Transport) bool {
	return !t.Capabilities.Manual && !t.Capabilities.ExternalPico
}

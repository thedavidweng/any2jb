package readiness

import (
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

func usbChecks(o Observation) []session.Check {
	if !o.USBSupported {
		return []session.Check{makeUSBCheck(session.CheckUSBPlatform,
			locales.Key("dynUsbUnsupported"), locales.Key("dynUsbChooseManual"), session.RequiresAttention)}
	}
	if o.Linux != nil {
		return linuxUSBChecks(o)
	}
	present := makeUSBCheck(session.CheckDriverPresent,
		locales.Key("dynUsbKextAbsent"), locales.Key("dynUsbReviewDriver"), session.RequiresAttention)
	if o.Driver.PresentError != nil {
		setCheckText(&present, locales.Key("dynUsbKextNotInspected"), locales.Key("dynUsbReviewDriver"))
	} else if o.Driver.Present {
		present.State = session.CheckVerified
		setCheckText(&present, locales.Key("dynUsbKextPresent"), locales.Text{})
	}
	restart := makeUSBCheck(session.CheckDriverRestart,
		locales.Key("dynUsbNoRestartPending"), locales.Text{}, session.CheckVerified)
	var restartAction locales.Text
	if o.Driver.RestartError != nil {
		restart.State = session.RequiresAttention
		restartAction = locales.Key("dynUsbCheckRestartMarker")
		setCheckText(&restart, locales.Key("dynUsbRestartUnknown"), restartAction)
	} else if o.Driver.RestartPending {
		restart.State = session.RequiresAttention
		restartAction = locales.Key("dynUsbRestartAction")
		setCheckText(&restart, locales.Key("dynUsbRestartRequired"), restartAction)
	}
	loaded := makeUSBCheck(session.CheckDriverLoaded,
		locales.Key("dynUsbKextNotLoaded"), locales.Key("dynUsbCompleteApproval"), session.RequiresAttention)
	if o.Driver.LoadError != nil {
		setCheckText(&loaded, locales.Key("dynUsbLoadedUnknown"), locales.Key("dynUsbCheckInspection"))
	} else if o.Driver.RestartPending || o.Driver.RestartError != nil {
		setCheckText(&loaded, locales.Key("dynUsbKextTreatedUnloaded"), restartAction)
	} else if o.Driver.Loaded {
		loaded.State = session.CheckVerified
		setCheckText(&loaded, locales.Key("dynUsbKextLoaded"), locales.Text{})
	}
	sip := makeUSBCheck(session.CheckSIP,
		locales.Key("dynSipUnknown"), locales.Key("dynSipUnknownAction"), session.Available)
	switch o.SIPStatus {
	case "enabled":
		sip.State = session.RequiresAttention
		setCheckText(&sip, locales.Key("dynSipEnabled"), locales.Key("dynSipEnabledAction"))
	case "disabled":
		sip.State = session.CheckVerified
		setCheckText(&sip, locales.Key("dynSipDisabled"), locales.Text{})
	}
	checks := []session.Check{sip, present, loaded, restart}
	controllers := makeUSBCheck(session.CheckUSBControllers,
		locales.Key("dynUsbNoControllers"), locales.Key("dynUsbCheckCableDirection"), session.RequiresAttention)
	if o.ControllerError != nil {
		setCheckText(&controllers, locales.Key("dynUsbDiscoveryFailed"), locales.Key("dynUsbCheckRegistry"))
	} else if len(o.Controllers) > 1 {
		setCheckText(&controllers, locales.Key("dynUsbMultipleControllers"), locales.Key("dynUsbVerifyController"))
	} else if len(o.Controllers) == 1 {
		controllers.State = session.Available
		setCheckText(&controllers, locales.Key("dynUsbOneController"), locales.Key("dynUsbVerifyEnumeration"))
	}
	checks = append(checks, controllers)
	if o.ControllerError == nil {
		for _, id := range o.Controllers {
			controller := makeUSBCheck(id,
				locales.Format("dynUsbController", map[string]any{"controller": id}),
				locales.Key("dynUsbConnectCable"), session.Available)
			controller.Controller = id
			checks = append(checks, controller)
		}
	}
	return checks
}

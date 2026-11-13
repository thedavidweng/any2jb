package readiness

import "github.com/thedavidweng/any2jb/session"

func usbChecks(o Observation) []session.Check {
	check := func(id, message, action string, state session.CheckState) session.Check {
		return session.Check{ID: id, Message: message, NextAction: action, State: state, GatesTransport: "usb-hid"}
	}
	if !o.USBSupported {
		return []session.Check{check("usb-platform", "Built-in USB device-mode input is not supported by this platform adapter.",
			"Choose guided manual input. Ordinary USB-C ports do not establish USB device-mode support.", session.RequiresAttention)}
	}
	if o.Linux != nil {
		return linuxUSBChecks(o, check)
	}
	present := check("driver-present", "The USB gadget kext bundle is absent.",
		"Review driver setup and macOS approval requirements, or choose guided manual input.", session.RequiresAttention)
	if o.Driver.PresentError != nil {
		present.Message = "The USB gadget kext bundle could not be inspected."
	} else if o.Driver.Present {
		present.State, present.Message, present.NextAction = session.CheckVerified,
			"The USB gadget kext bundle is present. Presence does not prove integrity or loading.", ""
	}
	restart := check("driver-restart", "No driver installation restart is pending.", "", session.CheckVerified)
	if o.Driver.RestartError != nil {
		restart.State, restart.Message, restart.NextAction = session.RequiresAttention,
			"Driver restart state could not be determined.", "Check the driver restart marker and system boot time, then refresh readiness."
	} else if o.Driver.RestartPending {
		restart.State, restart.Message, restart.NextAction = session.RequiresAttention,
			"A driver installation requires a Mac restart.", "Restart this Mac, then refresh readiness before using USB input."
	}
	loaded := check("driver-loaded", "The USB gadget kext is not loaded.",
		"Complete macOS driver approval, restart if requested, then refresh readiness; or choose guided manual input.", session.RequiresAttention)
	if o.Driver.LoadError != nil {
		loaded.Message = "Loaded driver state could not be inspected."
		loaded.NextAction = "Check driver inspection access, then refresh readiness; or choose guided manual input."
	} else if o.Driver.RestartPending || o.Driver.RestartError != nil {
		loaded.Message, loaded.NextAction = "The USB gadget kext is treated as not loaded until restart state is clear.", restart.NextAction
	} else if o.Driver.Loaded {
		loaded.State, loaded.Message, loaded.NextAction = session.CheckVerified,
			"The running kernel reports the USB gadget kext loaded. USB enumeration is not verified.", ""
	}
	sip := check("sip", "SIP status could not be determined.",
		"Check SIP status yourself in macOS Recovery, then refresh readiness; or choose guided manual input. This app never changes security settings.", session.Available)
	switch o.SIPStatus {
	case "enabled":
		sip.State = session.RequiresAttention
		sip.Message = "SIP is enabled. The unsigned USB gadget driver requires SIP disabled."
		sip.NextAction = "Disable SIP yourself in macOS Recovery before using the unsigned USB gadget driver, or choose guided manual input. This app never changes security settings."
	case "disabled":
		sip.State, sip.Message, sip.NextAction = session.CheckVerified,
			"SIP reports disabled.", ""
	}
	checks := []session.Check{sip, present, loaded, restart}
	controllers := check("usb-controllers", "No attached USB gadget controllers were discovered.",
		"Check driver loading and the USB-C data cable direction, then refresh readiness; or choose guided manual input.", session.RequiresAttention)
	if o.ControllerError != nil {
		controllers.Message = "USB controller discovery could not be completed."
		controllers.NextAction = "Check USB registry access and refresh readiness, or choose guided manual input."
	} else if len(o.Controllers) > 1 {
		controllers.Message = "Multiple attached USB gadget controllers were discovered; the connected controller is ambiguous."
		controllers.NextAction = "More than one controller was discovered. Verify the connected controller before using USB input, or choose guided manual input."
	} else if len(o.Controllers) == 1 {
		controllers.State = session.Available
		controllers.Message = "One attached USB gadget controller was discovered. Keyboard enumeration and console input are not verified."
		controllers.NextAction = "Verify keyboard enumeration on the connected console before using USB input, or choose guided manual input."
	}
	checks = append(checks, controllers)
	if o.ControllerError == nil {
		for _, id := range o.Controllers {
			controller := check(id, id+": attached gadget controller, available but not verified.",
				"Connect a USB-C data cable so the Mac receives power from the console; verify keyboard enumeration before sending input.", session.Available)
			controller.Controller = id
			checks = append(checks, controller)
		}
	}
	return checks
}

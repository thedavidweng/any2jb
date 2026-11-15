package readiness

import (
	"testing"

	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/locales"
)

func TestParsePowerSource(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
	}{
		{"AC", "Now drawing from 'AC Power'\n -InternalBattery-0 100%; AC attached; not charging\n", "ac"},
		{"battery", "Now drawing from 'Battery Power'\n -InternalBattery-0 42%; discharging; 4:30 remaining\n", "battery"},
		{"unknown source", "Now drawing from 'UPS Power'\n", "unknown"},
		{"missing source", " -InternalBattery-0 42%; discharging\n", "unknown"},
		{"malformed source", "Now drawing from AC Power\n", "unknown"},
		{"unterminated source", "Now drawing from 'AC Power\n", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePowerSource(tc.output); got != tc.want {
				t.Fatalf("parsePowerSource(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

func TestUSBPowerSourceCheckStates(t *testing.T) {
	for _, tc := range []struct {
		source, messageKey, actionKey string
		state                         session.CheckState
	}{
		{"battery", "dynUsbPowerBattery", "dynUsbPowerBatteryAction", session.RequiresAttention},
		{"ac", "dynUsbPowerAC", "dynUsbPowerACAction", session.Available},
		{"unknown", "dynUsbPowerUnknown", "dynUsbPowerUnknownAction", session.Available},
		{"", "dynUsbPowerUnknown", "dynUsbPowerUnknownAction", session.Available},
	} {
		t.Run(tc.source, func(t *testing.T) {
			checks := usbChecks(Observation{USBSupported: true, PowerSource: tc.source})
			check := session.FindCheck(checks, session.CheckUSBPowerSource)
			if check.ID == "" || check.State != tc.state ||
				check.Message != locales.Key(tc.messageKey).English() ||
				check.NextAction != locales.Key(tc.actionKey).English() ||
				check.GatesTransport != session.GateUSBHID {
				t.Fatalf("unexpected power-source check: %+v", check)
			}
			transport := session.Transport{Capabilities: session.Capabilities{Keyboard: true}}
			if check.Blocks(transport) != (tc.state == session.RequiresAttention) {
				t.Fatalf("state %q has incorrect launch blocking behavior", tc.state)
			}
			if check.Blocks(session.Transport{Capabilities: session.Capabilities{Manual: true}}) {
				t.Fatal("USB power check blocks guided manual input")
			}
		})
	}
}

func TestDarwinConnectStepShowsBatteryPowerBlock(t *testing.T) {
	checks := []session.Check{
		{ID: session.CheckNetwork, State: session.CheckVerified},
		{ID: session.CheckSIP, State: session.CheckVerified},
		{ID: session.CheckDriverPresent, State: session.CheckVerified},
		{ID: session.CheckDriverLoaded, State: session.CheckVerified},
		{ID: session.CheckDriverRestart, State: session.CheckVerified},
		{ID: session.CheckUSBControllers, State: session.CheckVerified},
		{ID: session.CheckUSBPowerSource, State: session.RequiresAttention},
	}
	steps := ProjectSetupSteps(session.Snapshot{Stage: session.Ready, Checks: checks}, USBUnavailable)
	connect := steps[3]
	if connect.State != session.RequiresAttention ||
		connect.Status != locales.MacNeedsAction() ||
		connect.Detail != locales.Key("dynUsbPowerStepAction").English() {
		t.Fatalf("battery power was hidden by connection projection: %+v", connect)
	}
}

func TestPowerSourceInspectionDoesNotAffectManualReadiness(t *testing.T) {
	checks := usbChecks(Observation{USBSupported: true, PowerSource: "battery"})
	manual := session.Transport{Capabilities: session.Capabilities{Manual: true}}
	for _, check := range checks {
		if check.Blocks(manual) {
			t.Fatalf("USB observation %q blocks manual transport", check.ID)
		}
	}
}

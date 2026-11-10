package hwevidence

import (
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/manifest"
)

func pepsipuDigest(t *testing.T) string {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Profile("pepsipu")
	if err != nil {
		t.Fatal(err)
	}
	return p.BootstrapSHA256
}

// validP1A returns a complete, valid USB HID qualification log.
func validP1A(t *testing.T) *Log {
	t.Helper()
	return &Log{
		Schema: Schema,
		Date:   "2026-11-20",
		Tester: "david",
		Route:  RouteP1A,
		Combination: Combination{
			MacModel:        "MacBook Pro (Apple Silicon)",
			MacOSVersion:    "26.1 (25B78)",
			StartupSecurity: "Reduced Security with user-managed kernel extensions",
			HostProfile:     "pepsipu",
			ConsoleSystem:   "23.0.1",
			GameEdition:     "full",
			GameVersion:     "4.4.9",
			BootstrapSHA256: pepsipuDigest(t),
			Transport:       TransportUSBHID,
			USBController:   "usb-drd0",
			Cable:           "USB-C data cable, console supplies power",
			LANTopology:     "Switch 2 on Ethernet, Mac on the same LAN",
		},
		Steps: []Step{
			{ID: "helper-dry-run", Outcome: OutcomePass, Log: "sender/commands/keyboard/civswitch-hid dry-runs passed via cmd/check"},
			{ID: "usb-enumeration", Outcome: OutcomePass, Log: "MAC2JB_KEYBOARD_READY on usb-drd0"},
			{ID: "harmless-typing", Outcome: OutcomePass, Log: "typed into TextEdit and a SmileBASIC new-line prompt"},
			{ID: "stop-key-release", Outcome: OutcomePass, Log: "no repeating keys after Stop"},
			{ID: "console-bootstrap-save", Outcome: OutcomePass, Log: "SAVE confirmed, empty Direct prompt"},
			{ID: "host-connection", Outcome: OutcomePass, Log: "host paired"},
			{ID: "verification-4of4", Outcome: OutcomePass, Log: "structured 4/4 result"},
			{ID: "retained-rpc-readonly", Outcome: OutcomeBlocked, Notes: "pepsipu declares no held RPC", Log: "Advanced Tools shows the capability reason"},
		},
	}
}

func TestValidateAcceptsCompleteUSBHIDLog(t *testing.T) {
	if err := validP1A(t).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsBadLogs(t *testing.T) {
	cases := map[string]func(*Log){
		"schema":                   func(l *Log) { l.Schema = "other" },
		"date":                     func(l *Log) { l.Date = "20.11.2026" },
		"tester":                   func(l *Log) { l.Tester = " " },
		"route":                    func(l *Log) { l.Route = "p2" },
		"missing macOS version":    func(l *Log) { l.Combination.MacOSVersion = "" },
		"missing startup security": func(l *Log) { l.Combination.StartupSecurity = "" },
		"unknown host profile":     func(l *Log) { l.Combination.HostProfile = "unknown" },
		"bootstrap digest mismatch": func(l *Log) {
			l.Combination.BootstrapSHA256 = strings.Repeat("0", 64)
		},
		"edition":    func(l *Log) { l.Combination.GameEdition = "trial" },
		"transport":  func(l *Log) { l.Combination.Transport = "wifi" },
		"controller": func(l *Log) { l.Combination.USBController = "" },
		"cable":      func(l *Log) { l.Combination.Cable = "" },
		"lan":        func(l *Log) { l.Combination.LANTopology = "" },
		"missing required step": func(l *Log) {
			l.Steps = l.Steps[:len(l.Steps)-2]
		},
		"unknown step": func(l *Log) {
			l.Steps = append(l.Steps, Step{ID: "teleport", Outcome: OutcomePass, Log: "x"})
		},
		"out of order steps": func(l *Log) {
			l.Steps[0], l.Steps[1] = l.Steps[1], l.Steps[0]
		},
		"pass after failed step": func(l *Log) {
			l.Steps[1].Outcome = OutcomeFail
			l.Steps[1].Notes = "controller not observed"
		},
		"fail without notes": func(l *Log) {
			l.Steps[0].Outcome = OutcomeFail
		},
		"step without log": func(l *Log) {
			l.Steps[2].Log = " "
		},
		"unredacted pairing command": func(l *Log) {
			l.Steps[5].Log = "V=START(1,2,3,4,5,6,7,8,9) accepted"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			log := validP1A(t)
			mutate(log)
			if err := log.Validate(); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestValidateGuidedManualSequence(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	kurumuz, err := m.Profile("kurumuz")
	if err != nil {
		t.Fatal(err)
	}
	log := &Log{
		Schema: Schema,
		Date:   "2026-11-20",
		Tester: "david",
		Route:  RouteP1A,
		Combination: Combination{
			MacModel:        "Mac mini (Apple Silicon)",
			MacOSVersion:    "26.1 (25B78)",
			HostProfile:     "kurumuz",
			ConsoleSystem:   "23.0.1",
			GameEdition:     "demo",
			GameVersion:     "4.4.6",
			BootstrapSHA256: kurumuz.BootstrapSHA256,
			Transport:       TransportGuidedManual,
			Cable:           "none; manual entry",
			LANTopology:     "Switch 2 on Ethernet, Mac on the same LAN",
		},
		Steps: []Step{
			{ID: "console-bootstrap-save", Outcome: OutcomePass, Log: "SAVEV confirmed by hand"},
			{ID: "host-connection", Outcome: OutcomePass, Log: "LOAD/RUN/REMOVE/START typed per guide"},
			{ID: "verification-4of4", Outcome: OutcomePass, Log: "structured 4/4 result"},
			{ID: "retained-rpc-readonly", Outcome: OutcomePass, Log: "rpc-status and system-code-dump read-only checks passed"},
		},
	}
	if err := log.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsUnsupportedEdition(t *testing.T) {
	log := validP1A(t)
	log.Combination.GameEdition = "demo"
	log.Combination.GameVersion = "4.4.6"
	if err := log.Validate(); err == nil {
		t.Fatal("pepsipu does not support the demo edition")
	}
}

func TestValidateCivSwitchSequence(t *testing.T) {
	log := &Log{
		Schema: Schema,
		Date:   "2026-11-21",
		Tester: "david",
		Route:  RouteCivSwitch,
		Combination: Combination{
			MacModel:          "MacBook Pro (Apple Silicon)",
			MacOSVersion:      "26.1 (25B78)",
			StartupSecurity:   "Reduced Security with user-managed kernel extensions",
			ConsoleGeneration: "Switch 1",
			ConsoleSystem:     "20.5.0",
			GameVersion:       "Civilization VII cartridge 1.0.0",
			SystemLanguage:    "English (US)",
			Transport:         TransportCompositeHID,
			USBController:     "usb-drd0",
			Cable:             "USB-C data cable",
			LANTopology:       "Switch 1 and Mac on the same LAN",
		},
		Steps: []Step{
			{ID: "civ-prerequisites", Outcome: OutcomePass, Log: "cartridge, language and chat focus confirmed"},
			{ID: "civ-composite-enumeration", Outcome: OutcomePass, Log: "keyboard and gamepad interfaces enumerated"},
			{ID: "civ-chat-transfer", Outcome: OutcomePass, Log: "87/87 lines"},
			{ID: "civ-stop-release", Outcome: OutcomePass, Log: "both interfaces released"},
			{ID: "civ-loader-verify", Outcome: OutcomePass, Log: "nonce verified on TCP 9026"},
			{ID: "civ-save-backup", Outcome: OutcomePass, Log: "save:/saves/Single backup verified"},
			{ID: "civ-trigger-install", Outcome: OutcomePass, Log: "INSTALL DONE - reboot, Load Game"},
		},
	}
	if err := log.Validate(); err != nil {
		t.Fatal(err)
	}
}

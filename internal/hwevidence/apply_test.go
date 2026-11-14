package hwevidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func named(t *testing.T, log *Log, name string) *Log {
	t.Helper()
	log.Name = name
	return log
}

func verifiedSet(t *testing.T, logs ...*Log) map[string][]string {
	t.Helper()
	rows, err := VerifiedRows(logs)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestVerifiedRowsUSBHID(t *testing.T) {
	log := named(t, validP1A(t), "2026-11-20-p1a-pepsipu.json")
	rows := verifiedSet(t, log)
	for _, want := range []string{
		"usb-role", "usb-probe", "keyboard-text", "keyboard-stop",
		"run-cancel", "run-stop",
		"bootstrap-open", "bootstrap-type", "keyboard-indent", "bootstrap-save-full",
		"bootstrap-confirm", "bootstrap-reuse",
		"host-only", "start-jailbreak",
		"command-load", "command-run", "command-remove", "command-start",
		"run-progress",
	} {
		if len(rows[want]) == 0 {
			t.Errorf("%s not verified by a passing log", want)
		}
	}
	for _, unwanted := range []string{
		"bootstrap-save-demo", "manual-launch",
		"host-hold", "rpc-check", "rpc-ownership", "system-dump", "run-finish",
		"host-pepsipu",
	} {
		if len(rows[unwanted]) != 0 {
			t.Errorf("%s must not be verified by this log", unwanted)
		}
	}
}

func TestVerifiedRowsGuidedManualDemo(t *testing.T) {
	log := &Log{
		Schema: Schema, Date: "2026-11-20", Tester: "david", Route: RouteP1A,
		Combination: Combination{Transport: TransportGuidedManual, GameEdition: "demo"},
		Steps: []Step{
			{ID: "console-bootstrap-save", Outcome: OutcomePass, Log: "x"},
			{ID: "host-connection", Outcome: OutcomePass, Log: "x"},
			{ID: "verification-4of4", Outcome: OutcomePass, Log: "x"},
			{ID: "retained-rpc-readonly", Outcome: OutcomePass, Log: "x"},
		},
		Name: "2026-11-20-p1a-kurumuz-manual.json",
	}
	rows := verifiedSet(t, log)
	for _, want := range []string{"bootstrap-save-demo", "manual-launch", "host-hold", "rpc-check", "run-finish"} {
		if len(rows[want]) == 0 {
			t.Errorf("%s not verified", want)
		}
	}
	for _, unwanted := range []string{"bootstrap-save-full", "bootstrap-type", "keyboard-indent", "start-jailbreak", "usb-probe"} {
		if len(rows[unwanted]) != 0 {
			t.Errorf("%s must not be verified by a guided manual demo log", unwanted)
		}
	}
}

const ledgerFixture = `# ledger

| ID | Group | Capability / action | Tier | Upstream source | Preconditions | Risk | Disposition | Evidence | Experimental | Evidence link | Tickets | UI keys |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| usb-probe | USB | Probe controllers | P1a | src | HID | input | gated | not verified | false | [not verified](#not-verified) | 09 | - |
| keyboard-stop | Keyboard | Release keys | P1b | src | HID | input | gated | integration-tested | false | [tests](../transport/keyboard_test.go) | 12 | stop |
| run-progress | Core | 4/4 result | P1a | src | Core | read-only | gated | not verified | false | [not verified](#not-verified) | 08 | - |
| host-pepsipu | Hosts | pepsipu host | P1a | src | Core | session | gated | blocked | false | [release blocker](#redistribution-release-blockers) | 04 | host-profile |
| host-hold | Core | Hold RPC | P1a | src | RPC | session | gated | not verified | false | [not verified](#not-verified) | 14 | - |
`

func writeLedger(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parity-ledger.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApplyAdvancesOnlyPassingRows(t *testing.T) {
	log := named(t, validP1A(t), "2026-11-20-p1a-pepsipu.json")
	path := writeLedger(t, ledgerFixture)
	advanced, err := ApplyLedger(path, log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"usb-probe", "keyboard-stop", "run-progress"} {
		found := false
		for _, id := range advanced {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s not advanced", want)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "| usb-probe | USB | Probe controllers | P1a | src | HID | input | gated | hardware-verified | false | [hardware evidence](hardware-evidence/2026-11-20-p1a-pepsipu.json) | 09 | - |") {
		t.Error("usb-probe row not rewritten with hardware evidence link")
	}
	if !strings.Contains(text, "| host-hold | Core | Hold RPC | P1a | src | RPC | session | gated | not verified |") {
		t.Error("blocked retained-RPC step must not advance host-hold")
	}
}

func TestApplyRefusesBlockedRow(t *testing.T) {
	log := &Log{
		Schema: Schema, Date: "2026-11-20", Tester: "david", Route: RouteP1A,
		Combination: Combination{Transport: TransportUSBHID, GameEdition: "full"},
		Steps: []Step{
			{ID: "helper-dry-run", Outcome: OutcomePass, Log: "x"},
			{ID: "usb-enumeration", Outcome: OutcomePass, Log: "x"},
			{ID: "harmless-typing", Outcome: OutcomePass, Log: "x"},
			{ID: "stop-key-release", Outcome: OutcomePass, Log: "x"},
			{ID: "console-bootstrap-save", Outcome: OutcomePass, Log: "x"},
			{ID: "host-connection", Outcome: OutcomePass, Log: "x"},
			{ID: "verification-4of4", Outcome: OutcomePass, Log: "x"},
			{ID: "retained-rpc-readonly", Outcome: OutcomePass, Log: "x"},
		},
		Name: "log.json",
	}
	fixture := strings.Replace(ledgerFixture,
		"| host-hold | Core | Hold RPC | P1a | src | RPC | session | gated | not verified |",
		"| host-hold | Core | Hold RPC | P1a | src | RPC | session | gated | blocked |", 1)
	path := writeLedger(t, fixture)
	if _, err := ApplyLedger(path, log); err == nil {
		t.Fatal("a blocked row needs distribution clearance, not hardware evidence")
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	log := named(t, validP1A(t), "2026-11-20-p1a-pepsipu.json")
	path := writeLedger(t, ledgerFixture)
	if _, err := ApplyLedger(path, log); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := ApplyLedger(path, log)
	if err != nil {
		t.Fatal(err)
	}
	if len(advanced) != 0 {
		t.Errorf("second apply advanced rows again: %v", advanced)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("second apply changed the ledger")
	}
}

func TestReportFollowsReleaseEvidenceFormat(t *testing.T) {
	log := named(t, validP1A(t), "2026-11-20-p1a-pepsipu.json")
	var out strings.Builder
	Report(&out, []*Log{log})
	text := out.String()
	for _, want := range []string{
		"2026-11-20", "MacBook Pro (Apple Silicon)", "macOS 26.1 (25B78)",
		"pepsipu", "23.0.1", "full 4.4.9", "usb-hid", "usb-drd0",
		"helper-dry-run: pass", "retained-rpc-readonly: blocked",
		"2026-11-20-p1a-pepsipu.json",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report missing %q\n%s", want, text)
		}
	}
}

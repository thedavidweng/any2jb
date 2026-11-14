package hwevidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkFixture(t *testing.T, ledger string, logs ...*Log) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parity-ledger.md")
	if err := os.WriteFile(path, []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err := CheckLedger(path, logs)
	if err != nil {
		t.Fatal(err)
	}
	return problems
}

const checkLedgerBase = `# ledger

| ID | Group | Capability / action | Tier | Upstream source | Preconditions | Risk | Disposition | Evidence | Experimental | Evidence link | Tickets | UI keys |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| usb-probe | USB | Probe controllers | P1a | src | HID | input | gated | not verified | false | [not verified](#not-verified) | 09 | - |
`

func TestCheckLedgerClean(t *testing.T) {
	log := named(t, validP1A(t), "run.json")
	ledger := strings.Replace(checkLedgerBase,
		"| usb-probe | USB | Probe controllers | P1a | src | HID | input | gated | not verified | false | [not verified](#not-verified) | 09 | - |",
		"| usb-probe | USB | Probe controllers | P1a | src | HID | input | gated | hardware-verified | false | [hardware evidence](hardware-evidence/run.json) | 09 | - |", 1)
	problems := checkFixture(t, ledger, log)
	for _, problem := range problems {
		if strings.Contains(problem, "usb-probe") {
			t.Errorf("unexpected problem for verified row: %s", problem)
		}
	}
}

func TestCheckLedgerRejectsUnverifiedClaim(t *testing.T) {
	ledger := strings.Replace(checkLedgerBase, "gated | not verified |", "gated | hardware-verified |", 1)
	problems := checkFixture(t, ledger)
	if len(problems) != 1 || !strings.Contains(problems[0], "usb-probe") {
		t.Fatalf("expected one usb-probe problem, got %v", problems)
	}
}

func TestCheckLedgerRejectsWrongLink(t *testing.T) {
	log := named(t, validP1A(t), "run.json")
	ledger := strings.Replace(checkLedgerBase,
		"gated | not verified | false | [not verified](#not-verified) |",
		"gated | hardware-verified | false | [hardware evidence](hardware-evidence/other.json) |", 1)
	problems := checkFixture(t, ledger, log)
	found := false
	for _, problem := range problems {
		if strings.Contains(problem, "usb-probe") && strings.Contains(problem, "other.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a link mismatch problem, got %v", problems)
	}
}

func TestCheckLedgerRejectsUnrecordedEvidence(t *testing.T) {
	log := named(t, validP1A(t), "run.json")
	problems := checkFixture(t, checkLedgerBase, log)
	found := false
	for _, problem := range problems {
		if strings.Contains(problem, "usb-probe") && strings.Contains(problem, "apply") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a drift problem naming the apply command, got %v", problems)
	}
}

package hwevidence

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ApplyLedger advances the parity ledger rows a validated log verifies to
// hardware-verified, linking the log. It returns the advanced row IDs.
// Rows already hardware-verified are left untouched; a row whose evidence is
// blocked needs distribution clearance and fails the apply instead of being
// silently advanced.
func ApplyLedger(ledgerPath string, log *Log) ([]string, error) {
	rows, err := VerifiedRows([]*Log{log})
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return nil, err
	}
	link := "[hardware evidence](hardware-evidence/" + log.Name + ")"
	var advanced []string
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| ID ") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 13 {
			continue
		}
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		if len(rows[cells[0]]) == 0 {
			continue
		}
		switch cells[8] {
		case "blocked":
			return nil, fmt.Errorf("%s is blocked; hardware evidence does not clear a distribution blocker", cells[0])
		case "hardware-verified":
			continue
		}
		cells[8] = "hardware-verified"
		cells[10] = link
		lines[i] = "| " + strings.Join(cells, " | ") + " |"
		advanced = append(advanced, cells[0])
	}
	if len(advanced) > 0 {
		if err := os.WriteFile(ledgerPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			return nil, err
		}
	}
	return advanced, nil
}

// Report prints one SPEC "Release checklist and evidence reporting format"
// hardware bullet per log: platform, host, console system and game versions,
// bootstrap revision, transport, test cases, observed outcome, redacted log
// reference and date.
func Report(w io.Writer, logs []*Log) {
	for _, log := range logs {
		c := log.Combination
		console := "Switch 2"
		if c.ConsoleGeneration != "" {
			console = c.ConsoleGeneration
		}
		game := "SmileBASIC " + c.GameEdition + " " + c.GameVersion
		if log.Route == RouteCivSwitch {
			game = c.GameVersion + ", system language " + c.SystemLanguage
		}
		fmt.Fprintf(w, "- Date: %s. Tester: %s.\n", log.Date, log.Tester)
		fmt.Fprintf(w, "  Platform: %s, macOS %s.\n", c.MacModel, c.MacOSVersion)
		if c.StartupSecurity != "" {
			fmt.Fprintf(w, "  Startup security: %s.\n", c.StartupSecurity)
		}
		if c.HostProfile != "" {
			fmt.Fprintf(w, "  Host: %s. Bootstrap SHA-256: %s.\n", c.HostProfile, c.BootstrapSHA256)
		}
		fmt.Fprintf(w, "  Console: %s system %s; %s.\n", console, c.ConsoleSystem, game)
		fmt.Fprintf(w, "  Transport: %s", c.Transport)
		if c.USBController != "" {
			fmt.Fprintf(w, " (%s)", c.USBController)
		}
		fmt.Fprintf(w, "; cable: %s; LAN: %s.\n", c.Cable, c.LANTopology)
		passed, total := 0, 0
		fmt.Fprint(w, "  Test cases:\n")
		for _, step := range log.Steps {
			total++
			if step.Outcome == OutcomePass {
				passed++
			}
			fmt.Fprintf(w, "  - %s: %s", step.ID, step.Outcome)
			if step.Notes != "" {
				fmt.Fprintf(w, " (%s)", step.Notes)
			}
			fmt.Fprintf(w, " — %s\n", step.Log)
		}
		fmt.Fprintf(w, "  Observed outcome: %d/%d recorded steps pass.\n", passed, total)
		fmt.Fprintf(w, "  Redacted logs: docs/hardware-evidence/%s.\n", log.Name)
	}
}

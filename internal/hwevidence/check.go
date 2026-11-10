package hwevidence

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// CheckLedger compares the parity ledger against the evidence logs and
// returns every inconsistency: a row claiming hardware-verified without a
// matching log, a hardware-verified row linking a log that does not verify
// it, or a row a passing log verifies that the ledger has not advanced.
func CheckLedger(ledgerPath string, logs []*Log) ([]string, error) {
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return nil, err
	}
	type ledgerRow struct {
		evidence string
		link     string
	}
	rows := map[string]ledgerRow{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| ID ") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 13 {
			continue
		}
		rows[strings.TrimSpace(cells[0])] = ledgerRow{
			evidence: strings.TrimSpace(cells[8]),
			link:     strings.TrimSpace(cells[10]),
		}
	}
	verified, err := VerifiedRows(logs)
	if err != nil {
		return nil, err
	}
	var problems []string
	for id, row := range rows {
		if row.evidence != "hardware-verified" {
			continue
		}
		names := verified[id]
		if len(names) == 0 {
			problems = append(problems, fmt.Sprintf("%s claims hardware-verified without a matching evidence log", id))
			continue
		}
		linked := false
		for _, name := range names {
			if strings.Contains(row.link, "hardware-evidence/"+name) {
				linked = true
			}
		}
		if !linked {
			problems = append(problems, fmt.Sprintf("%s links %s but is verified by %s", id, row.link, strings.Join(names, ", ")))
		}
	}
	for _, id := range sortedKeys(verified) {
		row, ok := rows[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s is verified by %s but has no ledger row", id, strings.Join(verified[id], ", ")))
			continue
		}
		if row.evidence != "hardware-verified" {
			problems = append(problems, fmt.Sprintf("%s is verified by %s but the ledger says %s; run: go run ./cmd/evidence apply docs/hardware-evidence/%s", id, strings.Join(verified[id], ", "), row.evidence, verified[id][0]))
		}
	}
	return problems, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

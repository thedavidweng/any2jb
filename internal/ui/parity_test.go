package ui

import (
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/hwevidence"
	"github.com/thedavidweng/any2jb/internal/parity"
)

// TestHardwareEvidenceMatchesLedger keeps hardware claims traceable: every
// ledger row marked hardware-verified needs a matching validated log in
// docs/hardware-evidence, and every log-verified row must be marked.
func TestHardwareEvidenceMatchesLedger(t *testing.T) {
	root := repoRootDir(t)
	logs, err := hwevidence.LoadDir(filepath.Join(root, "docs", "hardware-evidence"))
	if err != nil {
		t.Fatal(err)
	}
	problems, err := hwevidence.CheckLedger(filepath.Join(root, "docs", "parity-ledger.md"), logs)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestParityLedger(t *testing.T) {
	root := repoRootDir(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "parity-ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	ids := map[string]bool{}
	rows := 0
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| ID ") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 13 {
			t.Fatalf("ledger row needs 13 columns: %s", line)
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
			if cells[i] == "" {
				t.Errorf("empty column %d: %s", i+1, line)
			}
		}
		id := cells[0]
		if ids[id] {
			t.Errorf("duplicate ledger ID %q", id)
		}
		ids[id] = true
		for column, allowed := range map[int]string{
			3: "P1a|P1b",
			7: "reused|reimplemented|gated|excluded",
			8: "not verified|unit-tested|integration-tested|hardware-verified|blocked",
			9: "true|false",
		} {
			if !strings.Contains("|"+allowed+"|", "|"+cells[column]+"|") {
				t.Errorf("%s: invalid column %d: %q", id, column+1, cells[column])
			}
		}
		if !strings.Contains(cells[10], "](") {
			t.Errorf("%s: evidence must link a test, log or explicit note", id)
		}
		if cells[8] != "not verified" && cells[8] != "blocked" && strings.Contains(cells[10], "#not-verified") {
			t.Errorf("%s: an unverified note cannot establish test or hardware evidence", id)
		}
		for _, key := range strings.Split(cells[12], ",") {
			keys[strings.TrimSpace(key)] = true
		}
		rows++
	}
	if rows == 0 {
		t.Fatal("no parity rows")
	}
	controls, err := parity.GUIControls(root)
	if err != nil {
		t.Fatal(err)
	}
	for key, position := range controls {
		if key == "" || !keys[key] {
			t.Errorf("%s needs a literal c.Key(\"action-id\") and matching ledger UI key (got %q)", position, key)
		}
	}
}

func TestParityControlIncludesPrimaryButton(t *testing.T) {
	for _, name := range []string{"PrimaryButton", "PrimaryButtonBase"} {
		if !parity.IsParityControl(name) {
			t.Errorf("%s must require a ledger UI key", name)
		}
	}
}

// TestParityControlCoversUnlistedConstructors keeps the rule closed: anything
// that is neither static text, scrolling nor a layout container is a control,
// including constructors this file has never heard of.
func TestParityControlCoversUnlistedConstructors(t *testing.T) {
	for _, name := range []string{"PopoverMenuButton", "Autocomplete", "NumberInput", "RadioGroup", "CheckboxGroup", "DateInput"} {
		if !parity.IsParityControl(name) {
			t.Errorf("%s is an interactive control and must require a ledger UI key", name)
		}
	}
	for _, name := range []string{"Text", "Scroll", "Row", "Column", "View"} {
		if parity.IsParityControl(name) {
			t.Errorf("%s draws no control and must not require a ledger UI key", name)
		}
	}
}

func TestParityControlIncludesRawInputCapture(t *testing.T) {
	expr, err := parser.ParseExpr(`ui.Text(c.Key("capture"), "Type here").Focusable().HandleInput(handler)`)
	if err != nil {
		t.Fatal(err)
	}
	constructor, label := parity.ParityControl(expr.(*ast.CallExpr), "ui")
	if constructor == nil || label != "HandleInput" || constructor.Fun.(*ast.SelectorExpr).Sel.Name != "Text" {
		t.Fatal("raw input capture bypassed the ledger")
	}
}

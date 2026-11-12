package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thedavidweng/any2jb/internal/hwevidence"
)

// TestHardwareEvidenceMatchesLedger keeps hardware claims traceable: every
// ledger row marked hardware-verified needs a matching validated log in
// docs/hardware-evidence, and every log-verified row must be marked.
func TestHardwareEvidenceMatchesLedger(t *testing.T) {
	logs, err := hwevidence.LoadDir("docs/hardware-evidence")
	if err != nil {
		t.Fatal(err)
	}
	problems, err := hwevidence.CheckLedger("docs/parity-ledger.md", logs)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestParityLedger(t *testing.T) {
	data, err := os.ReadFile("docs/parity-ledger.md")
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
	for key, position := range guiControls(t) {
		if key == "" || !keys[key] {
			t.Errorf("%s needs a literal c.Key(\"action-id\") and matching ledger UI key (got %q)", position, key)
		}
	}
}

// guiControls maps each interactive control's literal key to its position;
// a control without a literal key is reported under the empty key.
func guiControls(t *testing.T) map[string]string {
	t.Helper()
	controls := map[string]string{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "build" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		uiName := ""
		for _, imp := range file.Imports {
			if imp.Path.Value == `"github.com/egoist/mygo/ui"` {
				uiName = "ui"
				if imp.Name != nil {
					uiName = imp.Name.Name
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			control, label := parityControl(call, uiName)
			if control == nil {
				return true
			}
			key := ""
			if len(control.Args) > 0 {
				if context, ok := control.Args[0].(*ast.CallExpr); ok {
					if method, ok := context.Fun.(*ast.SelectorExpr); ok && method.Sel.Name == "Key" && len(context.Args) == 1 {
						if literal, ok := context.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
							key, _ = strconv.Unquote(literal.Value)
						}
					}
				}
			}
			controls[key] = fset.Position(call.Pos()).String() + ": " + label
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return controls
}

func parityControl(call *ast.CallExpr, uiName string) (*ast.CallExpr, string) {
	fun := call.Fun
	if indexed, ok := fun.(*ast.IndexExpr); ok {
		fun = indexed.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return nil, ""
	}
	if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == uiName && isParityControl(sel.Sel.Name) {
		return call, sel.Sel.Name
	}
	if sel.Sel.Name != "HandleInput" {
		return nil, ""
	}
	receiver := sel.X
	for {
		constructor, ok := receiver.(*ast.CallExpr)
		if !ok {
			break
		}
		method, ok := constructor.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		if pkg, ok := method.X.(*ast.Ident); ok && pkg.Name == uiName {
			return constructor, "HandleInput"
		}
		receiver = method.X
	}
	return call, "HandleInput"
}

// isParityControl treats every ui constructor as an interactive control except
// static text, scrolling and layout containers, so a control this file has
// never heard of still has to name a ledger key.
func isParityControl(name string) bool {
	switch strings.TrimSuffix(name, "Base") {
	case "Text", "Scroll", "Row", "Column", "View":
		return false
	}
	return true
}

func TestParityControlIncludesPrimaryButton(t *testing.T) {
	for _, name := range []string{"PrimaryButton", "PrimaryButtonBase"} {
		if !isParityControl(name) {
			t.Errorf("%s must require a ledger UI key", name)
		}
	}
}

// TestParityControlCoversUnlistedConstructors keeps the rule closed: anything
// that is neither static text, scrolling nor a layout container is a control,
// including constructors this file has never heard of.
func TestParityControlCoversUnlistedConstructors(t *testing.T) {
	for _, name := range []string{"PopoverMenuButton", "Autocomplete", "NumberInput", "RadioGroup", "CheckboxGroup", "DateInput"} {
		if !isParityControl(name) {
			t.Errorf("%s is an interactive control and must require a ledger UI key", name)
		}
	}
	for _, name := range []string{"Text", "Scroll", "Row", "Column", "View"} {
		if isParityControl(name) {
			t.Errorf("%s draws no control and must not require a ledger UI key", name)
		}
	}
}

func TestParityControlIncludesRawInputCapture(t *testing.T) {
	expr, err := parser.ParseExpr(`ui.Text(c.Key("capture"), "Type here").Focusable().HandleInput(handler)`)
	if err != nil {
		t.Fatal(err)
	}
	constructor, label := parityControl(expr.(*ast.CallExpr), "ui")
	if constructor == nil || label != "HandleInput" || constructor.Fun.(*ast.SelectorExpr).Sel.Name != "Text" {
		t.Fatal("raw input capture bypassed the ledger")
	}
}

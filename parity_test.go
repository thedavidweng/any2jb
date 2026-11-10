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
)

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
	err = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
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
			fun := call.Fun
			if indexed, ok := fun.(*ast.IndexExpr); ok {
				fun = indexed.X
			}
			sel, ok := fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != uiName || !isParityControl(sel.Sel.Name) {
				return true
			}
			key := ""
			if len(call.Args) > 0 {
				if context, ok := call.Args[0].(*ast.CallExpr); ok {
					if method, ok := context.Fun.(*ast.SelectorExpr); ok && method.Sel.Name == "Key" && len(context.Args) == 1 {
						if literal, ok := context.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
							key, _ = strconv.Unquote(literal.Value)
						}
					}
				}
			}
			if key == "" || !keys[key] {
				t.Errorf("%s: %s needs a literal c.Key(\"action-id\") and matching ledger UI key (got %q)", fset.Position(call.Pos()), sel.Sel.Name, key)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isParityControl(name string) bool {
	switch strings.TrimSuffix(name, "Base") {
	case "Button", "Checkbox", "CheckboxState", "Combobox", "MenuButton", "Radio", "Segmented", "Select", "SelectBaseOpen", "Slider", "Switch", "TextArea", "TextAreaBuffer", "TextInput", "TextInputBuffer", "Toggle":
		return true
	}
	return false
}

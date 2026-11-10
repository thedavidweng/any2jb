package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func TestCatalogCompleteness(t *testing.T) {
	read := func(path string) map[string]json.RawMessage {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]json.RawMessage
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		return messages
	}
	en, zh := read("locales/en.json"), read("locales/zh-CN.json")
	placeholders := regexp.MustCompile(`\{[a-zA-Z][a-zA-Z0-9]*\}`)
	for key, source := range en {
		if key[0] == '@' {
			continue
		}
		var english, chinese string
		if err := json.Unmarshal(source, &english); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(zh[key], &chinese); err != nil || chinese == "" {
			t.Errorf("missing zh-CN translation: %s", key)
			continue
		}
		sorted := func(values []string) []string { out := append([]string{}, values...); sort.Strings(out); return out }
		if !reflect.DeepEqual(sorted(placeholders.FindAllString(english, -1)), sorted(placeholders.FindAllString(chinese, -1))) {
			t.Errorf("%s: placeholder mismatch", key)
		}
	}
	for key := range zh {
		if key[0] != '@' && en[key] == nil {
			t.Errorf("unknown zh-CN key: %s", key)
		}
	}
}

// TestNoHardcodedUIStrings keeps every widget label on the catalog: a string
// literal passed directly to a ui widget constructor, Label or Placeholder is
// a violation. Keys (c.Key) and computed text are fine.
func TestNoHardcodedUIStrings(t *testing.T) {
	widgets := map[string]bool{
		"Text": true, "Button": true, "PrimaryButton": true, "Checkbox": true,
		"TextInput": true, "TextArea": true, "Select": true, "Combobox": true,
	}
	check := func(fset *token.FileSet, call *ast.CallExpr, name string, arg ast.Expr) {
		literal, ok := arg.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return
		}
		text, _ := strconv.Unquote(literal.Value)
		if strings.IndexFunc(text, unicode.IsLetter) >= 0 {
			t.Errorf("%s: hardcoded UI string %s in %s", fset.Position(call.Pos()), literal.Value, name)
		}
	}
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
		if file.Name.Name != "main" {
			return nil
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
		if uiName == "" {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == uiName && widgets[sel.Sel.Name] {
				for _, arg := range call.Args {
					check(fset, call, sel.Sel.Name, arg)
				}
				return true
			}
			if (sel.Sel.Name == "Label" || sel.Sel.Name == "Placeholder") && len(call.Args) == 1 {
				check(fset, call, sel.Sel.Name, call.Args[0])
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

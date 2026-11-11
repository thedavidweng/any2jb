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

// adapterMessage is one literal an adapter can hand a view, with the file it
// comes from.
type adapterMessage struct{ where, text string }

// availabilityFunctions return text the views render through localize.
var availabilityFunctions = map[string]bool{
	"Availability": true, "Unavailable": true,
	"CivSwitchUnavailable": true, "civSwitchUnavailable": true,
}

// formatVerb matches the verbs fmt leaves for live data.
var formatVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

// adapterMessages collects the literals adapters can hand to localize: a
// Check's message or next action, and the errors that decide whether a driver
// or CivSwitch action is available. The structured fake is skipped because no
// window renders it.
func adapterMessages(t *testing.T) []adapterMessage {
	t.Helper()
	var messages []adapterMessage
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "build" || d.Name() == "vendor" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || path == filepath.Join("session", "fake.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		add := func(node ast.Node, expr ast.Expr) {
			literal, ok := expr.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return
			}
			text, _ := strconv.Unquote(literal.Value)
			messages = append(messages, adapterMessage{where: fset.Position(node.Pos()).String(), text: text})
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				for _, element := range node.Elts {
					field, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := field.Key.(*ast.Ident)
					if ok && (key.Name == "Message" || key.Name == "NextAction") {
						add(field, field.Value)
					}
				}
			case *ast.FuncDecl:
				if node.Body == nil || !availabilityFunctions[node.Name.Name] {
					return true
				}
				ast.Inspect(node.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok || len(call.Args) == 0 {
						return true
					}
					method, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || (method.Sel.Name != "New" && method.Sel.Name != "Errorf") {
						return true
					}
					add(call, call.Args[0])
					return true
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

// segmentsCover reports whether every fixed piece of text has a segment. A
// literal with live data is localized piece by piece because localize matches
// whole strings.
func segmentsCover(segments map[string]bool, text string) bool {
	if segments[text] {
		return true
	}
	pieces := formatVerb.Split(text, -1)
	if len(pieces) < 2 {
		return false
	}
	for _, piece := range pieces {
		if piece != "" && !segments[piece] {
			return false
		}
	}
	return true
}

// TestAdapterMessagesAreLocalized keeps the exact-match localization complete:
// adapter text the views pass through localize must be built from catalog
// segments, so it cannot fall through in English.
func TestAdapterMessagesAreLocalized(t *testing.T) {
	segments := map[string]bool{}
	for _, segment := range dynamicSegments {
		segments[segment.source] = true
	}
	messages := adapterMessages(t)
	if len(messages) < 8 {
		t.Fatalf("collected only %d adapter messages; the walk lost the adapters", len(messages))
	}
	for _, message := range messages {
		if !segmentsCover(segments, message.text) {
			t.Errorf("%s: %q has no dynamic segment", message.where, message.text)
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

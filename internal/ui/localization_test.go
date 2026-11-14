package ui

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

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/plugins"
	"github.com/thedavidweng/any2jb/locales"
)

func repoRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above working directory")
		}
		dir = parent
	}
}

func loadCatalogStrings(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if k != "" && k[0] == '@' {
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			t.Fatalf("%s: key %s: %v", path, k, err)
		}
		out[k] = s
	}
	return out
}

func TestCatalogCompleteness(t *testing.T) {
	root := repoRootDir(t)
	en := loadCatalogStrings(t, filepath.Join(root, "locales", "en.json"))
	placeholders := regexp.MustCompile(`\{[a-zA-Z][a-zA-Z0-9]*\}`)
	sorted := func(values []string) []string {
		out := append([]string{}, values...)
		sort.Strings(out)
		return out
	}

	if len(locales.Switch2Languages) != 18 {
		t.Fatalf("Switch2Languages has %d locales, want 18", len(locales.Switch2Languages))
	}

	for _, lang := range locales.Switch2Languages {
		if lang.Tag == "en" {
			continue
		}
		target := loadCatalogStrings(t, filepath.Join(root, "locales", lang.Tag+".json"))
		for key, english := range en {
			translated, ok := target[key]
			if !ok || strings.TrimSpace(translated) == "" {
				t.Errorf("[%s] missing translation: %s", lang.Tag, key)
				continue
			}
			if !reflect.DeepEqual(sorted(placeholders.FindAllString(english, -1)), sorted(placeholders.FindAllString(translated, -1))) {
				t.Errorf("[%s] %s: placeholder mismatch", lang.Tag, key)
			}
		}
		for key := range target {
			if _, ok := en[key]; !ok {
				t.Errorf("[%s] unknown key: %s", lang.Tag, key)
			}
		}
	}
}

func TestPluginSpecsAreLocalized(t *testing.T) {
	for _, spec := range plugins.All() {
		for _, lang := range locales.Switch2Languages {
			title := locales.Catalog.Format(lang.Tag, spec.TitleKey, nil)
			if title == "" || title == spec.TitleKey {
				t.Errorf("[%s] plugin %s TitleKey %q missing from catalog", lang.Tag, spec.ID, spec.TitleKey)
			}
			desc := locales.Catalog.Format(lang.Tag, spec.DescKey, nil)
			if desc == "" || desc == spec.DescKey {
				t.Errorf("[%s] plugin %s DescKey %q missing from catalog", lang.Tag, spec.ID, spec.DescKey)
			}
		}
	}
}

func TestManifestDistributionsMatchCatalog(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	check := func(id, reason string) {
		key, ok := locales.DistributionKeys[id]
		if !ok {
			t.Errorf("manifest entry %q (%q) has no key in locales.DistributionKeys", id, reason)
			return
		}
		text := locales.DistributionReason(id, reason)
		if got := locales.Key(key).English(); got != reason {
			t.Errorf("manifest entry %q (%s): catalog en %q != manifest reason %q", id, key, got, reason)
		}
		if zh := text.InLocale("zh-CN"); zh == "" || zh == reason {
			t.Errorf("manifest entry %q (%s) is not translated in zh-CN: %q", id, key, zh)
		}
	}
	for _, p := range m.Profiles {
		check(p.ID, p.Distribution.Reason)
	}
	for _, c := range m.Components {
		if (c.Source != nil && c.ID != "libusbd-source") || c.Launcher != nil {
			check(c.ID, c.Distribution.Reason)
		}
	}
	for _, id := range []string{"browser-research", "civswitch-payload", "usbgadget-binary", "usbgadget-info", "usbgadget-checksums", "driver-installer"} {
		d, err := m.DistributionFor(id)
		if err != nil {
			t.Fatalf("DistributionFor(%q): %v", id, err)
		}
		check(id, d.Reason)
	}
	for id := range locales.DistributionKeys {
		d, err := m.DistributionFor(id)
		if err != nil {
			t.Errorf("locales.DistributionKeys[%q] does not exist in manifest: %v", id, err)
			continue
		}
		check(id, d.Reason)
	}
}

// TestAdapterMessagesAreLocalized verifies that every structured catalog key
// referenced by production code exists and is translated, that Check literals
// carry catalog-backed MessageText/NextActionText instead of raw English
// literals, and that adapter availability checks do not construct raw English
// errors directly.
func TestAdapterMessagesAreLocalized(t *testing.T) {
	root := repoRootDir(t)
	en := loadCatalogStrings(t, filepath.Join(root, "locales", "en.json"))
	zh := loadCatalogStrings(t, filepath.Join(root, "locales", "zh-CN.json"))

	availabilityFunctions := map[string]bool{
		"Availability": true, "UnavailableText": true, "CivSwitchAvailability": true,
	}
	keyCalls := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() && rel != "." {
			base := d.Name()
			if strings.HasPrefix(base, ".") || base == "build" || base == "dist" || base == "node_modules" || base == "vendor" || base == "testdata" || base == "sessiontest" ||
				rel == filepath.Join("cmd", "check") || rel == filepath.Join("cmd", "check-package") ||
				rel == filepath.Join("cmd", "evidence") || rel == filepath.Join("cmd", "prepare-runtime") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		localesAlias := ""
		for _, imp := range file.Imports {
			if strings.HasSuffix(strings.Trim(imp.Path.Value, `"`), "/locales") {
				localesAlias = "locales"
				if imp.Name != nil {
					localesAlias = imp.Name.Name
				}
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				if localesAlias != "" {
					if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == localesAlias &&
							(sel.Sel.Name == "Key" || sel.Sel.Name == "Format" || sel.Sel.Name == "WrapError") && len(node.Args) > 0 {
							if lit, ok := node.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								key, _ := strconv.Unquote(lit.Value)
								keyCalls++
								if _, ok := en[key]; !ok {
									t.Errorf("%s: unknown catalog key %q", fset.Position(lit.Pos()), key)
								} else if zh[key] == "" || zh[key] == en[key] {
									t.Errorf("%s: untranslated catalog key %q", fset.Position(lit.Pos()), key)
								}
							}
						}
					}
				}
			case *ast.CompositeLit:
				isCheck := false
				switch typ := node.Type.(type) {
				case *ast.Ident:
					isCheck = typ.Name == "Check"
				case *ast.SelectorExpr:
					isCheck = typ.Sel.Name == "Check"
				}
				if !isCheck {
					return true
				}
				fields := map[string]ast.Expr{}
				for _, element := range node.Elts {
					if kv, ok := element.(*ast.KeyValueExpr); ok {
						if ident, ok := kv.Key.(*ast.Ident); ok {
							fields[ident.Name] = kv.Value
						}
					}
				}
				for _, pair := range [][2]string{{"Message", "MessageText"}, {"NextAction", "NextActionText"}} {
					val, has := fields[pair[0]]
					if !has {
						continue
					}
					if lit, ok := val.(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value != `""` {
						t.Errorf("%s: Check.%s carries hardcoded literal %s instead of catalog Text", fset.Position(val.Pos()), pair[0], lit.Value)
					} else if _, hasText := fields[pair[1]]; !hasText {
						t.Errorf("%s: Check.%s set without %s", fset.Position(val.Pos()), pair[0], pair[1])
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
					if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						t.Errorf("%s: %s constructs raw error literal %s instead of catalog Text", fset.Position(call.Pos()), node.Name.Name, lit.Value)
					}
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
	if keyCalls < 30 {
		t.Fatalf("inspected only %d structured catalog key calls; walk missed adapter packages", keyCalls)
	}
}

// TestNoHardcodedUIStrings keeps every widget label on the catalog: a string
// literal passed directly to a ui widget constructor, Label or Placeholder is
// a violation. Keys (c.Key) and computed text are fine.
func TestNoHardcodedUIStrings(t *testing.T) {
	root := repoRootDir(t)
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
	uiFiles := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "build" || d.Name() == "dist" || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == "testdata") {
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
		if uiName == "" {
			return nil
		}
		uiFiles++
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
	if uiFiles == 0 {
		t.Fatal("TestNoHardcodedUIStrings did not inspect any UI source files")
	}
}

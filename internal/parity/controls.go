package parity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// GUIControls maps each interactive UI control's literal key under repoRoot to
// its source position; a control without a literal key is reported under "".
func GUIControls(repoRoot string) (map[string]string, error) {
	controls := map[string]string{}
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != repoRoot && (strings.HasPrefix(d.Name(), ".") || d.Name() == "build" || d.Name() == "vendor") {
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
			control, label := ParityControl(call, uiName)
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
	return controls, err
}

// ParityControl returns the underlying widget constructor call and label when
// call invokes a parity-tracked UI control or raw HandleInput receiver.
func ParityControl(call *ast.CallExpr, uiName string) (*ast.CallExpr, string) {
	fun := call.Fun
	if indexed, ok := fun.(*ast.IndexExpr); ok {
		fun = indexed.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return nil, ""
	}
	if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == uiName && IsParityControl(sel.Sel.Name) {
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

// IsParityControl treats every ui constructor as an interactive control except
// static text, scrolling, and layout containers.
func IsParityControl(name string) bool {
	switch strings.TrimSuffix(name, "Base") {
	case "Text", "Scroll", "Row", "Column", "View":
		return false
	}
	return true
}

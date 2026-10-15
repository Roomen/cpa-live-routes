package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFileIO keeps request data in memory: no file, temp file, log or stdout
// output may appear in the shipped sources.
func TestNoFileIO(t *testing.T) {
	banned := map[string]bool{
		"os.Create": true, "os.OpenFile": true, "os.WriteFile": true, "os.MkdirTemp": true, "os.CreateTemp": true,
		"os.Stdout": true, "os.Stderr": true,
		"fmt.Print": true, "fmt.Printf": true, "fmt.Println": true, "fmt.Fprint": true, "fmt.Fprintf": true, "fmt.Fprintln": true,
	}
	bannedImports := map[string]bool{"io/ioutil": true, "log": true, "log/slog": true}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(path)
		if d.IsDir() {
			switch slash {
			case ".git", "tools", "scripts", "web/test", "prototype", "bin", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if p := strings.Trim(imp.Path.Value, `"`); bannedImports[p] {
				t.Errorf("%s imports %s", path, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && banned[id.Name+"."+sel.Sel.Name] {
					t.Errorf("%s: %s.%s", fset.Position(sel.Pos()), id.Name, sel.Sel.Name)
				}
				if sel.Sel.Name == "WriteFile" || sel.Sel.Name == "CreateTemp" || sel.Sel.Name == "MkdirTemp" {
					t.Errorf("%s: %s", fset.Position(sel.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

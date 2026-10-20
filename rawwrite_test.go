package utils

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// rawWriteAllowed names the library code that may write past the logger, by
// "file:function", each with the reason it is not a log event. Everything
// else writes through Log, so a JSON stream never meets a line that is not an
// event.
var rawWriteAllowed = map[string]string{
	"logger.go:out":                 "the sink: its writer is stderr, the one place events are written",
	"logger.go:stderrTTY":           "the sink chooses human or json by whether stderr is a terminal",
	"kev.go:Dump":                   "data on stdout: Dump prints the environment as namespace:KEY=value, the output it is called for",
	"cli/live.go:Live":              "a live region is terminal UI on stderr, not an event; log events print above it through LogThrough",
	"internal/ansi/ansi.go:Enabled": "the colour decision reads whether stdout and stderr are terminals; nothing is written",
}

// TestNoRawWrites refuses library code that prints by itself: fmt.Print*,
// the standard log package, the print/println builtins, or os.Stdout and
// os.Stderr used directly (fmt.Fprint*(os.Stderr, …), os.Stderr.Write, …).
// cmd/ tools, tests/ and _test.go files are programs, not library, and are
// not scanned.
func TestNoRawWrites(t *testing.T) {
	fset := token.NewFileSet()
	used := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (path == "cmd" || path == "tests") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		imports := map[string]string{} // local name -> import path
		for _, spec := range file.Imports {
			p, _ := strconv.Unquote(spec.Path.Value)
			name := p[strings.LastIndex(p, "/")+1:]
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = p
		}
		for _, decl := range file.Decls {
			owner := path + ":"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				owner += fn.Name.Name
			} else if gen, ok := decl.(*ast.GenDecl); ok && len(gen.Specs) == 1 {
				if vs, ok := gen.Specs[0].(*ast.ValueSpec); ok && len(vs.Names) == 1 {
					owner += vs.Names[0].Name
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				what := rawWrite(n, imports)
				if what == "" {
					return true
				}
				if _, ok := rawWriteAllowed[owner]; ok {
					used[owner] = true
					return true
				}
				t.Errorf("%s: %s in %s writes past the logger: use Log.Scope(…) with F fields, or allowlist it in rawWriteAllowed with a reason", fset.Position(n.Pos()), what, owner)
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for owner := range rawWriteAllowed {
		if !used[owner] {
			t.Errorf("rawWriteAllowed[%q] matches no raw write: delete the entry", owner)
		}
	}
}

// rawWrite names what n is when it writes past the logger, "" otherwise.
func rawWrite(n ast.Node, imports map[string]string) string {
	switch x := n.(type) {
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") && id.Obj == nil {
			return id.Name
		}
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return ""
		}
		switch imports[pkg.Name] {
		case "fmt":
			if strings.HasPrefix(sel.Sel.Name, "Print") {
				return "fmt." + sel.Sel.Name
			}
		case "log":
			return "log." + sel.Sel.Name
		}
	case *ast.SelectorExpr:
		if pkg, ok := x.X.(*ast.Ident); ok && imports[pkg.Name] == "os" && (x.Sel.Name == "Stdout" || x.Sel.Name == "Stderr") {
			return "os." + x.Sel.Name
		}
	}
	return ""
}

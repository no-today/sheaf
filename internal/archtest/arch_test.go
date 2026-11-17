// Package archtest checks the module boundaries; it has no code of its own.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// allowed is the dependency graph between internal packages (production code only).
// A new edge fails this test: add it here only if it keeps dependencies pointing down.
var allowed = map[string][]string{
	"app":      {"appdir", "native", "native/diffwin", "native/settingswin", "native/theme", "session", "watch"},
	"appdir":   {},
	"cli":      {"appdir", "session"},
	"discover": {"gitcli"},
	"engine":   {"index", "query"},
	"gitcli":   {},
	"gitops":   {"gitcli"},
	"gitview":  {"gitcli", "index"},
	"index":    {"discover", "gitcli"},
	// The native UI talks to session as app does, never to git. The workspace window (native) registers the views; the
	// packages below it are its parts, which know nothing of the views. Any other package under native/ is a page (a view
	// or a window): it may use only pageDeps, so pages never import each other (TestDependencies).
	"native":               {"native/i18n", "native/shell", "native/theme", "native/track", "native/view", "native/widgets", "session"},
	"native/diffview":      {"native/i18n", "native/theme", "native/widgets", "session"},
	"native/i18n":          {},
	"native/shell":         {"native/i18n", "native/theme", "native/widgets"},
	"native/theme":         {},
	"native/view":          {"native/shell", "native/widgets", "query", "session"},
	"native/view/viewtest": {"native/shell", "native/theme", "native/view", "native/widgets", "session", "testutil"},
	"native/widgets":       {"native/i18n", "native/theme"},
	"query":                {},
	"session":              {"discover", "engine", "gitcli", "gitops", "gitview", "index", "query", "watch"},
	"testutil":             {},
	"watch":                {"discover"},
}

// pageDeps is what a page of the native UI (a package under native/ that allowed does not list) may import.
var pageDeps = []string{"native/diffview", "native/i18n", "native/shell", "native/theme", "native/view", "native/widgets", "query", "session"}

// Packages that may import os/exec; only gitcli may start git or glab.
var execAllowed = []string{"gitcli", "app", "testutil"}

type file struct {
	pkg  string
	path string
	test bool
	ast  *ast.File
	fset *token.FileSet
}

func parseAll(t *testing.T) []file {
	t.Helper()
	var out []file
	// Nested packages (native/shell) count under their path below internal.
	err := filepath.WalkDir("..", func(dir string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || dir == ".." {
			return err
		}
		pkg := filepath.ToSlash(strings.TrimPrefix(dir, "../"))
		if pkg == "archtest" || strings.Contains(pkg, "testdata") {
			return filepath.SkipDir
		}
		paths, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, p := range paths {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			out = append(out, file{pkg, p, strings.HasSuffix(p, "_test.go"), f, fset})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDependencies(t *testing.T) {
	got := map[string][]string{}
	for _, f := range parseAll(t) {
		if _, ok := got[f.pkg]; !ok {
			got[f.pkg] = []string{}
		}
		if f.test {
			continue
		}
		for _, im := range f.ast.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if dep, ok := strings.CutPrefix(p, "sheaf/internal/"); ok && !slices.Contains(got[f.pkg], dep) {
				got[f.pkg] = append(got[f.pkg], dep)
			}
		}
	}
	for k := range got {
		slices.Sort(got[k])
		if _, listed := allowed[k]; listed || !strings.HasPrefix(k, "native/") {
			continue
		}
		for _, dep := range got[k] {
			if !slices.Contains(pageDeps, dep) {
				t.Errorf("native page %s imports %s; a page may import only %v", k, dep, pageDeps)
			}
		}
		delete(got, k)
	}
	if !reflect.DeepEqual(got, allowed) {
		t.Errorf("internal dependency graph changed:\n got  %v\n want %v", got, allowed)
	}
}

func TestOnlyGitcliRunsGit(t *testing.T) {
	for _, f := range parseAll(t) {
		if f.test {
			continue
		}
		if f.pkg == "gitcli" {
			gitcliStartsOnlyGitAndGlab(t, f)
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "gitcli" && (sel.Sel.Name == "GlabPath" || sel.Sel.Name == "GhPath") {
					t.Errorf("%s uses gitcli.%s; CLI tools run only in gitcli", f.fset.Position(sel.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
		for _, im := range f.ast.Imports {
			if im.Path.Value == `"os/exec"` && !slices.Contains(execAllowed, f.pkg) {
				t.Errorf("%s imports os/exec", f.path)
			}
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
				return true
			}
			name := call.Args[0]
			if sel.Sel.Name == "CommandContext" && len(call.Args) > 1 {
				name = call.Args[1]
			}
			if lit, ok := name.(*ast.BasicLit); ok && lit.Value == `"git"` && f.pkg != "testutil" {
				t.Errorf("%s starts git; use gitcli", f.fset.Position(call.Pos()))
			}
			if lit, ok := name.(*ast.BasicLit); ok && (lit.Value == `"gh"` || strings.Contains(lit.Value, "glab")) {
				t.Errorf("%s starts glab; use gitcli", f.fset.Position(call.Pos()))
			}
			return true
		})
	}
}

// gitcliStartsOnlyGitAndGlab: inside gitcli, a command is the literal "git", or glab started from glab.go (its path is found
// at run time, so it is not a literal), where the glab whitelist and recording are.
func gitcliStartsOnlyGitAndGlab(t *testing.T, f file) {
	ast.Inspect(f.ast, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "exec" {
			return true
		}
		i := 0
		if sel.Sel.Name == "CommandContext" {
			i = 1
		}
		if i >= len(call.Args) {
			return true
		}
		if lit, ok := call.Args[i].(*ast.BasicLit); ok && lit.Value == `"git"` {
			return true
		}
		if base := filepath.Base(f.path); base != "glab.go" && base != "gh.go" {
			t.Errorf("%s starts something other than git; only glab.go and gh.go may start their CLI tools", f.fset.Position(call.Pos()))
		}
		return true
	})
}

// session and app use gitcli's types only; they read git through gitview or discover.
func TestSessionDoesNotCallGitcli(t *testing.T) {
	for _, f := range parseAll(t) {
		if f.test || (f.pkg != "session" && f.pkg != "app") {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "gitcli" {
						t.Errorf("%s calls gitcli.%s", f.fset.Position(call.Pos()), sel.Sel.Name)
					}
				}
			}
			return true
		})
	}
}

// Writes are fixed command lines in gitcli (gitcli.Write is called nowhere else), so the whitelist and the functions next to it
// are every change Sheaf can make to a repository.
func TestWritesOnlyInGitcli(t *testing.T) {
	for _, f := range parseAll(t) {
		if f.pkg == "gitcli" {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "gitcli" && sel.Sel.Name == "Write" {
					t.Errorf("%s uses gitcli.Write; add a function to gitcli instead", f.fset.Position(sel.Pos()))
				}
			}
			return true
		})
	}
}

package i18n

import (
	"fmt"
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

func TestLanguageFollowsTheLocale(t *testing.T) {
	for locale, want := range map[string]string{"zh-Hans": "提交记录", "zh-Hant-TW": "提交记录", "ZH": "提交记录", "en-US": "Commits", "ja": "Commits", "": "Commits"} {
		if got := Common.For(locale).Log; got != want {
			t.Errorf("%q: %q, want %q", locale, got, want)
		}
	}
}

func TestPlural(t *testing.T) {
	for _, tc := range []struct {
		text string
		n    int
		want string
	}{
		{"{n} repo|{n} repos", 0, "0 repos"},
		{"{n} repo|{n} repos", 1, "1 repo"},
		{"{n} repo|{n} repos", 2, "2 repos"},
		{"{n} repo|{n} repos", 1000, "1000 repos"},
		{"{n} repo|{n} repos", -1, "-1 repos"},
		{"{n} 个仓库", 0, "0 个仓库"},
		{"{n} 个仓库", 1, "1 个仓库"},
		{"{n} 个仓库", 2, "2 个仓库"},
		{"{n} uncommitted", 1, "1 uncommitted"},
		{"{n} uncommitted", 2, "2 uncommitted"},
		{"{wt}: {n} change|{wt}: {n} changes", 2, "{wt}: 2 changes"},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.text, tc.n), func(t *testing.T) {
			if got := Plural(tc.text, tc.n); got != tc.want {
				t.Errorf("Plural = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPluralRendersEveryTable(t *testing.T) {
	fset := token.NewFileSet()
	templates := 0
	err := fs.WalkDir(os.DirFS(".."), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, filepath.Join("..", path), nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			table := false
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				pkg, ok := fn.X.(*ast.Ident)
				table = ok && pkg.Name == "i18n" && fn.Sel.Name == "New"
			case *ast.Ident:
				table = filepath.Dir(path) == "i18n" && fn.Name == "New"
			}
			if !table {
				return true
			}
			for _, arg := range call.Args {
				words, ok := arg.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, element := range words.Elts {
					field, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					literal, ok := field.Value.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					text, err := strconv.Unquote(literal.Value)
					if err != nil || !strings.Contains(text, "|") || !strings.Contains(text, "{n}") {
						continue
					}
					templates++
					t.Run(fset.Position(literal.Pos()).String(), func(t *testing.T) {
						for _, n := range []int{0, 1, 2, 1000} {
							if got := Plural(text, n); strings.ContainsAny(got, "|") || strings.Contains(got, "{n}") {
								t.Errorf("count %d left a raw plural template: %q", n, got)
							}
						}
					})
				}
			}
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if templates == 0 {
		t.Fatal("no plural templates checked")
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 183012: "183,012", 1234567: "1,234,567", -4500: "-4,500"} {
		if got := Thousands(n); got != want {
			t.Errorf("%d: %q, want %q", n, got, want)
		}
	}
}

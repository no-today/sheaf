package archtest

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// textSinks are the calls of the native UI whose string arguments the user reads (or hears): ui's text and buttons, an
// element's label, tooltip and placeholder, menu items, toasts, and the widgets'.
var textSinks = map[string]bool{
	"Text": true, "Button": true, "PrimaryButton": true, "DangerButton": true, "FlatButton": true, "IconButton": true,
	"Badge": true, "Link": true, "Label": true, "Tooltip": true, "Placeholder": true, "Description": true, "Item": true,
	"Submenu": true, "Toast": true, "ToastAction": true, "Show": true, "Error": true, "Undo": true, "Window": true,
	"Segmented": true, "Select": true, "Checkbox": true, "Switch": true, "Radio": true,
}

// textFields are the fields of composite literals (shell.Pane, widgets.Confirm…) that the user reads.
var textFields = map[string]bool{"Title": true, "Label": true, "Tip": true, "Placeholder": true, "Detail": true, "OK": true, "Cancel": true}

// wordy: s has a letter (Latin, CJK, any script); "›", "+%d" and "%d / %d" are not words.
func wordy(s string) bool {
	for _, r := range verbRE.ReplaceAllString(s, "") {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// nativeFiles are the production files of the native UI but its words (native/i18n) and its colours (native/theme).
func nativeFiles(t *testing.T) []file {
	var out []file
	for _, f := range parseAll(t) {
		if f.test || (f.pkg != "native" && !strings.HasPrefix(f.pkg, "native/")) || f.pkg == "native/i18n" || f.pkg == "native/theme" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// TestNativeHygiene keeps native UI words in i18n tables (TestNativeWords) and colours in native/theme.
func TestNativeHygiene(t *testing.T) {
	for _, f := range nativeFiles(t) {
		literal := func(e ast.Expr) (string, bool) {
			if call, ok := e.(*ast.CallExpr); ok && len(call.Args) > 0 { // fmt.Sprintf("… %d", n)
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "Sprint") {
					e = call.Args[0]
				}
			}
			lit, ok := e.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return "", false
			}
			s, err := strconv.Unquote(lit.Value)
			return s, err == nil && wordy(s)
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				name := ""
				switch fn := n.Fun.(type) {
				case *ast.SelectorExpr:
					name = fn.Sel.Name
					if x, ok := fn.X.(*ast.Ident); ok && x.Name == "ui" && (name == "Hex" || name == "RGBA" || name == "RGB") {
						t.Errorf("%s: a colour outside native/theme", f.fset.Position(n.Pos()))
					}
				case *ast.Ident:
					name = fn.Name
				}
				if !textSinks[name] {
					return true
				}
				for _, a := range n.Args {
					if s, ok := literal(a); ok {
						t.Errorf("%s: %s(%q): words go in the package's i18n table", f.fset.Position(a.Pos()), name, s)
					}
				}
			case *ast.KeyValueExpr:
				if k, ok := n.Key.(*ast.Ident); ok && textFields[k.Name] {
					if s, ok := literal(n.Value); ok {
						t.Errorf("%s: %s: %q: words go in the package's i18n table", f.fset.Position(n.Pos()), k.Name, s)
					}
				}
			}
			return true
		})
	}
}

var verbRE = regexp.MustCompile(`%[-+# 0]*[0-9]*(\.[0-9]+)?[a-zA-Z%]`)

// TestNativeWords checks every i18n table of the native UI (i18n.New(words{…zh…}, words{…en…})): both literals set every
// field of the words type, no text is empty, and a Chinese and an English text have the same fmt verbs in the same order.
func TestNativeWords(t *testing.T) {
	all := parseAll(t)
	tables := 0
	for _, f := range all {
		if f.test || (f.pkg != "native" && !strings.HasPrefix(f.pkg, "native/")) {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 || !isI18nNew(f, call.Fun) {
				return true
			}
			tables++
			at := f.fset.Position(call.Pos())
			zh, ok1 := call.Args[0].(*ast.CompositeLit)
			en, ok2 := call.Args[1].(*ast.CompositeLit)
			if !ok1 || !ok2 {
				t.Errorf("%s: i18n.New takes two composite literals", at)
				return true
			}
			typ, _ := zh.Type.(*ast.Ident)
			if typ == nil {
				t.Errorf("%s: the words type must be a named struct of the package", at)
				return true
			}
			fields := structFields(all, f.pkg, typ.Name)
			if fields == nil {
				t.Errorf("%s: no struct type %s in %s", at, typ.Name, f.pkg)
				return true
			}
			zhv, env := keyed(t, f, zh), keyed(t, f, en)
			for _, name := range fields {
				a, okA := zhv[name]
				b, okB := env[name]
				if !okA || !okB {
					t.Errorf("%s: %s.%s is missing in the Chinese or the English words", at, typ.Name, name)
					continue
				}
				sa, isA := strLit(a)
				sb, isB := strLit(b)
				if isA != isB {
					continue
				}
				if isA && (sa == "" || sb == "") {
					t.Errorf("%s: %s.%s is empty", at, typ.Name, name)
				}
				if va, vb := verbRE.FindAllString(sa, -1), verbRE.FindAllString(sb, -1); isA && !slices.Equal(va, vb) {
					t.Errorf("%s: %s.%s: verbs %v in Chinese, %v in English", at, typ.Name, name, va, vb)
				}
			}
			return true
		})
	}
	if tables == 0 {
		t.Fatal("no i18n tables found")
	}
}

// isI18nNew: fn is native/i18n's New (i18n.New outside the package, New inside it).
func isI18nNew(f file, fn ast.Expr) bool {
	switch fn := fn.(type) {
	case *ast.SelectorExpr:
		x, ok := fn.X.(*ast.Ident)
		if !ok || fn.Sel.Name != "New" {
			return false
		}
		for _, im := range f.ast.Imports {
			if im.Path.Value != `"sheaf/internal/native/i18n"` {
				continue
			}
			name := "i18n"
			if im.Name != nil {
				name = im.Name.Name
			}
			return x.Name == name
		}
	case *ast.Ident:
		return f.pkg == "native/i18n" && fn.Name == "New"
	}
	return false
}

// structFields lists the named fields of struct type name in package pkg.
func structFields(all []file, pkg, name string) []string {
	for _, f := range all {
		if f.pkg != pkg || f.test {
			continue
		}
		for _, d := range f.ast.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, sp := range g.Specs {
				ts := sp.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok || ts.Name.Name != name {
					continue
				}
				out := []string{}
				for _, fl := range st.Fields.List {
					for _, n := range fl.Names {
						out = append(out, n.Name)
					}
				}
				return out
			}
		}
	}
	return nil
}

func keyed(t *testing.T, f file, lit *ast.CompositeLit) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			t.Errorf("%s: words literals are keyed", f.fset.Position(e.Pos()))
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); ok {
			out[k.Name] = kv.Value
		}
	}
	return out
}

func strLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		panic(fmt.Sprint(lit.Value, err))
	}
	return s, true
}

// TestIconSizes keeps icons at the sizes of native/theme: at any other size their stroke drifts from the text's weight.
func TestIconSizes(t *testing.T) {
	for _, f := range nativeFiles(t) {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "FontSize" || !startsAtIcon(sel.X) {
				return true
			}
			if _, isLit := call.Args[0].(*ast.BasicLit); isLit {
				t.Errorf("%s:%d: icon size must be theme.IconS, IconM, IconL or IconXL", f.path, f.fset.Position(call.Pos()).Line)
			}
			return true
		})
	}
}

// startsAtIcon reports whether a chain of element calls begins with ui.Icon.
func startsAtIcon(e ast.Expr) bool {
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "ui" && sel.Sel.Name == "Icon" {
			return true
		}
		e = sel.X
	}
}

// TestTextFieldsShowTheirCaret: every text field outside widgets is wrapped in widgets.Caret (widgets' own fields go
// through fieldLook), or a theme around it, as a list's, can make its caret invisible.
func TestTextFieldsShowTheirCaret(t *testing.T) {
	fields := map[string]bool{"TextInput": true, "TextInputBase": true, "SearchField": true, "Autocomplete": true, "TextArea": true, "TextAreaBase": true}
	for _, f := range parseAll(t) {
		if path.Base(f.pkg) == "widgets" {
			continue
		}
		wrapped := map[ast.Node]bool{}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "widgets" && sel.Sel.Name == "Caret" && len(call.Args) == 1 {
					wrapped[call.Args[0]] = true
				}
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "ui" && fields[sel.Sel.Name] && !wrapped[call] {
					t.Errorf("%s: ui.%s without widgets.Caret", f.fset.Position(call.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
	}
}

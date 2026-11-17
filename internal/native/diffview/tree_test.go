package diffview

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

// fc is a file of repo changed by commits commits.
func fc(repo, path string, commits int) File {
	return File{Path: path, Status: "M", Source: session.FileChange{Repo: repo, Path: path, Status: "M", Commits: commits, Add: 1}}
}

func files(spec ...string) []File {
	var out []File
	for _, s := range spec {
		repo, path, _ := cut(s)
		out = append(out, fc(repo, path, 1))
	}
	return out
}

func cut(s string) (string, string, bool) {
	for i := range s {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return "", s, false
}

// shown is the rows of t with every heading open: depth, then [heading] or the file's label.
func shown(t *Tree) []string {
	var out []string
	var walk func(ns []node, depth int)
	walk = func(ns []node, depth int) {
		for _, n := range ns {
			if n.kind == fileNode {
				out = append(out, fmt.Sprintf("%d%s", depth, t.label(n)))
				continue
			}
			out = append(out, fmt.Sprintf("%d[%s]", depth, t.label(n)))
			walk(t.shape.kids[n], depth+1)
		}
	}
	walk(t.shape.roots, 0)
	return out
}

func TestTreeShape(t *testing.T) {
	cases := []struct {
		name  string
		files []File
		group Grouper
		flat  bool
		want  []string
	}{
		{"repo, then directory (its whole path), then files", files("r:a/b/x.ts", "r:a/b/y.ts", "r:c/z.ts"), ByRepo, false,
			[]string{"0[r]", "1[a/b]", "2x.ts", "2y.ts", "1[c]", "2z.ts"}},
		{"a chain of single directories is one row", files("r:a/b/c/d.go"), ByRepo, false,
			[]string{"0[r]", "1[a/b/c]", "2d.go"}},
		{"files of a directory stay together; root files sit right under the repo", files("r:a/x.ts", "s:README.md", "r:b/y.ts", "r:a/w.ts"), ByRepo, false,
			[]string{"0[r]", "1[a]", "2x.ts", "2w.ts", "1[b]", "2y.ts", "0[s]", "1README.md"}},
		{"no groups: directories and root files at the top", files("a/x", "README", "a/y", "b/z"), nil, false,
			[]string{"0[a]", "1x", "1y", "0README", "0[b]", "1z"}},
		{"flat: the full paths under their group", files("r:a/x.ts", "r:b/y.ts"), ByRepo, true,
			[]string{"0[r]", "1a/x.ts", "1b/y.ts"}},
		{"flat without groups: a list", files("a/x.ts", "b/y.ts"), nil, true,
			[]string{"0a/x.ts", "0b/y.ts"}},
		{"several picked commits show ×N", []File{fc("r", "a/x.ts", 3), fc("r", "a/y.ts", 1)}, ByRepo, false,
			[]string{"0[r]", "1[a]", "2x.ts ×3", "2y.ts"}},
		{"a filter's files show ×N too", []File{
			{Path: "a/x.ts", Status: "M", Source: session.AggFile{Key: "r", Repo: "r", Path: "a/x.ts", Commits: 2}},
			{Path: "a/y.ts", Status: "A", Source: session.AggFile{Key: "r", Repo: "r", Path: "a/y.ts", Uncommitted: true}},
		}, nil, false, []string{"0[a]", "1x.ts ×2", "1y.ts"}},
		{"none", nil, ByRepo, false, nil},
	}
	for _, c := range cases {
		tr := &Tree{files: c.files, Flat: c.flat}
		tr.shape = shapeOf(c.files, c.group, c.flat)
		if got := shown(tr); !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestTreeShapeCountsAndHeadings(t *testing.T) {
	s := shapeOf(files("r:a/x", "r:a/y", "r:z", "s:b/w"), ByRepo, false)
	if s.count["r"] != 3 || s.count["s"] != 1 {
		t.Errorf("counts %v", s.count)
	}
	var heads []string
	for _, h := range s.heads {
		heads = append(heads, h.group+"/"+h.dir)
	}
	if want := []string{"r/a", "r/", "s/b", "s/"}; !slices.Equal(heads, want) {
		t.Errorf("headings %q, want %q", heads, want)
	}
	// The heading over each file, and over each directory: what opens to show a file.
	if up := s.up[s.leaf[0]]; up.kind != dirNode || up.dir != "a" {
		t.Errorf("over a/x: %+v", up)
	}
	if up := s.up[s.leaf[2]]; up.kind != groupNode || up.group != "r" {
		t.Errorf("over z: %+v", up)
	}
	if up := s.up[s.up[s.leaf[3]]]; up.kind != groupNode || up.group != "s" {
		t.Errorf("over b: %+v", up)
	}
}

func TestTreeOrderIsTheOrderTheTreeShows(t *testing.T) {
	cases := []struct {
		files []File
		group Grouper
		want  []string
	}{
		{files("r:a/x.ts", "s:README.md", "r:a/y/z.ts", "r:a/z.ts"), ByRepo, []string{"a/x.ts", "a/z.ts", "a/y/z.ts", "README.md"}},
		{files("b/1", "a/2", "b/3", "4"), nil, []string{"b/1", "b/3", "a/2", "4"}},
	}
	for _, c := range cases {
		var got []string
		for _, f := range TreeOrder(c.files, c.group) {
			got = append(got, f.Path)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("order %q, want %q", got, c.want)
		}
	}
}

type treeHarness struct {
	tt     *ui.Tester
	tree   *Tree
	files  []File
	sel    int
	picked []int
}

func newTreeHarness(t *testing.T, fs []File) *treeHarness {
	t.Helper()
	h := &treeHarness{files: fs, sel: -1}
	h.tree = &Tree{Selected: &h.sel, Group: ByRepo, OnSelect: func(i int) { h.picked = append(h.picked, i) }}
	h.tt = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { FileTree(c, h.tree, h.files).Grow(1).FillWidth() })
	}, 400, 400)
	h.tt.SetLocale("zh-Hans")
	h.tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	h.tt.Frame()
	return h
}

// chosen is the label of the row the tree has chosen.
func (h *treeHarness) chosen() string {
	if h.tree.row < 0 {
		return ""
	}
	return h.tree.label(h.tree.outline.Item(h.tree.row))
}

func TestTreeKeysMoveThroughTheRowsAndChooseFiles(t *testing.T) {
	h := newTreeHarness(t, files("r:a/x.go", "r:a/y.go", "r:b/z.go"))
	for _, s := range []string{"r", "3 个文件", "a", "x.go", "y.go", "b", "z.go"} {
		if !h.tt.HasText(s) {
			t.Fatalf("no %q in %q", s, h.tt.Texts())
		}
	}
	if err := h.tt.Click("x.go"); err != nil {
		t.Fatal(err)
	}
	if h.sel != 0 || !slices.Equal(h.picked, []int{0}) {
		t.Fatalf("click: sel %d, picked %v", h.sel, h.picked)
	}
	steps := []struct {
		mods   ui.Modifiers
		key    ui.Key
		chosen string
		sel    int
		zShown bool
	}{
		{0, ui.KeyDown, "y.go", 1, true},
		{0, ui.KeyDown, "b", 1, true}, // a heading: the file stays
		{0, ui.KeyLeft, "b", 1, false},
		{0, ui.KeyRight, "b", 1, true},
		{0, ui.KeyRight, "z.go", 2, true},
		{0, ui.KeyLeft, "b", 2, true},
		{0, ui.KeyUp, "y.go", 1, true},
		{0, ui.KeyLeft, "a", 1, true},
		{0, ui.KeyLeft, "a", 1, true}, // closed a
		{0, ui.KeyLeft, "r", 1, true},
	}
	for i, s := range steps {
		h.tt.Key(s.mods, s.key)
		if h.chosen() != s.chosen || h.sel != s.sel || h.tt.HasText("z.go") != s.zShown {
			t.Fatalf("step %d (%v): chose %q, file %d, shows %q", i, s.key, h.chosen(), h.sel, h.tt.Texts())
		}
	}
	if h.tt.HasText("x.go") {
		t.Errorf("a is closed but shows x.go")
	}
	if !slices.Equal(h.picked, []int{0, 1, 2, 1}) {
		t.Errorf("picked %v", h.picked)
	}
}

func TestTreeFollowsAFileChosenElsewhere(t *testing.T) {
	h := newTreeHarness(t, files("r:a/x.go", "r:b/y.go", "s:z.go"))
	h.tree.CollapseAll()
	h.tt.Frame()
	if h.tt.HasText("y.go") || h.tt.HasText("z.go") {
		t.Fatalf("collapsed, shows %q", h.tt.Texts())
	}
	// Preview Diff stepped to y.go: its headings open and the tree chooses it, without calling OnSelect.
	h.sel = 1
	h.tt.Frame()
	if !h.tt.HasText("y.go") || h.tt.HasText("x.go") || h.chosen() != "y.go" || len(h.picked) != 0 {
		t.Fatalf("followed: chose %q, picked %v, shows %q", h.chosen(), h.picked, h.tt.Texts())
	}
	h.tree.ExpandAll()
	h.tt.Frame()
	for _, s := range []string{"x.go", "y.go", "z.go"} {
		if !h.tt.HasText(s) {
			t.Errorf("expanded, no %s in %q", s, h.tt.Texts())
		}
	}
	// The same files in a new slice (a page filtering its files each frame) keep what is closed.
	h.tree.CollapseAll()
	h.files = slices.Clone(h.files)
	h.tt.Frame()
	if h.tt.HasText("x.go") || h.chosen() != "r" {
		t.Fatalf("same files anew: chose %q, shows %q", h.chosen(), h.tt.Texts())
	}
	h.sel = -1
	h.tt.Frame()
	if h.chosen() != "" {
		t.Errorf("none chosen, the tree chose %q", h.chosen())
	}
}

func TestTreeRefreshScrollsWhenTheChosenFileOrItsRowChanges(t *testing.T) {
	cases := []struct {
		name       string
		sel        int
		refresh    func([]File) ([]File, int)
		row        int
		scrollAway bool
		keepPlace  bool
	}{
		{"same file moves down", 0, func(fs []File) ([]File, int) {
			return slices.Concat(fs[1:41], fs[:1], fs[41:]), 40
		}, 41, false, false},
		{"same file moves up", 60, func(fs []File) ([]File, int) {
			return slices.Concat(fs[60:61], fs[:60], fs[61:]), 0
		}, 1, false, false},
		{"only the row index changes", 25, func(fs []File) ([]File, int) {
			for i := range 25 {
				fs[i].Path = fmt.Sprintf("dir-%02d/%s", i, fs[i].Path)
			}
			return fs, 25
		}, 51, false, false},
		{"same row after another path changes", 0, func(fs []File) ([]File, int) {
			fs[len(fs)-1].Path = "other.go"
			return fs, 0
		}, 1, true, true},
		{"same row after metadata changes", 0, func(fs []File) ([]File, int) {
			fs[0].Status = "A"
			return fs, 0
		}, 1, true, true},
		{"another file on the same row", 0, func(fs []File) ([]File, int) {
			fs[0].Path = "chosen.go"
			return fs, 0
		}, 1, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fs []File
			for i := range 100 {
				fs = append(fs, fc("r", fmt.Sprintf("file-%02d.go", i), 1))
			}
			h := newTreeHarness(t, fs)
			h.sel = tc.sel
			h.tt.Frame()
			if tc.scrollAway {
				h.tt.Scroll(100, 100, 0, 900)
			}
			first, last := h.tree.outline.List.Visible()
			anchor := h.tree.label(h.tree.outline.Item(first))
			before, ok := h.tt.Find(anchor)
			if !ok {
				t.Fatalf("missing scroll anchor %q", anchor)
			}
			h.files, h.sel = tc.refresh(slices.Clone(fs))
			h.tt.Frame()
			if h.tree.row != tc.row {
				t.Fatalf("chosen row %d, want %d", h.tree.row, tc.row)
			}
			nextFirst, nextLast := h.tree.outline.List.Visible()
			if tc.keepPlace {
				after, ok := h.tt.Find(anchor)
				if nextFirst != first || nextLast != last || !ok || after != before {
					t.Errorf("refresh moved scroll: rows %d…%d → %d…%d, anchor %+v → %+v", first, last, nextFirst, nextLast, before, after)
				}
				return
			}
			if h.tree.row < nextFirst || h.tree.row > nextLast {
				t.Errorf("chosen row %d outside visible rows %d…%d", h.tree.row, nextFirst, nextLast)
			}
			r, ok := h.tt.Find(h.chosen())
			if !ok || r.Y < 0 || r.Y+r.H > 400 {
				t.Errorf("chosen file is clipped at %+v (found %v)", r, ok)
			}
		})
	}
}

func TestTreeOpensEveryHeadingInAnotherListUnlessKeepClosed(t *testing.T) {
	h := newTreeHarness(t, files("r:a/x.go", "r:b/y.go"))
	h.tree.CollapseAll()
	h.tt.Frame()
	h.files, h.sel = files("r:a/w.go", "r:c/v.go"), 1 // the log's next commit
	h.tt.Frame()
	if !h.tt.HasText("w.go") || !h.tt.HasText("v.go") || h.chosen() != "v.go" {
		t.Errorf("next list: chose %q, shows %q", h.chosen(), h.tt.Texts())
	}
}

func TestTreeKeepsClosedHeadingsFromListToList(t *testing.T) {
	h := newTreeHarness(t, files("r:a/x.go", "r:b/y.go"))
	h.tree.KeepClosed = true
	if err := h.tt.Click("x.go"); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(0, ui.KeyLeft) // to a
	h.tt.Key(0, ui.KeyLeft) // closes a
	if h.tt.HasText("x.go") || h.chosen() != "a" {
		t.Fatalf("closing a: chose %q, shows %q", h.chosen(), h.tt.Texts())
	}
	steps := []struct {
		name   string
		files  []File
		sel    int
		chosen string
		shown  []string
		hidden []string
	}{
		// Another commit: a stays closed, c is new and open.
		{"another list", files("r:a/w.go", "r:c/v.go"), 1, "v.go", []string{"v.go"}, []string{"w.go"}},
		// Preview Diff steps into the closed a: it opens.
		{"stepping into it", nil, 0, "w.go", []string{"w.go", "v.go"}, nil},
	}
	for _, s := range steps {
		if s.files != nil {
			h.files = s.files
		}
		h.sel = s.sel
		h.tt.Frame()
		if h.chosen() != s.chosen {
			t.Errorf("%s: chose %q", s.name, h.chosen())
		}
		for _, x := range s.shown {
			if !h.tt.HasText(x) {
				t.Errorf("%s: no %s in %q", s.name, x, h.tt.Texts())
			}
		}
		for _, x := range s.hidden {
			if h.tt.HasText(x) {
				t.Errorf("%s: shows %s", s.name, x)
			}
		}
	}
	// A list whose file shown is in a closed heading keeps it closed; the choice is that heading.
	h.tt.Key(0, ui.KeyLeft)
	h.tt.Key(0, ui.KeyLeft)
	h.files, h.sel = files("r:a/u.go", "r:d/t.go"), 0
	h.tt.Frame()
	if h.tt.HasText("u.go") || !h.tt.HasText("t.go") || h.chosen() != "a" {
		t.Errorf("shown file closed in: chose %q, shows %q", h.chosen(), h.tt.Texts())
	}
	// Expand all opens what other lists closed too.
	h.tree.ExpandAll()
	h.files = files("r:a/x.go")
	h.tt.Frame()
	if !h.tt.HasText("x.go") {
		t.Errorf("after expand all, a closed: %q", h.tt.Texts())
	}
}

func TestTreeMarksTheFileShownWhileTheChoiceIsElsewhere(t *testing.T) {
	h := newTreeHarness(t, files("r:a/x.go", "r:a/y.go"))
	x, y := h.tree.shape.leaf[0], h.tree.shape.leaf[1]
	type look struct{ name, bg ui.Color }
	at := func(n node) look { c, b := h.tree.look(n); return look{c, b} }
	if got := at(x); got != (look{theme.Text, ui.Transparent}) {
		t.Errorf("nothing shown: %v", got)
	}
	if err := h.tt.Click("x.go"); err != nil {
		t.Fatal(err)
	}
	if got := at(x); got != (look{theme.Text, ui.Transparent}) {
		t.Errorf("x.go chosen: %v", got)
	}
	h.tt.Key(0, ui.KeyLeft) // the choice goes to a; x.go is still shown
	if got := at(x); h.chosen() != "a" || got != (look{theme.Text, theme.SelDim}) {
		t.Errorf("choice on %q: x.go %v", h.chosen(), got)
	}
	h.tree.CollapseAll()
	h.tree.ExpandAll()
	h.tt.Frame()
	if got := at(x); h.chosen() != "r" || got != (look{theme.Text, theme.SelDim}) {
		t.Errorf("after collapse and expand all, choice on %q: x.go %v", h.chosen(), got)
	}
	if got := at(y); got != (look{theme.Text, ui.Transparent}) {
		t.Errorf("y.go %v", got)
	}
}

func TestTreeNotesAndRows(t *testing.T) {
	fs := []File{
		{Path: "a/x.go", Status: "M", Source: session.FileStatus{Path: "a/x.go", Area: "staged", Add: 3, Del: 2}},
		{Path: "logo.png", Status: "A", Source: session.FileStatus{Path: "logo.png", Area: "untracked", Binary: true}},
	}
	h := newTreeHarness(t, fs)
	h.tree.Group = nil
	h.tree.Note = func(f File) string { return f.Source.(session.FileStatus).Area }
	extra := map[int]bool{}
	h.tree.Row = func(c *ui.Context, row ui.Element, i int) { extra[i] = true }
	h.files = slices.Clone(fs) // another slice: rebuilt without groups
	h.tt.Frame()
	for _, s := range []string{"+3 −2", "bin", "staged", "untracked"} {
		if !h.tt.HasText(s) {
			t.Errorf("no %q in %q", s, h.tt.Texts())
		}
	}
	if _, grouped := h.tt.Find("r"); grouped || !extra[0] || !extra[1] {
		t.Errorf("rows %v, texts %q", extra, h.tt.Texts())
	}
}

func TestTreeStatsUseAddedAndDeletedColours(t *testing.T) {
	h := newTreeHarness(t, []File{{Path: "a.txt", Status: "M", Source: session.FileChange{Add: 38, Del: 25}}})
	r, ok := h.tt.Find("+38 −25")
	if !ok {
		t.Fatal("no line statistics")
	}
	img := h.tt.Image()
	for _, want := range []ui.Color{theme.Add, theme.Del} {
		found := false
		for y := int(r.Y); y < int(r.Y+r.H); y++ {
			for x := int(r.X); x < int(r.X+r.W); x++ {
				p := img.RGBAAt(x, y)
				if p.R == want.R && p.G == want.G && p.B == want.B {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("statistics do not paint colour %v", want)
		}
	}
}

func TestFitTailKeepsTheEndOfAPath(t *testing.T) {
	runes := func(s string) float32 { return float32(len([]rune(s))) }
	cases := []struct {
		s     string
		width float32
		want  string
	}{
		{"src/main/java/dto", 8, "…ava/dto"},
		{"src/main/java/dto", 4, "…dto"},
		{"src/main/java/dto", 1, "…"},
		{"src/main/java/dto", 0, "…"},
		{"文档/药学门诊", 5, "…药学门诊"},
	}
	for _, c := range cases {
		if got := fitTail(c.s, c.width, runes); got != c.want {
			t.Errorf("fitTail(%q, %v) = %q, want %q", c.s, c.width, got, c.want)
		}
	}
}

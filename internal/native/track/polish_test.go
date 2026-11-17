package track

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

func TestCompareCandidatesTakeKeyboardFocus(t *testing.T) {
	w := tr.For("zh-Hans")
	p := &Page{pop: popup{ok: true, opts: []session.Option{{Value: "feat-a", Count: 2}}}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Themed(c, theme.Theme()).Grow(1).Children(func() { p.targetBody(c, w, "master") })
	}, 360, 300)
	if !tt.Focused(w.BranchSearch) {
		t.Fatal("the branch search did not take focus")
	}
	for _, label := range []string{w.CompareBase, w.Fav, "feat-a"} {
		tt.Key(0, ui.KeyTab)
		if !tt.Focused(label) {
			t.Fatalf("Tab did not reach %q", label)
		}
	}
}

func TestBranchCandidatesSkipUsedBranchesAndActivateFromKeyboard(t *testing.T) {
	w := tr.For("zh-Hans")
	r := session.AggRepo{Key: "a", Repo: "repo", Branch: "feat-a", Auto: "feat-a"}
	p := &Page{
		rows: []session.AggRepo{r, {Key: "b", Repo: "repo", Branch: "feat-used"}},
		pop: popup{kind: "branch", key: r.Key, ok: true, brs: []session.AggBranch{
			{Name: "feat-a"}, {Name: "feat-used"}, {Name: "feat-c"},
		}},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Themed(c, theme.Theme()).Grow(1).Children(func() { p.branchBody(c, w, r) })
	}, 360, 300)
	if err := tt.Click("feat-used"); err != nil {
		t.Fatal(err)
	}
	if len(p.branches) != 0 || p.pop.kind != "branch" {
		t.Fatal("a used branch was picked")
	}
	if err := tt.Click(w.BranchSearch); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"feat-a", "feat-c"} {
		tt.Key(0, ui.KeyTab)
		if !tt.Focused(label) {
			t.Fatalf("Tab did not reach %q", label)
		}
	}
	tt.Key(0, ui.KeyEnter)
	if p.branches[r.Key] != "feat-c" || p.pop.kind != "" {
		t.Fatalf("keyboard pick: branches %v, popup %q", p.branches, p.pop.kind)
	}
}

func TestDirtyTableFocusFollowsDirectorySelection(t *testing.T) {
	w := tr.For("zh-Hans")
	dirs := []session.DirtyDir{
		{Path: "/repo-a", Repo: "repo-a", Branch: "feat-a"},
		{Path: "/repo-b", Repo: "repo-b", Branch: "feat-b"},
	}
	d := dirtyPage{tab: 1}
	d.init(nil)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Themed(c, theme.Theme()).Grow(1).Children(func() {
			ui.Button(c, "outside")
			d.table(c, &Page{}, w, dirs)
		})
	}, 1400, 400)
	if err := tt.Click("repo-a"); err != nil {
		t.Fatal(err)
	}
	if d.focus != "/repo-a" || d.tab != 1 {
		t.Fatalf("row click: directory %q, tab %d", d.focus, d.tab)
	}
	if err := tt.Click("repo-b"); err != nil {
		t.Fatal(err)
	}
	if d.focus != "/repo-b" {
		t.Fatalf("second row click: directory %q", d.focus)
	}
	if err := tt.Click("outside"); err != nil {
		t.Fatal(err)
	}
	if d.focus != "/repo-b" {
		t.Fatalf("click outside: directory %q", d.focus)
	}
	if err := tt.Click("repo-b"); err != nil {
		t.Fatal(err)
	}
	if d.focus != "" {
		t.Fatalf("click on the scoped row again: directory %q", d.focus)
	}
}

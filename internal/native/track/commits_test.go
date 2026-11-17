package track

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

func cachedCommits(rows ...session.Row) *commitList {
	return &commitList{AggCommits: session.AggCommits{Total: len(rows)}, pages: map[int][]session.Row{0: rows}}
}

func TestCommitListKeepsSelectionAndOpensFromKeyboardOrDoubleClick(t *testing.T) {
	for _, scope := range []string{"requirement", "branch", "compare"} {
		for _, activate := range []string{"Enter", "double click"} {
			t.Run(scope+"/"+activate, func(t *testing.T) {
				cond := query.Default()
				cond.Q = "YT1"
				tag := session.TagView{Cond: cond}
				rows := []session.Row{
					{Repo: "api", Hash: "first", Subject: "first commit"},
					{Repo: "billing", Hash: "second", Subject: "second commit"},
				}
				h := &viewtest.Host{}
				p := &Page{h: h, reqsOK: true, temp: &tempFilter{Req: "YT1", Cond: cond}, want: tmpID,
					res: &session.TagAggResult{Tag: tag}, commits: cachedCommits(rows...), down: cachedCommits(rows...), commitSel: -1}
				if scope == "branch" || scope == "compare" {
					p.focus = "api"
				}
				if scope == "compare" {
					p.side = 1
				}
				p.commitSt.Selected = &p.commitSel
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Themed(c, theme.Theme()).Grow(1).Children(func() { p.commitsList(c, tr.Of(c)) })
				}, 900, 300)
				if activate == "Enter" {
					tt.Key(0, ui.KeyTab)
					if scope == "compare" { // past 回到本分支 over the list
						tt.Key(0, ui.KeyTab)
					}
					tt.Key(0, ui.KeyEnter)
					if len(h.Went) != 0 {
						t.Fatal("Enter with no selection opened a commit")
					}
					tt.Key(0, ui.KeyDown)
					tt.Key(0, ui.KeyDown)
					tt.Frame()
					if p.commitSel != 1 {
						t.Fatalf("keyboard selection was lost between frames: %d", p.commitSel)
					}
					tt.Key(0, ui.KeyEnter)
				} else {
					if err := tt.Click(rows[1].Subject); err != nil {
						t.Fatal(err)
					}
					tt.Frame()
					if p.commitSel != 1 {
						t.Fatalf("clicked selection was lost between frames: %d", p.commitSel)
					}
					if err := tt.Click(rows[1].Subject); err != nil {
						t.Fatal(err)
					}
				}
				diffs := h.Wins.Diffs()
				if len(diffs) != 1 || len(h.Went) != 0 {
					t.Fatalf("activation should open exactly one diff: %+v, navigation %+v", diffs, h.Went)
				}
				want := session.DiffSource{Kind: "commit", Repo: rows[1].Repo, Hash: rows[1].Hash}
				if diffs[0] != want {
					t.Errorf("opened %+v, want %+v", diffs[0], want)
				}
			})
		}
	}
}

func TestCommitSelectionClearsWhenTheShownDataChanges(t *testing.T) {
	p := &Page{
		commits: cachedCommits(session.Row{Subject: "branch commit"}),
		down:    cachedCommits(session.Row{Subject: "compare commit"}),
		focus:   "api", commitSel: -1,
	}
	p.commitSt.Selected = &p.commitSel
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Themed(c, theme.Theme()).Grow(1).Children(func() { p.commitsList(c, tr.Of(c)) })
	}, 900, 300)
	for _, side := range []int{0, 1, 0} {
		p.side = side
		tt.Frame()
		if p.commitSel != -1 {
			t.Fatalf("switching commit sources kept selection %d", p.commitSel)
		}
		// Tab to the list: on side 1 past 回到本分支 over it.
		for range 3 {
			tt.Key(0, ui.KeyTab)
			tt.Key(0, ui.KeyHome)
			tt.Frame()
			if p.commitSel == 0 {
				break
			}
		}
		if p.commitSel != 0 {
			t.Fatalf("selection was not retained on side %d: %d", side, p.commitSel)
		}
	}
	p.commits = cachedCommits(session.Row{Subject: "refreshed commit"})
	tt.Frame()
	if p.commitSel != -1 {
		t.Errorf("new commit data kept the old selection: %d", p.commitSel)
	}
}

// The line under the commits shows the merges hidden and toggles them.
func TestCommitsTailTogglesHiddenMerges(t *testing.T) {
	w := tr.For("zh-Hans")
	l := cachedCommits(session.Row{Repo: "api", Hash: "abcdef123456", Subject: "YT-1 work"})
	l.Merges = 2
	p := &Page{commits: l, commitSel: -1}
	p.commitSt.Selected = &p.commitSel
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Themed(c, theme.Theme()).Grow(1).Children(func() { p.commitsList(c, w) })
	}, 900, 300)
	tt.SetLocale("zh-Hans")
	hidden := fill(w.MergesHidden, "n", 2)
	if !tt.HasText("abcdef1") || !tt.HasText(hidden) {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click(hidden); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !p.merges || !tt.HasText(w.MergesShown) {
		t.Errorf("merges %v, texts %q", p.merges, tt.Texts())
	}
}

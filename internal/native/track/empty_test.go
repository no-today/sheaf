package track

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
)

func TestEmptyRequirementPageHasNoLogEntry(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := openWithoutFilters(t, locale)
			w := tr.For(locale)
			for _, text := range []string{w.Intro, w.IntroHint, fill(w.IntroPattern, "pattern", session.DefaultRequirement), w.IntroSettings} {
				if !h.TT.HasText(text) {
					t.Errorf("missing %q; texts %q", text, h.TT.Texts())
				}
			}
			if h.TT.HasText("Saved") || h.TT.HasText("已保存") || slices.Contains(h.TT.Texts(), "0") {
				t.Errorf("empty sidebar has a heading or zero count: %q", h.TT.Texts())
			}
			if len(h.H.Went) != 0 || len(h.H.Wins.Diffs()) != 0 {
				t.Error("empty page opened a removed view or window")
			}
		})
	}
}

func TestSidebarShowsOnlyPopulatedGroupsAndCounts(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, tc := range []struct {
			name        string
			temp        *tempFilter
			dirty       []int
			count       string
			requirement string
		}{
			{name: "empty"},
			{name: "temporary", temp: &tempFilter{Branch: "feat-a"}},
			{name: "clean", dirty: []int{0, -1}},
			{name: "one directory", dirty: []int{0, 3, -1}, count: "1"},
			{name: "two directories", dirty: []int{2, 3}, count: "2"},
			{name: "tracked directory", dirty: []int{3}, requirement: "YT1", count: "1"},
			{name: "mixed directories", dirty: []int{3, 1, 0, -1}, requirement: "YT1", count: "2"},
		} {
			t.Run(locale+"/"+tc.name, func(t *testing.T) {
				h := &viewtest.Host{}
				wantDirs := 0
				for _, dirty := range tc.dirty {
					if dirty > 0 {
						wantDirs++
					}
					h.Inf.Repos = append(h.Inf.Repos, session.RepoInfo{Worktrees: []session.WorktreeInfo{{Dirty: dirty, Requirement: tc.requirement}}})
				}
				p := &Page{h: h, reqsOK: true, temp: tc.temp}
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Themed(c, theme.Theme()).Grow(1).Children(func() { p.leftBody(c) })
				}, 260, 600)
				tt.SetLocale(locale)
				w := tr.For(locale)
				if got := slices.Contains(tt.Texts(), "Saved") || slices.Contains(tt.Texts(), "已保存"); got {
					t.Errorf("saved heading remains; texts %q", tt.Texts())
				}
				if slices.Contains(tt.Texts(), "0") || tc.count != "" && !slices.Contains(tt.Texts(), tc.count) || p.dirtyDirs() != wantDirs {
					t.Errorf("uncommitted count; want %q, texts %q", tc.count, tt.Texts())
				}
				if tt.HasText(w.None) {
					t.Errorf("sidebar repeats the main empty state: %q", tt.Texts())
				}
			})
		}
	}
}

func TestEmptyCommitsKeepTheHiddenMergeControl(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			w := tr.For(locale)
			p := &Page{commits: &commitList{AggCommits: session.AggCommits{Merges: 2}}}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Themed(c, theme.Theme()).Grow(1).Children(func() { p.commitsList(c, w) })
			}, 600, 260)
			for _, text := range []string{w.MergesHiddenTitle, w.Show} {
				if !tt.HasText(text) {
					t.Errorf("missing %q; texts %q", text, tt.Texts())
				}
			}
			if slices.Contains(tt.Texts(), w.NoCommits) || tt.HasText(w.NoCommitsHint) {
				t.Errorf("hidden commits show an unrelated empty state: %q", tt.Texts())
			}
			title, _ := tt.Find(w.MergesHiddenTitle)
			action, _ := tt.Find(w.Show)
			if action.Y <= title.Y {
				t.Errorf("Show is not under the title: title %+v, action %+v", title, action)
			}
			if err := tt.Click(w.Show); err != nil {
				t.Fatal(err)
			}
			if !p.merges {
				t.Error("cannot show hidden merge commits from the empty state")
			}
		})
	}
}

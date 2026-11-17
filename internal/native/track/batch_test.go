package track

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

func TestSheetCountsOnlyRunnablePicks(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rows         []sheetRow
		run, skipped int
	}{
		{name: "loading"},
		{name: "runnable", rows: []sheetRow{{}, {}}, run: 2},
		{name: "skipped", rows: []sheetRow{{skip: true}, {skip: true, off: true}}, skipped: 2},
		{name: "unticked stays selectable", rows: []sheetRow{{}, {off: true}, {skip: true}}, run: 1, skipped: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := sheet{rows: tc.rows}
			if run, skipped := sh.count(); run != tc.run || skipped != tc.skipped {
				t.Errorf("got %d runnable, %d skipped; want %d, %d", run, skipped, tc.run, tc.skipped)
			}
		})
	}
}

func TestPlanCount(t *testing.T) {
	en := tr.For("en-US")
	for _, tc := range []struct {
		text string
		n    int
		want string
	}{
		{en.MrTitle, 1, "Create an MR for 1 branch"},
		{en.MrTitle, 2, "Create MRs for 2 branches"},
		{en.MrGo, 1, "Create 1 MR"},
		{en.MrGo, 2, "Create 2 MRs"},
		{zh.SyncTitle, 1, "更新 1 个收藏分支"},
		{en.SyncTitle, 1, "Update 1 favourite branch"},
		{en.SyncTitle, 2, "Update 2 favourite branches"},
		{en.SyncGo, 1, "Update 1 branch"},
		{en.SyncGo, 2, "Update 2 branches"},
	} {
		if got := planCount(tc.text, tc.n); got != tc.want {
			t.Errorf("%q, %d: got %q, want %q", tc.text, tc.n, got, tc.want)
		}
	}
}

func TestSheetSelectionAndSkipGroup(t *testing.T) {
	for _, tc := range []struct {
		locale, title, empty, goLabel string
		action                        sheetAction
	}{
		{"zh-Hans", "为 1 个分支创建 MR", "为 0 个分支创建 MR", "创建 1 个 MR", actionMR},
		{"en-US", "Create an MR for 1 branch", "Create MRs for 0 branches", "Create 1 MR", actionMR},
		{"zh-Hans", "更新 1 个收藏分支", "更新 0 个收藏分支", "更新 1 个分支", actionSync},
		{"en-US", "Update 1 favourite branch", "Update 0 favourite branches", "Update 1 branch", actionSync},
	} {
		t.Run(tc.title, func(t *testing.T) {
			sh := &sheet{open: true, action: tc.action, rows: []sheetRow{
				{key: "blocked", repo: "blocked", branch: "main", skip: true, how: func(w *words) string { return w.MrSkipNoBranch }},
				{key: "allowed", repo: "allowed", branch: "feature", how: func(w *words) string { return fill(w.MrHowOpen, "b", "feature", "t", "main") }},
			}}
			p := &Page{sheet: sh}
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme.Theme())
				p.w = tr.Of(c)
				p.sheetPopover(c)
			}, 1180, 500)
			tt.SetLocale(tc.locale)
			w := tr.For(tc.locale)
			if !tt.HasText(tc.title) || !tt.HasText(tc.goLabel) {
				t.Fatalf("wrong runnable count: %q", tt.Texts())
			}
			allowed, _ := tt.Find("allowed")
			blocked, _ := tt.Find("blocked")
			heading, ok := tt.Find(fill(w.PlanSkipped, "n", 1))
			if !ok || allowed.Y >= heading.Y || heading.Y >= blocked.Y {
				t.Fatalf("skipped rows are mixed with runnable rows: allowed %+v, heading %+v, skipped %+v", allowed, heading, blocked)
			}
			if _, ok := tt.Find(fill(w.PlanPick, "repo", "blocked", "branch", "main")); ok {
				t.Error("skipped row has a checkbox")
			}
			pick := fill(w.PlanPick, "repo", "allowed", "branch", "feature")
			if err := tt.Click(pick); err != nil {
				t.Fatal(err)
			}
			if !sh.rows[1].off || !tt.HasText(tc.empty) || tt.HasText(tc.goLabel) {
				t.Fatalf("unticking did not update the count and hide the action: %q", tt.Texts())
			}
			if err := tt.Click(pick); err != nil {
				t.Fatal(err)
			}
			if sh.rows[1].off || !tt.HasText(tc.title) || !tt.HasText(tc.goLabel) {
				t.Fatalf("unticked row could not be selected again: %q", tt.Texts())
			}
			tt.Key(0, ui.KeyEscape)
			if p.sheet != nil {
				t.Error("Escape did not dismiss the sheet")
			}
		})
	}
}

func TestSheetStaysHiddenWhileItsPlanIsMade(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sh       *sheet
		planning bool
	}{
		{name: "planning", sh: &sheet{busy: true}, planning: true},
		{name: "ready", sh: &sheet{open: true, rows: []sheetRow{{key: "a", repo: "a", how: func(*words) string { return "x" }}}}},
		{name: "running", sh: &sheet{busy: true, rows: []sheetRow{{key: "a", repo: "a", how: func(*words) string { return "x" }}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.sh.planning(); got != tc.planning {
				t.Fatalf("planning() = %v", got)
			}
			p := &Page{sheet: tc.sh}
			tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.Theme()); p.w = tr.Of(c); p.sheetPopover(c) }, 1180, 760)
			if shown := len(tt.Texts()) > 0; shown != tc.sh.open {
				t.Errorf("sheet shown = %v, want %v: %v", shown, tc.sh.open, tt.Texts())
			}
		})
	}
}

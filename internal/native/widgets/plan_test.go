package widgets

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

func TestPlanSheetKeepsItsFooterVisibleWhileRowsScroll(t *testing.T) {
	for _, height := range []int{900, 360} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			open, picked := true, true
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme.Theme())
				PlanSheet(c, &open, Plan{
					Title: "Run 40 actions", Promise: "Only clean working directories.",
					Rows: func() {
						for i := range 40 {
							PlanRow(c.Key(i), PlanEntry{
								Repo: fmt.Sprintf("repo-%02d", i), Branch: fmt.Sprintf("feature-%02d-%s", i, strings.Repeat("long-", 8)),
								Action:  fmt.Sprintf("action-%02d-%s", i, strings.Repeat("long action ", 12)),
								Leading: func() { ui.Checkbox(c, &picked, "").Label(fmt.Sprintf("pick-%02d", i)) },
							}).Label(fmt.Sprintf("row-%02d", i))
						}
					},
					Options: func() { ui.Checkbox(c, &picked, "Fetch first") },
					Actions: func() { Button(c, "Cancel"); PrimaryButton(c, "Run 40") },
				}).Label("sheet")
			}, 1180, height)
			tt.Advance(140 * time.Millisecond)
			panel := planRect(t, tt, "sheet")
			if panel.W != 640 || panel.Y < 20 || panel.Y+panel.H > float32(height)-20 {
				t.Fatalf("panel escapes the window margins: %+v", panel)
			}
			footer := planRect(t, tt, "Run 40")
			if footer.Y+footer.H > panel.Y+panel.H-20 {
				t.Fatalf("footer escapes the panel: %+v", footer)
			}
			row := planRect(t, tt, "row-00")
			last := planRect(t, tt, "row-39")
			if row.H != planRowH || last.Y < footer.Y {
				t.Fatalf("rows should overflow only their scroll area: first %+v, last %+v, footer %+v", row, last, footer)
			}
			branch := planRect(t, tt, "feature-00-"+strings.Repeat("long-", 8))
			action := planRect(t, tt, "action-00-"+strings.Repeat("long action ", 12))
			if branch.H > row.H || action.H > row.H || action.X+action.W > panel.X+panel.W-20 || branch.X+branch.W > action.X {
				t.Fatalf("long text escaped its single-line columns: branch %+v, action %+v", branch, action)
			}
			tt.Scroll(row.X+row.W/2, row.Y+row.H/2, 0, 2000)
			last = planRect(t, tt, "row-39")
			if last.Y+last.H > footer.Y || last.Y < panel.Y {
				t.Errorf("last row cannot be reached: %+v, footer %+v", last, footer)
			}
			if after := planRect(t, tt, "Run 40"); after != footer {
				t.Errorf("scrolling moved the footer: before %+v, after %+v", footer, after)
			}
		})
	}
}

func planRect(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing %q", label)
	}
	return r
}

package track

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/session"
)

func TestHiddenMergeCountsDescribeTheirMeaning(t *testing.T) {
	for _, tc := range []struct {
		locale string
		n      int
		hidden string
	}{
		{"zh-Hans", 1, "已隐藏 1 个无冲突合并，点此显示"},
		{"zh-Hans", 2, "已隐藏 2 个无冲突合并，点此显示"},
		{"en-US", 1, "1 clean merge hidden. Click to show"},
		{"en-US", 2, "2 clean merges hidden. Click to show"},
	} {
		w := tr.For(tc.locale)
		if got := fill(w.MergesHidden, "n", tc.n); got != tc.hidden {
			t.Errorf("%s/%d hidden merges = %q, want %q", tc.locale, tc.n, got, tc.hidden)
		}
	}
}

func TestMRResultsStayVisibleAtMinimumWidth(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := open(t, locale)
			h.Until("filter details", func() bool { return !h.p.aggBusy && !h.p.filesBusy && h.p.commits != nil && h.p.stream.Loaded() > 0 })
			w := tr.For(locale)
			r := h.p.rows[0]
			url := "https://gitlab.example.com/g/repo-a/-/merge_requests/12"
			row := session.MRRow{Key: r.Key, Outcome: "created", URL: url}
			h.p.mrRun = &session.MRRun{Created: 1, Rows: []session.MRRow{row}}
			h.p.mrFor = h.p.filterKey()
			cell := mrCell(w, row)
			h.TT.SetSize(1700, 900)
			h.TT.Frame()
			labels := []string{cell.Text}
			full := map[string]ui.Rect{}
			for _, label := range labels {
				r, ok := h.TT.Find(label)
				if !ok {
					t.Fatalf("missing result %q", label)
				}
				full[label] = r
			}
			h.TT.SetSize(1180, 760)
			for _, label := range labels {
				r, ok := h.TT.Find(label)
				if !ok || r.X < 0 || r.X+r.W > 1180 || r.W < full[label].W-0.5 || r.H > full[label].H+0.5 {
					t.Errorf("result %q clipped at 1180: %+v, full %+v", label, r, full[label])
				}
			}
			before, _ := h.TT.Find(cell.Text)
			h.TT.Scroll(before.X+before.W/2, before.Y+before.H/2, 1000, 0)
			after, _ := h.TT.Find(cell.Text)
			if before.X != after.X {
				t.Errorf("results still require horizontal scrolling: before %+v, after %+v", before, after)
			}
			if !h.H.DockOf.Left.Open {
				t.Error("left dock must remain open")
			}
			if err := h.TT.Click(cell.Text); err != nil {
				t.Fatal(err)
			}
			if urls := h.TT.OpenedURLs(); len(urls) != 1 || urls[0] != url {
				t.Errorf("MR result link did not open: %v", urls)
			}
		})
	}
}

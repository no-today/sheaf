package diffview

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestFoldControlsExpandLinesAndKeepNavigation(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		w := tr.For(locale)
		for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
			for _, tc := range []struct {
				name  string
				label string
				mods  ui.Modifiers
				left  int
			}{
				{"up by a step", w.ExpandUp, 0, 27},
				{"all by label", w.ExpandAll, 0, 0},
				{"all with shift", w.ExpandUp, ui.Shift, 0},
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", locale, layout, tc.name), func(t *testing.T) {
					s := New([]File{{Path: "a.txt", Texts: changedAt(50, 150, 250)}}, nil)
					t.Cleanup(s.Close)
					s.Layout = layout
					tt := tester(s)
					tt.SetLocale(locale)
					s.Diff().ExpansionLines = 20
					tt.Frame()
					if err := tt.Click("a.txt"); err != nil {
						t.Fatal(err)
					}
					if _, shown := tt.Find(w.Folded(47)); !shown {
						t.Fatalf("missing initial fold: %q", tt.Texts())
					}
					if err := tt.ClickWith(tc.mods, tc.label); err != nil {
						t.Fatal(err)
					}
					if _, shown := tt.Find(w.Folded(47)); shown {
						t.Fatal("the first gap did not expand")
					}
					if tc.left > 0 {
						if _, shown := tt.Find(w.Folded(tc.left)); !shown {
							t.Fatalf("missing remaining %d-line gap: %q", tc.left, tt.Texts())
						}
					}
					tt.Key(0, ui.KeyDown)
					if got := where(s); got != "a.txt 1" {
						t.Fatalf("navigation after expansion: %s", got)
					}
				})
			}
		}
	}
}

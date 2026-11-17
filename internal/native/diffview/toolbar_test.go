package diffview

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestHeaderShowsOnlyUsefulNavigationAndCounts(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		w := tr.For(locale)
		for _, tc := range []struct {
			name     string
			texts    *Texts
			files    int
			changes  int
			add, del string
		}{
			{"single change", &Texts{Old: "old\n", New: "new\n"}, 1, 1, "+1", "−1"},
			{"addition", &Texts{New: "new"}, 1, 1, "+1", ""},
			{"deletion", &Texts{Old: "old"}, 1, 1, "", "−1"},
			{"unchanged", &Texts{Old: "same\n", New: "same\n"}, 1, 0, "", ""},
			{"binary", &Texts{Binary: true}, 1, 0, "", ""},
			{"several changes", changedAt(50, 150, 250), 1, 3, "+3", "−3"},
			{"several files", &Texts{Old: "old\n", New: "new\n"}, 2, 1, "+1", "−1"},
		} {
			t.Run(locale+"/"+tc.name, func(t *testing.T) {
				files := []File{{Path: "a.txt", Status: "M", Texts: tc.texts}}
				if tc.files > 1 {
					files = append(files, File{Path: "b.txt", Texts: tc.texts})
				}
				s := New(files, nil)
				t.Cleanup(s.Close)
				tt := tester(s)
				tt.SetLocale(locale)
				tt.Frame()
				for label, want := range map[string]bool{
					w.PrevChange: tc.changes > 1 || tc.files > 1, w.NextChange: tc.changes > 1 || tc.files > 1,
					w.PrevFile: tc.files > 1, w.NextFile: tc.files > 1,
					fmt.Sprintf(w.FileOf, 1, tc.files): tc.files > 1,
					w.Change(1, max(tc.changes, 1)):    tc.changes > 1,
					"+0":                               false, "−0": false,
				} {
					if _, shown := tt.Find(label); shown != want {
						t.Errorf("%q shown %v, want %v; texts %q", label, shown, want, tt.Texts())
					}
				}
				for _, count := range []string{tc.add, tc.del} {
					if count != "" {
						if _, shown := tt.Find(count); !shown {
							t.Errorf("missing nonzero count %q", count)
						}
					}
				}
				if tc.files > 1 {
					if err := tt.Click(w.NextFile); err != nil {
						t.Fatal(err)
					}
					if s.Current() != 1 {
						t.Fatal("next file did not select b.txt")
					}
					_ = tt.Click(w.NextFile) // greyed out at the last file
					tt.Frame()
					if s.Current() != 1 {
						t.Fatal("next file at the last file moved")
					}
					if err := tt.Click(w.PrevFile); err != nil {
						t.Fatal(err)
					}
					if s.Current() != 0 {
						t.Fatal("previous file did not select a.txt")
					}
				}
			})
		}
	}
}

func TestEmptyPreview(t *testing.T) {
	for _, tc := range []struct {
		locale, title, hint string
	}{
		{"zh-Hans", "选一个文件", "这里显示它的改动"},
		{"en-US", "Select a file", "Its changes appear here"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			tt := ui.NewTester(func(c *ui.Context) { Empty(c) }, 400, 300)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if !tt.HasText(tc.title) || !tt.HasText(tc.hint) {
				t.Fatalf("empty preview: %q", tt.Texts())
			}
		})
	}
}

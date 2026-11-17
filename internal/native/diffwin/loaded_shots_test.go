package diffwin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

func TestShotsLoaded(t *testing.T) {
	root := viewtest.Fixture(t)
	if err := os.WriteFile(filepath.Join(root, "repo-a", "a.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := viewtest.Open(t, root)
	in := &session.TagAggInput{Cond: &query.Conditions{Q: "second in a", Authors: []string{}}}
	res, err := h.S.TagAggCommits(*in, "", true)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := h.S.Rows(res.ResultID, 0, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("commit: %+v, %v", rows, err)
	}
	for _, src := range []session.DiffSource{
		{Kind: "commit", Repo: "repo-a", Hash: rows[0].Hash, Path: "a.txt"},
		{Kind: "wc", Repo: "repo-a", Worktree: filepath.Join(root, "repo-a"), Path: "a.txt"},
		{Kind: "agg", Repo: "repo-a", Row: "repo-a", Agg: in, Path: "a.txt"},
	} {
		t.Run(src.Kind, func(t *testing.T) {
			w := New(h.S, src, h.Update)
			t.Cleanup(w.Close)
			tt := ui.NewTester(w.View, 1100, 620)
			for range 400 {
				h.Drain()
				tt.Frame()
				if w.loaded && w.diff.Diff() != nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !w.loaded || w.diff.Diff() == nil {
				t.Fatalf("source did not load: %q", tt.Texts())
			}
			for _, locale := range []string{"zh-Hans", "en-US"} {
				tt.SetLocale(locale)
				for _, mode := range []struct {
					name   string
					layout editor.DiffLayout
				}{
					{"split", editor.DiffSplit},
					{"unified", editor.DiffUnified},
				} {
					label := "Split"
					if mode.layout == editor.DiffUnified {
						label = "Unified"
					}
					if locale == "zh-Hans" {
						label = "并排"
						if mode.layout == editor.DiffUnified {
							label = "单列"
						}
					}
					tt.Frame()
					if err := tt.Click(label); err != nil {
						t.Fatal(err)
					}
					tt.Frame()
					if w.diff.Layout != mode.layout || w.diff.Diff().Layout != mode.layout {
						t.Fatalf("layout %v, viewer %v, want %v", w.diff.Layout, w.diff.Diff().Layout, mode.layout)
					}
					hr := &viewtest.Harness{T: t, H: h, TT: tt}
					hr.Shot("diffwin-loaded-" + src.Kind + "-" + mode.name + "-" + locale)
				}
			}
		})
	}
}

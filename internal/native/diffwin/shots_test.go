package diffwin

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
)

func shotWindow(t *testing.T, src session.DiffSource, files []diffview.File, commit *session.CommitDetail) *Window {
	t.Helper()
	_, cancel := context.WithCancel(context.Background())
	w := &Window{cancel: cancel, src: src, loaded: true, listW: 250, p: plan{files: files, commit: commit}, diff: diffview.New(files, nil)}
	w.tree.Selected = &w.sel
	w.tree.OnSelect = func(i int) { w.diff.Select(i) }
	w.diff.OnSelect = func(i int) { w.sel = i }
	t.Cleanup(w.Close)
	return w
}

func TestShots(t *testing.T) {
	commit := &session.CommitDetail{Hash: "e5cd1ac0123456789", Subject: "Keep configuration paths readable", Author: "Ada", Time: 1775000000}
	files := []diffview.File{
		{Path: "internal/config/settings.go", Status: "M", Texts: &diffview.Texts{Old: "package config\n\nconst name = \"old\"\n", New: "package config\n\nconst name = \"new\"\n"}},
		{Path: "notes.txt", Status: "A", Texts: &diffview.Texts{New: "A new note\n"}},
	}
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, scene := range []struct {
			name string
			kind string
			n    int
		}{
			{"commit-single", "commit", 1},
			{"commit-multiple", "commit", 2},
			{"working-copy", "wc", 1},
			{"range", "agg", 1},
		} {
			t.Run(locale+"/"+scene.name, func(t *testing.T) {
				var head *session.CommitDetail
				if scene.kind == "commit" {
					head = commit
				}
				sceneFiles := append([]diffview.File(nil), files[:scene.n]...)
				if scene.kind == "agg" {
					for i := range sceneFiles {
						sceneFiles[i].Source = session.AggFile{Key: "sheaf"}
					}
				}
				src := session.DiffSource{Kind: scene.kind, Repo: "sheaf", Hash: commit.Hash, Path: files[0].Path}
				if scene.kind == "agg" {
					src.Agg = &session.TagAggInput{Requirement: "1234"}
				}
				w := shotWindow(t, src, sceneFiles, head)
				w.p.rows = []session.AggRepo{{Key: "sheaf", Base: "main", Branch: "feature/keep-configuration-paths-readable"}}
				tt := ui.NewTester(w.View, 1100, 620)
				tt.SetLocale(locale)
				tt.Frame()
				h := &viewtest.Harness{T: t, H: &viewtest.Host{}, TT: tt}
				name := "diffwin-" + scene.name + "-" + locale
				h.Shot(name)
				if scene.kind == "agg" {
					tt.SetSize(720, 420)
					h.Shot(name + "-narrow")
				}
			})
		}
	}
}

func TestShotsMultiRepo(t *testing.T) {
	files := []diffview.File{
		{Path: "internal/config/settings.go", Status: "M", Source: session.AggFile{Key: "payment-api", Repo: "payment-api"}, Texts: &diffview.Texts{Old: "package config\n\nconst name = \"old\"\n", New: "package config\n\nconst name = \"new\"\n"}},
		{Path: "src/checkout.ts", Status: "A", Source: session.AggFile{Key: "checkout-web", Repo: "checkout-web"}, Texts: &diffview.Texts{New: "export const checkout = true;\n"}},
	}
	src := session.DiffSource{Kind: "agg", Repo: "payment-api", Path: files[0].Path, Agg: &session.TagAggInput{Requirement: "PAY-142"}}
	w := shotWindow(t, src, files, nil)
	w.p.rows = []session.AggRepo{
		{Key: "payment-api", Repo: "payment-api", Base: "main", Branch: "feat-PAY-142"},
		{Key: "checkout-web", Repo: "checkout-web", Base: "main", Branch: "feat-PAY-142"},
	}
	w.tree.Group = byRow
	tt := ui.NewTester(w.View, 1100, 620)
	tt.SetLocale("zh-Hans")
	h := &viewtest.Harness{T: t, H: &viewtest.Host{}, TT: tt}
	h.Shot("diffwin-multi-repo-zh-Hans")
}

// Changes a few lines apart: the second one, reached with Next change, is marked.
func TestShotsCloseChanges(t *testing.T) {
	old := "package config\n\nconst a = 1\nconst b = 2\nconst c = 3\nconst d = 4\nconst e = 5\nconst f = 6\n"
	nw := "package config\n\nconst a = 10\nconst b = 2\nconst c = 30\nconst d = 4\nconst e = 50\nconst f = 6\n"
	files := []diffview.File{{Path: "internal/config/values.go", Status: "M", Texts: &diffview.Texts{Old: old, New: nw}}}
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			w := shotWindow(t, session.DiffSource{Kind: "wc", Worktree: "/src/sheaf", Path: files[0].Path}, files, nil)
			tt := ui.NewTester(w.View, 1100, 420)
			tt.SetLocale(locale)
			tt.Frame()
			w.diff.NextChange()
			tt.Frame()
			tt.Frame()
			h := &viewtest.Harness{T: t, H: &viewtest.Host{}, TT: tt}
			h.Shot("diffwin-close-changes-" + locale)
		})
	}
}

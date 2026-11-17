package diffwin

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

func TestTitleNamesTheSourceNotTheFile(t *testing.T) {
	commit := &session.CommitDetail{Hash: "e5cd1ac0123456789", Subject: "Update configuration"}
	for _, locale := range []string{"zh-Hans", "en-US"} {
		words := tr.For(locale)
		for _, tc := range []struct {
			name      string
			src       session.DiffSource
			commit    *session.CommitDetail
			main, sub string
		}{
			{"commit", session.DiffSource{Kind: "commit"}, commit, "Update configuration", "e5cd1ac"},
			{"commit without detail", session.DiffSource{Kind: "commit", Hash: "e5cd1ac0123456789"}, nil, words.Commit, "e5cd1ac"},
			{"worktree", session.DiffSource{Kind: "wc", Worktree: "/src/sheaf"}, nil, words.Wc, "sheaf"},
			{"whole", session.DiffSource{Kind: "agg", Agg: &session.TagAggInput{}}, nil, words.Whole, fmt.Sprintf(words.Range, "main", "feature")},
			{"requirement", session.DiffSource{Kind: "agg", Agg: &session.TagAggInput{Requirement: "1234"}}, nil,
				fmt.Sprintf(words.Req, "1234", words.Whole), fmt.Sprintf(words.Range, "main", "feature")},
			{"stage", session.DiffSource{Kind: "agg", Agg: &session.TagAggInput{DiffMode: session.DiffStage}}, nil, fmt.Sprintf(words.Stage, "test"), ""},
			{"uncommitted section", session.DiffSource{Kind: "agg", Only: true, Agg: &session.TagAggInput{}}, nil, words.Wc, ""},
		} {
			t.Run(locale+"/"+tc.name, func(t *testing.T) {
				tc.src.Path = "internal/config/settings.go"
				w := shotWindow(t, tc.src, []diffview.File{{Path: tc.src.Path, Source: session.AggFile{Key: "repo"}}}, tc.commit)
				w.p.rows = []session.AggRepo{{Key: "repo", Base: "main", Branch: "feature"}}
				w.p.stage = "test"
				if main, sub := w.heading(words); main != tc.main || sub != tc.sub {
					t.Fatalf("title %q / %q, want %q / %q", main, sub, tc.main, tc.sub)
				}
			})
		}
	}
}

func TestStatusBarKeepsOnlyWhatTheTitleLacks(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		words := tr.For(locale)
		for _, tc := range []struct {
			name   string
			files  int
			commit bool
		}{
			{"commit", 1, true},
			{"commit with several files", 2, true},
			{"one file", 1, false},
			{"several files", 2, false},
			{"no files", 0, false},
		} {
			t.Run(locale+"/"+tc.name, func(t *testing.T) {
				var files []diffview.File
				for i := range tc.files {
					files = append(files, diffview.File{Path: fmt.Sprintf("%d.txt", i)})
				}
				var commit *session.CommitDetail
				if tc.commit {
					commit = &session.CommitDetail{Hash: "e5cd1ac0123456789", Subject: "Update configuration", Author: "Ada", Time: 1775000000}
				}
				w := shotWindow(t, session.DiffSource{Kind: "wc"}, files, commit)
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(theme.Theme())
					ui.Row(c).Fill().Children(func() {
						if status := w.status(); status != nil {
							status(c)
						}
					})
				}, 900, 100)
				tt.SetLocale(locale)
				tt.Frame()
				var want []string
				if tc.commit {
					want = append(want, "Ada", time.Unix(1775000000, 0).Format("2006/01/02 15:04"))
				}
				if tc.files > 1 {
					want = append(want, words.StepHint)
				}
				if got := tt.Texts(); !slices.Equal(got, want) {
					t.Fatalf("status %q, want %q", got, want)
				}
				w.loaded = false
				tt.Frame()
				if got := tt.Texts(); len(got) != 0 {
					t.Fatalf("status before loading: %q", got)
				}
				w.loaded, w.err = true, "unavailable"
				tt.Frame()
				if got := tt.Texts(); len(got) != 0 {
					t.Fatalf("status after an error: %q", got)
				}
			})
		}
	}
}

func TestRangeFollowsTheSelectedFile(t *testing.T) {
	w := shotWindow(t, session.DiffSource{Kind: "agg", Agg: &session.TagAggInput{}}, []diffview.File{
		{Path: "a.txt", Source: session.AggFile{Key: "a"}},
		{Path: "b.txt", Source: session.AggFile{Key: "b"}},
		{Path: "c.txt", Source: session.AggFile{Key: "c"}},
	}, nil)
	w.p.rows = []session.AggRepo{
		{Key: "a", Base: "main", Branch: "feature-a"},
		{Key: "b", Base: "release", Branch: "feature-b"},
		{Key: "c", BranchFrom: session.FromMerged},
	}
	words := tr.For("en-US")
	for _, tc := range []struct {
		index int
		want  string
	}{
		{0, "main…feature-a"},
		{1, "release…feature-b"},
		{2, ""},
	} {
		w.diff.Select(tc.index)
		if _, got := w.heading(words); got != tc.want {
			t.Errorf("file %d: range %q, want %q", tc.index, got, tc.want)
		}
	}
}

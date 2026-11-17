package diffwin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/query"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

type fixture struct {
	s      *session.Session
	dir    string
	second string
}

func open(t *testing.T) fixture {
	t.Helper()
	testutil.Isolate(t)
	dir := filepath.Join(t.TempDir(), "app")
	r := testutil.NewRepo(t, dir)
	r.Commit("add", map[string]string{"a.txt": "1\n2\n3\n", "b.txt": "b\n"})
	second := r.Commit("edit", map[string]string{"a.txt": "1\n2\nthree\n", "c.txt": "c\n"})
	s, err := session.Open(context.Background(), dir, session.Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return fixture{s: s, dir: dir, second: second}
}

func TestPlanOfACommitStartsAtTheAskedFile(t *testing.T) {
	f := open(t)
	ctx := context.Background()
	src := session.DiffSource{Kind: "commit", Repo: f.s.Name, Hash: f.second, Path: "c.txt"}
	p, err := planOf(ctx, f.s, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.files) != 2 || p.files[p.current].Path != "c.txt" || p.commit == nil || p.commit.Subject != "edit" {
		t.Fatalf("plan %+v", p)
	}
	tx, err := p.load(ctx, p.files[indexOf(p.files, func(x diffview.File) bool { return x.Path == "a.txt" })])
	if err != nil || tx.Old != "1\n2\n3\n" || tx.New != "1\n2\nthree\n" {
		t.Errorf("a.txt: %+v, %v", tx, err)
	}
}

func TestPlanOfUncommitted(t *testing.T) {
	f := open(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(f.dir, "b.txt"), []byte("b\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := planOf(ctx, f.s, session.DiffSource{Kind: "wc", Repo: f.s.Name, Worktree: f.dir, Path: "new.txt", Area: "untracked"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.files) != 2 || p.files[p.current].Path != "new.txt" || p.files[p.current].Source.(session.FileStatus).Area != "untracked" {
		t.Fatalf("wc plan %+v", p)
	}
	if tx, err := p.load(ctx, p.files[p.current]); err != nil || tx.New != "n\n" || tx.Old != "" {
		t.Errorf("new.txt: %+v, %v", tx, err)
	}
}

func TestUnsupportedSource(t *testing.T) {
	for _, kind := range []string{"cur", "history", "unknown", ""} {
		if _, err := planOf(context.Background(), nil, session.DiffSource{Kind: kind}); !errors.Is(err, errUnsupported) {
			t.Errorf("source %q: %v", kind, err)
		}
	}
}

func TestPlanOfAFilterStepsThroughItsFilesWithTheMergedDiff(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	r.Git("branch", "-M", "master")
	r.Git("checkout", "-q", "-b", "feat")
	r.Commit("feat adds", map[string]string{"new.txt": "n\n", "a.txt": "a\nb\n"})
	r.Git("checkout", "-q", "master")
	s, err := session.Open(ctx, root, session.Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	agg := &session.TagAggInput{Base: "master", Cond: &query.Conditions{Authors: []string{}, Branches: []string{"feat"}}}
	p, err := planOf(ctx, s, session.DiffSource{Kind: "agg", Repo: "r", Row: "r", Agg: agg, Path: "new.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.files) != 2 || p.files[p.current].Path != "new.txt" {
		t.Fatalf("plan %+v", p)
	}
	// The branch diff is one patch, without a title.
	if tx, err := p.load(ctx, p.files[p.current]); err != nil || len(tx.Parts) != 1 || tx.Parts[0].Title != "" || len(tx.Parts[0].Patch.Hunks) != 1 {
		t.Errorf("new.txt: %+v, %v", tx, err)
	} else if ls := tx.Parts[0].Patch.Hunks[0].Lines; len(ls) != 1 || ls[0].Kind != "+" || ls[0].Text != "n" {
		t.Errorf("new.txt lines %+v", ls)
	}
	// Every row's files: the tree heads them with their row.
	all, err := planOf(ctx, s, session.DiffSource{Kind: "agg", Repo: "r", Row: "r", Agg: agg, Path: "new.txt", All: true})
	if err != nil {
		t.Fatal(err)
	}
	if all.group == nil {
		t.Fatal("all rows: no headings")
	}
	if key, name := all.group(all.files[0]); key != "r" || name != "r" {
		t.Errorf("all rows: heading %q %q", key, name)
	}
	if _, err := planOf(ctx, s, session.DiffSource{Kind: "agg"}); err == nil {
		t.Error("agg without a filter must fail")
	}
}

// A repo without a branch diff (its branch merged into the base) shows every commit of the filter that changed the
// file, oldest first, each under its commit; not only the newest.
func TestPlanOfAFilterShowsEveryCommitWithoutABranchDiff(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	r.Git("branch", "-M", "master")
	r.Git("checkout", "-q", "-b", "feat")
	r.Commit("YT9 first", map[string]string{"a.txt": "a\nb\n"})
	r.Commit("YT9 second", map[string]string{"a.txt": "a\nb\nc\n"})
	r.Git("checkout", "-q", "master")
	r.Git("merge", "-q", "--ff-only", "feat")
	s, err := session.Open(ctx, root, session.Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	agg := &session.TagAggInput{Base: "master", Cond: &query.Conditions{Q: "YT9", Authors: []string{}}}
	p, err := planOf(ctx, s, session.DiffSource{Kind: "agg", Repo: "r", Row: "r", Agg: agg, Path: "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.files) != 1 {
		t.Fatalf("plan %+v", p)
	}
	tx, err := p.load(ctx, p.files[0])
	if err != nil || len(tx.Parts) != 2 {
		t.Fatalf("a.txt: %+v, %v", tx, err)
	}
	for i, want := range []string{"YT9 first", "YT9 second"} {
		if title := tx.Parts[i].Title; len(title) < 9 || title[9:] != want {
			t.Errorf("part %d title %q, want hash + %q", i, title, want)
		}
	}
	if ls := tx.Parts[1].Patch.Hunks[0].Lines; ls[len(ls)-1].Kind != "+" || ls[len(ls)-1].Text != "c" {
		t.Errorf("second part lines %+v", ls)
	}
}

func TestRequirementDiffWindowKeepsItsComparisonMode(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
	repo.Commit("init", map[string]string{"file.txt": "base\n"})
	repo.Git("checkout", "-q", "-b", "feat-ABC-1234")
	repo.Commit("ABC-1234 landed", map[string]string{"file.txt": "base\nlanded\n"})
	repo.Git("branch", "release")
	repo.Commit("ABC-1234 pending", map[string]string{"file.txt": "base\nlanded\npending\n"})
	if err := os.WriteFile(filepath.Join(repo.Dir, "file.txt"), []byte("base\nlanded\npending\ndirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := session.Open(t.Context(), root, session.Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.Close)
	if err := sess.SetMergeFlow([]session.MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		only  bool
		mode  string
		added []string
	}{
		{false, session.DiffWorktree, []string{"dirty"}},
		{false, session.DiffStage, []string{"pending"}},
		{false, session.DiffBase, []string{"landed", "pending"}},
		{true, session.DiffBase, []string{"dirty"}},
		{true, session.DiffStage, []string{"dirty"}},
	} {
		t.Run(scenario.mode, func(t *testing.T) {
			input := &session.TagAggInput{Requirement: "ABC-1234", Cond: &query.Conditions{Q: "ABC-1234", Authors: []string{}}, Base: "master", DiffMode: scenario.mode}
			plan, err := planOf(t.Context(), sess, session.DiffSource{Kind: "agg", Repo: "repo", Agg: input, Path: "file.txt", Only: scenario.only})
			if err != nil || len(plan.files) != 1 {
				t.Fatalf("files: %+v, %v", plan.files, err)
			}
			texts, err := plan.load(t.Context(), plan.files[plan.current])
			if err != nil {
				t.Fatal(err)
			}
			var added []string
			for _, part := range texts.Parts {
				for _, hunk := range part.Patch.Hunks {
					for _, line := range hunk.Lines {
						if line.Kind == "+" {
							added = append(added, line.Text)
						}
					}
				}
			}
			if !slices.Equal(added, scenario.added) {
				t.Errorf("window diff additions %v, want %v", added, scenario.added)
			}
			if scenario.mode == session.DiffStage && !scenario.only && plan.stage != sess.MergeFlow()[0].Name {
				t.Errorf("window lost stage context: %q", plan.stage)
			}
		})
	}
}

func TestWindowShowsTheFilesAndStepsWithAltArrows(t *testing.T) {
	f := open(t)
	var mu sync.Mutex
	var queue []func()
	w := New(f.s, session.DiffSource{Kind: "commit", Repo: f.s.Name, Hash: f.second, Path: "a.txt"}, func(fn func()) {
		mu.Lock()
		queue = append(queue, fn)
		mu.Unlock()
	})
	t.Cleanup(w.Close)
	tt := ui.NewTester(w.View, 1000, 600)
	tt.SetLocale("zh-Hans")
	pump := func(until func() bool) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			mu.Lock()
			fs := queue
			queue = nil
			mu.Unlock()
			for _, fn := range fs {
				fn()
			}
			tt.Frame()
			if until() {
				return
			}
		}
		t.Fatalf("texts %q", tt.Texts())
	}
	pump(func() bool { return tt.HasText("c.txt") && tt.HasText("⌥↑ ⌥↓ 切换文件") })
	if tt.HasText("来自：提交 " + short(f.second)) {
		t.Fatal("the footer repeats the commit header")
	}
	if w.diff.Current() != 0 {
		t.Fatalf("shows file %d, want a.txt first", w.diff.Current())
	}
	w.diff.NextFile()
	pump(func() bool { return w.sel == 1 })
	if got := w.p.files[w.diff.Current()].Path; got != "c.txt" {
		t.Errorf("after stepping: %s", got)
	}
	// A click in the tree shows that file.
	if err := tt.Click("+1 −1"); err != nil { // a.txt's row (the title says a.txt too)
		t.Fatal(err)
	}
	pump(func() bool { return w.sel == 0 && w.diff.Current() == 0 })
}

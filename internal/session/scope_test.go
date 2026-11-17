package session

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestOpenIgnoresLegacyScope(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	for _, name := range []string{"repo-a", "repo-b", "ignored"} {
		testutil.NewRepo(t, filepath.Join(root, name)).Commit("init", map[string]string{"a": "1\n"})
	}
	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir()}
	data := filepath.Join(opts.DataDir, "workspaces", workspaceID(root))
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "workspace.json"), []byte(`{"scope":"repo-b","pinned":["repo-b"],"order":["repo-b","repo-a"],"ignored":["ignored"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"first index", "snapshot"} {
		t.Run(run, func(t *testing.T) {
			s, err := Open(ctx, root, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if run == "first index" {
				if err := s.Refresh(ctx); err != nil {
					t.Fatal(err)
				}
			}
			info := s.Info()
			if !info.State.Ready || info.State.Commits != 2 || len(info.Repos) != 2 || info.Repos[0].Name != "repo-a" || info.Repos[1].Name != "repo-b" {
				t.Fatalf("repos = %+v, state = %+v", info.Repos, info.State)
			}
			if !slices.Equal(info.Ignored, []string{"ignored"}) {
				t.Errorf("ignored = %v", info.Ignored)
			}
			q := mustQuery(t, s, query.Conditions{Paths: []string{"repo-a"}})
			if rows, err := s.Rows(q.ID, 0, 10); err != nil || len(rows) != 1 || rows[0].Repo != "repo-a" {
				t.Errorf("filtered rows = %+v, %v", rows, err)
			}
		})
	}
}

// A refresh whose scan began before the watch read a checkout keeps the checkout; of two refreshes the older scan never
// replaces the newer one.
func TestScanOrder(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	a := testutil.NewRepo(t, filepath.Join(root, "repo-a"))
	a.Commit("in a", map[string]string{"a": "1\n"})
	wt := filepath.Join(root, "repo-a-wt")
	a.Git("worktree", "add", "-q", "-b", "feat", wt)
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	s.bg.Wait()
	branch := func() string {
		for _, r := range s.Info().Repos {
			for _, w := range r.Worktrees {
				if !w.Main {
					return w.Branch
				}
			}
		}
		return ""
	}

	seq := s.beginScan()
	before, err := discover.Scan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	testutil.NewRepoAt(t, wt).Git("checkout", "-q", "-b", "other")
	if err := s.UpdateRepos(ctx, []string{"repo-a"}); err != nil || branch() != "other" {
		t.Fatalf("watch update: %v, branch %q", err, branch())
	}
	if err := s.applyScan(ctx, seq, before, nil); err != nil || branch() != "other" {
		t.Errorf("after the older scan: %v, branch %q, want other", err, branch())
	}

	older := s.beginScan()
	scanOld, _ := discover.Scan(ctx, root)
	testutil.NewRepo(t, filepath.Join(root, "repo-c")).Commit("in c", map[string]string{"c": "1\n"})
	newer := s.beginScan()
	scanNew, _ := discover.Scan(ctx, root)
	s.applyScan(ctx, newer, scanNew, nil)
	s.applyScan(ctx, older, scanOld, nil)
	if info := s.Info(); len(info.Repos) != 2 || info.State.Indexing {
		t.Errorf("after the newer scan then the older: %d repos, indexing %v", len(info.Repos), info.State.Indexing)
	}
}

// A folder that is itself a repo opens as a workspace of that one repo.
func TestOpenSingleRepoFolder(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "solo")
	testutil.NewRepo(t, root).Commit("init", map[string]string{"a": "1\n"})
	if has, err := HasRepos(ctx, root); err != nil || !has {
		t.Fatalf("HasRepos = %v, %v", has, err)
	}
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if info := s.Info(); len(info.Repos) != 1 || info.Repos[0].Name != "solo" || info.State.Commits != 1 {
		t.Errorf("info: repos=%+v commits=%d", info.Repos, info.State.Commits)
	}
}

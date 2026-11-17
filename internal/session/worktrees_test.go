package session

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"sheaf/internal/testutil"
)

func TestWorktrees(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	a := testutil.NewRepo(t, filepath.Join(root, "app"))
	a.Commit("init", map[string]string{"a.txt": "a\n"})
	a.Git("tag", "v1")
	a.Git("config", "user.email", "Work@Corp.example")
	wt := filepath.Join(root, "app-feat")
	a.Git("worktree", "add", "-q", "-b", "feat", wt)
	w := testutil.NewRepoAt(t, wt)
	w.Commit("feat one", map[string]string{"f.txt": "1\n"})
	w.Commit("feat two", map[string]string{"f.txt": "2\n"})
	a.Commit("master moves", map[string]string{"a.txt": "b\n"})
	os.WriteFile(filepath.Join(wt, "f.txt"), []byte("3\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)

	var mu sync.Mutex // pushes come from background goroutines
	var pushed []string
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), OnWorktrees: func(ws WorktreeStatus) {
		mu.Lock()
		pushed = append(append(pushed, ws.Worktrees...), ws.Read...)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	// "me": the global email plus each repo's own (until the user picks)
	if me := s.Info().Me; !slices.Equal(me, []string{"me@example.com", "work@corp.example"}) {
		t.Errorf("default me: %v", me)
	}
	wtOf := func() WorktreeInfo {
		for _, r := range s.Info().Repos {
			for _, x := range r.Worktrees {
				if x.Branch == "feat" {
					return x
				}
			}
		}
		t.Fatal("feat worktree missing")
		return WorktreeInfo{}
	}
	// no base set: the local master is derived (no origin/master here)
	if r := s.Info().Repos[0]; r.Base != "master" || !r.BaseAuto {
		t.Errorf("derived base: %+v", r)
	}
	if x := wtOf(); x.Ahead != 2 || x.Behind != 1 {
		t.Errorf("derived base: want 2/1, got %+v", x)
	}
	// a remote master wins over the local one; a base the user set wins over both
	a.Git("update-ref", "refs/remotes/origin/master", "master~1")
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if r := s.Info().Repos[0]; r.Base != "origin/master" || !r.BaseAuto {
		t.Errorf("derived remote base: %+v", r)
	}
	if x := wtOf(); x.Ahead != 2 || x.Behind != 0 {
		t.Errorf("derived remote base: want 2/0, got %+v", x)
	}
	if err := s.SetBase("app", "master"); err != nil {
		t.Fatal(err)
	}
	if r := s.Info().Repos[0]; r.Base != "master" || r.BaseAuto {
		t.Errorf("set base: %+v", r)
	}
	if x := wtOf(); x.Ahead != 2 || x.Behind != 1 {
		t.Errorf("ahead/behind: %+v", x)
	}
	s.RefreshDirty(ctx, nil, false)
	if x := wtOf(); x.Dirty != 2 {
		t.Errorf("dirty: %+v", x)
	}
	mu.Lock()
	n := len(pushed)
	mu.Unlock()
	if n == 0 {
		t.Error("WorktreeStatusChanged not pushed")
	}

	wt = wtOf().Path // git reports linked worktrees with symlinks resolved (/private/var)
	bs, err := s.Branches("app")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]BranchView{}
	for _, b := range bs {
		got[b.Kind+" "+b.Name] = b
	}
	if got["local feat"].Worktree != wt || got["local master"].Worktree == "" || got["tag v1"].Kind != "tag" {
		t.Errorf("branches: %+v", bs)
	}

	fs, err := s.Uncommitted(ctx, wt)
	if err != nil || len(fs) != 2 || fs[0].Area != "unstaged" || fs[1].Area != "untracked" {
		t.Fatalf("uncommitted: %+v %v", fs, err)
	}
	p, err := s.UncommittedPatch(ctx, wt, "new.txt", "", "untracked", false)
	if err != nil || len(p.Hunks) != 1 {
		t.Errorf("untracked patch: %+v %v", p, err)
	}
	if _, err := s.Uncommitted(ctx, t.TempDir()); err == nil {
		t.Error("a directory outside the workspace was read")
	}

	clean := testutil.NewRepo(t, filepath.Join(root, "clean"))
	clean.Commit("c", nil)
	s.Refresh(ctx)
	if files, err := s.Uncommitted(ctx, clean.Dir); err != nil || len(files) != 0 {
		t.Errorf("clean worktree: %+v %v", files, err)
	}
}

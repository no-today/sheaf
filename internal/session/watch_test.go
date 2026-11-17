package session

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"sheaf/internal/testutil"
	"sheaf/internal/watch"
)

func TestUpdateReposReadsDirtyCountsForNewWorktrees(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	updates := make(chan WorktreeStatus, 4)
	s, err := Open(t.Context(), root, Options{
		DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true,
		OnWorktrees: func(ws WorktreeStatus) { updates <- ws },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	r.Git("worktree", "add", "-q", "-b", "feat", linked)
	linked, err = filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRepos(t.Context(), []string{"repo"}); err != nil {
		t.Fatal(err)
	}
	s.bg.Wait()
	select {
	case ws := <-updates:
		if got := append(ws.Worktrees, ws.Read...); !slices.Equal(got, []string{linked}) {
			t.Fatalf("status update = %+v, want only the new worktree", ws)
		}
	default:
		t.Fatal("new worktree status was not read after UpdateRepos")
	}
	for _, wt := range s.Info().Repos[0].Worktrees {
		if wt.Path == linked {
			if wt.Dirty != 2 {
				t.Errorf("new worktree dirty = %d, want 2", wt.Dirty)
			}
			return
		}
	}
	t.Fatal("new worktree missing from Info")
}

func TestWatch(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	a := testutil.NewRepo(t, filepath.Join(root, "a"))
	a.Commit("a init", map[string]string{"a.txt": "a\n"})
	b := testutil.NewRepo(t, filepath.Join(root, "b"))
	b.Commit("b init", map[string]string{"b.txt": "b\n"})

	warm, _ := watch.Files(ctx, []string{t.TempDir()}) // mygo starts its shared goroutines once
	warm.Close()
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()

	var mu sync.Mutex
	var idx [][]string
	var wts []string
	s, err := Open(ctx, root, Options{
		DataDir: t.TempDir(), CacheDir: t.TempDir(), Watch: watch.Files,
		OnIndex:     func(u IndexUpdate) { mu.Lock(); idx = append(idx, u.ChangedRepos); mu.Unlock() },
		OnWorktrees: func(w WorktreeStatus) { mu.Lock(); wts = append(wts, w.Worktrees...); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // first status run and its pushes
	mu.Lock()
	idx, wts = nil, nil
	mu.Unlock()
	wait := func(name string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(4 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			mu.Lock()
			done := ok()
			mu.Unlock()
			if done {
				return
			}
		}
		t.Errorf("%s: timed out (index %v, worktrees %v)", name, idx, wts)
	}

	t0 := time.Now()
	os.WriteFile(filepath.Join(b.Dir, "new.txt"), []byte("n\n"), 0o644)
	wait("edit -> WorktreeStatusChanged", func() bool { return slices.Contains(wts, b.Dir) })
	t.Logf("edit pushed after %v", time.Since(t0).Round(time.Millisecond))
	if n := s.Info().Repos[1].Worktrees[0].Dirty; n != 1 {
		t.Errorf("dirty after edit: %d", n)
	}

	t1 := time.Now()
	a.Commit("a two", map[string]string{"a.txt": "a2\n"})
	wait("commit -> IndexUpdated{a}", func() bool {
		return slices.ContainsFunc(idx, func(c []string) bool { return slices.Equal(c, []string{"a"}) })
	})
	t.Logf("commit pushed after %v", time.Since(t1).Round(time.Millisecond))
	if n := s.Info().State.Commits; n != 3 {
		t.Errorf("commits after update: %d", n)
	}

	a.Git("checkout", "-q", "-b", "other") // no new commit, the branch moved
	wait("checkout -> IndexUpdated", func() bool { return len(idx) >= 2 })
	if br := s.Info().Repos[0].Worktrees[0].Branch; br != "other" {
		t.Errorf("branch after checkout: %q", br)
	}

	s.Close()
	for i := 0; i < 100 && runtime.NumGoroutine() > before; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		buf := make([]byte, 1<<16)
		t.Errorf("goroutines leaked: %d before, %d after Close\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
}

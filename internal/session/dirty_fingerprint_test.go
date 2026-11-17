package session

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

func TestWatchDirtyFingerprint(t *testing.T) {
	testutil.Isolate(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "ws", "app"))
	repo.Commit("init", map[string]string{".gitignore": "ignored.log\n", "edit.txt": "before\n", "clean.txt": "clean\n"})
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo.Dir, path), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("edit.txt", "modified\n")
	var mu sync.Mutex
	var pushed, read []string
	s, err := Open(t.Context(), filepath.Dir(repo.Dir), Options{
		DataDir: t.TempDir(), CacheDir: t.TempDir(),
		OnWorktrees: func(ws WorktreeStatus) {
			mu.Lock()
			pushed = append(pushed, ws.Worktrees...)
			read = append(read, ws.Read...)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.bg.Wait()
	repo.Dir = s.Info().Repos[0].Worktrees[0].Path
	// Opening reads every status once; a diff already on screen came from the same files, so nothing changed.
	mu.Lock()
	if len(pushed) > 0 || !slices.Contains(read, repo.Dir) {
		t.Fatalf("first status after open: changed %v, read %v", pushed, read)
	}
	mu.Unlock()
	s.RefreshDirty(t.Context(), []string{repo.Dir}, false)
	take := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := pushed
		pushed = nil
		return out
	}
	take()
	check := func(t *testing.T, want bool) {
		t.Helper()
		got := take()
		if want && !slices.Equal(got, []string{repo.Dir}) || !want && len(got) > 0 {
			t.Fatalf("OnWorktrees = %v, want changed=%v", got, want)
		}
	}
	for _, tc := range []struct {
		name string
		hint string
		edit func()
		want bool
	}{
		{"ignored file", "", func() { write("ignored.log", "build output\n") }, false},
		{"git status rewrites index", ".git", func() {
			old := time.Unix(1_600_000_000, 0)
			for _, path := range []string{"clean.txt", ".git/index"} {
				if err := os.Chtimes(filepath.Join(repo.Dir, path), old, old); err != nil {
					t.Fatal(err)
				}
			}
			repo.Git("status", "--porcelain=v2")
			info, err := os.Stat(filepath.Join(repo.Dir, ".git/index"))
			if err != nil {
				t.Fatal(err)
			}
			if info.ModTime().Equal(old) {
				t.Fatal("git status did not rewrite the index")
			}
		}, false},
		{"unrelated remote ref", ".git", func() { repo.Git("update-ref", "refs/remotes/origin/unrelated", "HEAD") }, false},
		{"append to modified file", "", func() { write("edit.txt", "modified\nmore\n") }, true},
		{"same size edit", "", func() {
			info, err := os.Stat(filepath.Join(repo.Dir, "edit.txt"))
			if err != nil {
				t.Fatal(err)
			}
			write("edit.txt", "modified\nMORE\n")
			later := info.ModTime().Add(time.Second)
			if err := os.Chtimes(filepath.Join(repo.Dir, "edit.txt"), later, later); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"new untracked file", "", func() { write("new.txt", "new\n") }, true},
		{"staging changes area", ".git", func() { repo.Git("add", "edit.txt") }, true},
		{"rename changes paths", ".git", func() { repo.Git("mv", "clean.txt", "renamed.txt") }, true},
		{"HEAD changes with same status", ".git", func() { repo.Git("commit", "--amend", "--only", "--allow-empty", "-qm", "changed HEAD") }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.edit()
			hint := resolvePath(filepath.Join(repo.Dir, tc.hint))
			s.applyChanges(t.Context(), []string{hint})
			check(t, tc.want)
			s.applyChanges(t.Context(), []string{hint})
			check(t, false)
		})
	}
	t.Run("discard reports", func(t *testing.T) {
		if err := s.Discard(t.Context(), repo.Dir, "new.txt"); err != nil {
			t.Fatal(err)
		}
		check(t, true)
		s.RefreshDirty(t.Context(), []string{repo.Dir}, false)
		check(t, false)
	})
	t.Run("forced report without diff change", func(t *testing.T) {
		s.RefreshDirty(t.Context(), []string{repo.Dir}, true)
		check(t, true)
	})
}

func TestDirtyFingerprintKeepsNewestRun(t *testing.T) {
	var d dirtyTracker
	older, newer := d.ticket(), d.ticket()
	first, second := [32]byte{1}, [32]byte{2}
	if changed, isFirst, stale := d.store("wt", newer, 2, second); changed || !isFirst || stale {
		t.Fatal("a first read must be reported as read, not as changed")
	}
	if changed, _, stale := d.store("wt", older, 1, first); changed || !stale {
		t.Fatal("older run replaced the fingerprint")
	}
	if changed, isFirst, stale := d.store("wt", d.ticket(), 2, second); changed || isFirst || stale {
		t.Fatal("unchanged newest fingerprint was reported")
	}
	if changed, _, _ := d.store("wt", d.ticket(), 2, first); !changed {
		t.Fatal("a changed fingerprint was not reported")
	}
	if d.get("wt") != 2 {
		t.Fatal("older run replaced the count")
	}
}

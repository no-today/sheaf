package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

func TestCommitTexts(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "app"))
	add := r.Commit("add", map[string]string{"a.txt": "one\ntwo\n"})
	edit := r.Commit("edit", map[string]string{"a.txt": "one\n2\nthree\n"})
	r.Git("mv", "a.txt", "b.txt")
	mv := r.Commit("rename", nil)
	del := r.Commit("delete", map[string]string{"b.txt": ""})

	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_000_000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, hash, path, oldPath, status string
		old, new                          []string
	}{
		{"root commit, added", add, "a.txt", "", "A", []string{}, []string{"one", "two"}},
		{"modified", edit, "a.txt", "", "M", []string{"one", "two"}, []string{"one", "2", "three"}},
		{"renamed: old side under the old path", mv, "b.txt", "a.txt", "R", []string{"one", "2", "three"}, []string{"one", "2", "three"}},
		{"deleted", del, "b.txt", "", "D", []string{"one", "2", "three"}, []string{}},
	} {
		got, err := s.CommitTexts(ctx, CommitKey{"app", c.hash}, c.path, c.oldPath, c.status)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got.Old.Lines, c.old) || !reflect.DeepEqual(got.New.Lines, c.new) {
			t.Errorf("%s: old %q new %q", c.name, got.Old.Lines, got.New.Lines)
		}
	}
	if _, err := s.CommitTexts(ctx, CommitKey{"app", edit}, "missing.txt", "", "M"); err == nil {
		t.Error("a path not in the commit read without error")
	}
}

func TestUncommittedTexts(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "app"))
	r.Commit("base", map[string]string{"a.txt": "one\n", "b.txt": "bee\n", "gone.txt": "x\n"})
	write := func(p, s string) {
		if err := os.WriteFile(filepath.Join(r.Dir, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "staged\n")
	r.Git("add", "a.txt")
	write("a.txt", "on disk\n")
	write("b.txt", "bee\nmore\n")
	write("new.txt", "fresh\n")
	r.Git("rm", "-q", "gone.txt")

	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_000_000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	wt := r.Dir
	for _, c := range []struct {
		name, path, area, status string
		old, new                 []string
	}{
		{"staged: HEAD against the index", "a.txt", "staged", "M", []string{"one"}, []string{"staged"}},
		{"unstaged: the index against the disk", "a.txt", "unstaged", "M", []string{"staged"}, []string{"on disk"}},
		{"unstaged, not staged before", "b.txt", "unstaged", "M", []string{"bee"}, []string{"bee", "more"}},
		{"untracked: nothing against the disk", "new.txt", "untracked", "?", []string{}, []string{"fresh"}},
		{"staged delete: no new side", "gone.txt", "staged", "D", []string{"x"}, []string{}},
		{"head: HEAD against the disk", "a.txt", "head", "M", []string{"one"}, []string{"on disk"}},
	} {
		got, err := s.UncommittedTexts(ctx, wt, c.path, "", c.area, c.status)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got.Old.Lines, c.old) || !reflect.DeepEqual(got.New.Lines, c.new) {
			t.Errorf("%s: old %q new %q", c.name, got.Old.Lines, got.New.Lines)
		}
	}
	if _, err := s.UncommittedTexts(ctx, t.TempDir(), "a.txt", "", "staged", "M"); err == nil {
		t.Error("a directory outside the workspace was read")
	}
	if _, err := s.UncommittedTexts(ctx, wt, "../x", "", "untracked", "?"); err == nil {
		t.Error("a path outside the worktree was read")
	}
}

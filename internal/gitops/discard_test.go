package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/gitcli"
	"sheaf/internal/testutil"
)

func TestDiscard(t *testing.T) {
	testutil.Isolate(t)
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "r"))
	r.Commit("init", map[string]string{"a.txt": "a\n", "b.txt": "b\n", "keep.txt": "keep\n", "old.txt": "old\n", "dir/c.txt": "c\n"})
	write := func(name, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(r.Dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a staged\n")
	r.Git("add", "a.txt")
	write("a.txt", "a staged then edited\n")
	write("b.txt", "b edited\n")
	write("keep.txt", "keep edited\n")
	write("new.txt", "untracked\n")
	write("dir/other.txt", "untracked too\n")
	write("staged-new.txt", "staged new\n")
	r.Git("add", "staged-new.txt")
	r.Git("mv", "old.txt", "moved.txt")
	write("[ab].txt", "a wildcard name\n")
	ctx := context.Background()
	for _, p := range []string{"a.txt", "b.txt", "new.txt", "staged-new.txt", "moved.txt", "[ab].txt"} {
		if err := Discard(ctx, r.Dir, p); err != nil {
			t.Fatalf("discard %s: %v", p, err)
		}
	}
	got := map[string]bool{}
	cs, err := gitcli.Status(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		got[c.Path] = true
	}
	if len(got) != 2 || !got["keep.txt"] || !got["dir/other.txt"] {
		t.Errorf("left uncommitted: %v; want only the files not discarded", got)
	}
	for name, want := range map[string]string{"a.txt": "a\n", "b.txt": "b\n", "old.txt": "old\n", "keep.txt": "keep edited\n"} {
		if b, err := os.ReadFile(filepath.Join(r.Dir, name)); err != nil || string(b) != want {
			t.Errorf("%s = %q (%v), want %q", name, b, err, want)
		}
	}
	for _, name := range []string{"new.txt", "staged-new.txt", "moved.txt", "[ab].txt"} {
		if _, err := os.Lstat(filepath.Join(r.Dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", name, err)
		}
	}
	if err := Discard(ctx, r.Dir, "dir"); !errors.Is(err, ErrNotUncommitted) {
		t.Errorf("a directory was discarded: %v", err)
	}
	if err := Discard(ctx, r.Dir, "dir/c.txt"); !errors.Is(err, ErrNotUncommitted) {
		t.Errorf("a clean file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, ".git", "MERGE_HEAD"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Discard(ctx, r.Dir, "keep.txt"); !errors.Is(err, ErrBusy) {
		t.Errorf("discarded mid-merge: %v", err)
	}
}

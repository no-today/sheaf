package diffview

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestCommitLoader(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "app")
	r := testutil.NewRepo(t, dir)
	r.Commit("add", map[string]string{"main.go": "package main\n", "old.txt": "x\n"})
	r.Git("mv", "old.txt", "new.txt")
	hash := r.Commit("edit", map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	s, err := session.Open(ctx, dir, session.Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	k := session.CommitKey{Repo: s.Name, Hash: hash}
	changes, err := s.Files(ctx, []session.CommitKey{k})
	if err != nil {
		t.Fatal(err)
	}
	load := CommitLoader(s, k)
	got := map[string]Texts{}
	for _, f := range FilesOf(changes) {
		tx, err := load(ctx, f)
		if err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		got[f.Path] = tx
	}
	if tx := got["main.go"]; tx.Old != "package main\n" || tx.New != "package main\n\nfunc main() {}\n" {
		t.Errorf("main.go: %+v", tx)
	}
	if tx := got["new.txt"]; tx.Old != "x\n" || tx.New != "x\n" {
		t.Errorf("renamed file: %+v", tx)
	}
}

func TestUncommittedLoader(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "app")
	r := testutil.NewRepo(t, dir)
	r.Commit("add", map[string]string{"main.go": "package main\n"})
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := session.Open(ctx, dir, session.Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	changes, err := s.Uncommitted(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	load := UncommittedLoader(s, dir)
	got := map[string]Texts{}
	for _, fs := range changes {
		f := File{Path: fs.Path, OldPath: fs.OldPath, Status: fs.Status, Source: fs}
		tx, err := load(ctx, f)
		if err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		got[f.Path] = tx
	}
	if tx := got["main.go"]; tx.Old != "package main\n" || tx.New != "package main\n\nfunc main() {}\n" {
		t.Errorf("main.go: %+v", tx)
	}
	if tx := got["new.txt"]; tx.Old != "" || tx.New != "fresh\n" {
		t.Errorf("untracked file: %+v", tx)
	}
}

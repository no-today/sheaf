package discover

import (
	"context"
	"path/filepath"
	"testing"

	"sheaf/internal/testutil"
)

func TestScan(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()

	a := testutil.NewRepo(t, filepath.Join(root, "a"))
	a.Commit("init", map[string]string{"x": "1"})
	a.Git("worktree", "add", "-q", "-b", "feat-1", filepath.Join(root, "a-feat"))

	lib := testutil.NewRepo(t, filepath.Join(t.TempDir(), "lib"))
	lib.Commit("lib", map[string]string{"l": "1"})
	b := testutil.NewRepo(t, filepath.Join(root, "b"))
	b.Commit("init", map[string]string{"y": "1"})
	b.Git("submodule", "add", "-q", lib.Dir, "sub/lib")
	b.Commit("add submodule", nil)

	n := testutil.NewRepo(t, filepath.Join(root, "group", "nested"))
	n.Commit("init", map[string]string{"z": "1"})
	testutil.NewRepo(t, filepath.Join(root, "b", "node_modules", "pkg"))

	repos, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Repo{}
	for _, r := range repos {
		got[r.Name] = r
	}
	if len(repos) != 4 {
		t.Fatalf("want a, b, b/sub/lib, group/nested; got %+v", repos)
	}
	if r := got["a"]; r.Kind != KindRepo || len(r.Worktrees) != 2 || r.Worktrees[1].Branch != "refs/heads/feat-1" || !r.Worktrees[0].Main {
		t.Errorf("a: %+v", r)
	}
	if r := got["b/sub/lib"]; r.Kind != KindSubmodule || r.Worktrees[0].Path != filepath.Join(root, "b", "sub", "lib") {
		t.Errorf("submodule: %+v", r)
	}
	if _, ok := got["group/nested"]; !ok {
		t.Error("nested repo missing")
	}
	if _, ok := got["a-feat"]; ok {
		t.Error("linked worktree reported as a repo")
	}
}

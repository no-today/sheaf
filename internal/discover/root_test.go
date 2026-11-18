package discover

import (
	"context"
	"path/filepath"
	"testing"

	"sheaf/internal/testutil"
)

func TestScanNestedRepoNames(t *testing.T) {
	testutil.Isolate(t)
	for _, tc := range []struct{ child, wantRoot string }{
		{"api", "."},
		{"lib", "api"},
	} {
		t.Run(tc.child, func(t *testing.T) {
			root := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))
			child := testutil.NewRepo(t, filepath.Join(root.Dir, tc.child))
			repos, err := Scan(t.Context(), root.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(repos) != 2 || repos[0].Name != tc.wantRoot || repos[0].Dir != root.Dir || repos[1].Name != tc.child || repos[1].Dir != child.Dir {
				t.Fatalf("sorted repos = %+v, want root %q followed by child %q", repos, tc.wantRoot, tc.child)
			}
		})
	}
}

// A directory that is itself a repo is a workspace of one repo (single-repo mode), whatever it is called, with its worktrees.
func TestScanRootIsRepo(t *testing.T) {
	testutil.Isolate(t)
	for _, tc := range []struct {
		dir       string
		worktrees int
	}{
		{"proj", 1},
		{"build", 1}, // a name SkipDir skips below the root
		{"wt", 2},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), tc.dir)
			r := testutil.NewRepo(t, root)
			r.Commit("init", map[string]string{"x": "1"})
			if tc.worktrees == 2 {
				r.Git("worktree", "add", "-q", "-b", "feat-1", filepath.Join(t.TempDir(), "other"))
			}
			repos, err := Scan(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(repos) != 1 || repos[0].Name != tc.dir || repos[0].Kind != KindRepo || len(repos[0].Worktrees) != tc.worktrees {
				t.Fatalf("repos = %+v", repos)
			}
		})
	}
}

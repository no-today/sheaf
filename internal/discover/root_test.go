package discover

import (
	"context"
	"path/filepath"
	"testing"

	"sheaf/internal/testutil"
)

func TestScanNestedRepoNames(t *testing.T) {
	testutil.Isolate(t)
	for _, tc := range []struct {
		child, wantChild string
		rootIsRepo       bool
	}{
		{"api", "./api", true},
		{"lib", "lib", true},
		{"api", "api", false},
	} {
		t.Run(tc.wantChild, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "api")
			want := map[string]string{filepath.Join(root, tc.child): tc.wantChild}
			if tc.rootIsRepo {
				testutil.NewRepo(t, root)
				want[root] = "api"
			}
			testutil.NewRepo(t, filepath.Join(root, tc.child))
			repos, err := Scan(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(repos) != len(want) {
				t.Fatalf("got %d repos, want %d", len(repos), len(want))
			}
			for _, r := range repos {
				if r.Name != want[r.Dir] {
					t.Errorf("repo %s named %q, want %q", r.Dir, r.Name, want[r.Dir])
				}
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

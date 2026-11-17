package session

import (
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestRequirementWithoutCommitsIncludesUncommittedWorktree(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
	repo.Commit("init", map[string]string{"tracked.txt": "original\n", "staged.txt": "original\n"})
	branch := "feat-2611-YT03847"
	worktree := filepath.Join(root, "repo-YT03847")
	repo.Git("worktree", "add", "-q", "-b", branch, worktree)
	worktree, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := Open(t.Context(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.Close)
	if err := sess.SetRequirement(`YT\d+`); err != nil {
		t.Fatal(err)
	}
	if err := sess.SetMergeFlow([]MergeStep{{Name: "master", Branch: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Track("YT03847"); err != nil {
		t.Fatal(err)
	}
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	input := TagAggInput{
		Cond:        &query.Conditions{Q: "YT03847", Authors: []string{}},
		Requirement: "YT03847",
		Base:        "master",
	}
	for _, dirty := range []bool{false, true, false} {
		for _, path := range []string{"tracked.txt", "staged.txt", "new.txt"} {
			content := "original\n"
			if dirty {
				content = "unfinished\n"
			}
			if path == "new.txt" && !dirty {
				if err := os.Remove(filepath.Join(worktree, path)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				continue
			}
			if err := os.WriteFile(filepath.Join(worktree, path), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		testutil.NewRepoAt(t, worktree).Git("add", "staged.txt")
		sess.RefreshDirty(t.Context(), []string{worktree}, true)
		wantRepos, wantFiles := 0, 0
		if dirty {
			wantRepos, wantFiles = 1, 3
		}
		requirements, err := sess.Requirements(t.Context())
		if err != nil || len(requirements) != 1 || requirements[0].Repos != wantRepos {
			t.Fatalf("dirty=%t: requirements: %+v, %v", dirty, requirements, err)
		}
		result, err := sess.TagAgg(t.Context(), input)
		if err != nil || len(result.Repos) != wantRepos || result.Commits != 0 || result.Uncommitted != wantFiles {
			t.Fatalf("dirty=%t: overview: %+v, %v", dirty, result, err)
		}
		if dirty {
			row := result.Repos[0]
			if row.Worktree != worktree || row.Branch != branch || row.Stage != StageDev {
				t.Errorf("uncommitted worktree progress: %+v", row)
			}
		}
		for _, mode := range []string{DiffBase, DiffWorktree, DiffStage} {
			input.DiffMode = mode
			files, err := sess.TagAggFiles(t.Context(), input)
			if err != nil || len(files) != wantFiles {
				t.Fatalf("dirty=%t mode=%s: files: %+v, %v", dirty, mode, files, err)
			}
			for _, file := range files {
				if file.Repo != "repo" || !file.Uncommitted || file.Worktree != (mode != DiffWorktree) {
					t.Errorf("mode=%s: file: %+v", mode, file)
				}
				patch, err := sess.TagAggPatch(t.Context(), input, file.Key, file.Path, file.OldPath, file.Untracked, file.Worktree, false)
				if err != nil || len(patch.Parts) != 1 || !hasLine(patch.Parts[0].Patch, "+", "unfinished") {
					t.Errorf("mode=%s path=%s: patch: %+v, %v", mode, file.Path, patch, err)
				}
			}
		}
		input.Requirement = "YT0384"
		input.Cond.Q = "YT0384"
		other, err := sess.TagAgg(t.Context(), input)
		if err != nil || len(other.Repos) != 0 {
			t.Errorf("partial requirement number matched: %+v, %v", other, err)
		}
		input.Requirement, input.Cond.Q = "YT03847", "YT03847"
	}
}

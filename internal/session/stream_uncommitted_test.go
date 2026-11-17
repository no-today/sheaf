package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRequirementWorktreePatchesStaySeparate(t *testing.T) {
	s, _, input := diffModeFixture(t)
	for _, mode := range []string{DiffBase, DiffStage} {
		in := diffModeInput(t, input, mode, 0)
		for _, tc := range []struct {
			path      string
			untracked bool
			added     []string
		}{
			{"shared.txt", false, []string{"dirty", "unstaged"}},
			{"staged.txt", false, []string{"staged"}},
			{"scratch.txt", true, []string{"scratch"}},
		} {
			patch, err := s.TagAggPatch(t.Context(), in, "repo", tc.path, "", tc.untracked, true, false)
			if err != nil || !slices.Equal(addedLines(patch), tc.added) {
				t.Fatalf("%s %s: %+v, %v", mode, tc.path, patch, err)
			}
		}
		data, err := json.Marshal(AggFile{Worktree: true})
		if err != nil || string(data) == "" {
			t.Fatal(err)
		}
		var file AggFile
		if err := json.Unmarshal(data, &file); err != nil || !file.Worktree {
			t.Fatalf("worktree lost in JSON: %s, %v", data, err)
		}
	}
}

func TestRequirementOmitsCleanAndPersonalWorktrees(t *testing.T) {
	for _, personal := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "personal"}[personal], func(t *testing.T) {
			s, repo, input := diffModeFixture(t)
			if personal {
				repo.Git("checkout", "-q", "-b", "dev-hk")
				repo.Git("branch", "--force", "feat-YT1", "master")
			} else {
				for _, p := range []string{"staged.txt", "scratch.txt"} {
					if err := os.Remove(filepath.Join(repo.Dir, p)); err != nil {
						t.Fatal(err)
					}
				}
				repo.Git("reset", "-q", "HEAD", "staged.txt", "shared.txt")
				repo.Git("checkout", "--", "shared.txt")
			}
			if err := s.Refresh(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{DiffBase, DiffStage, DiffWorktree} {
				files, err := s.TagAggFiles(t.Context(), diffModeInput(t, input, mode, 0))
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range files {
					if f.Worktree || f.Uncommitted {
						t.Fatalf("%s leaked worktree: %+v", mode, files)
					}
				}
				if mode == DiffWorktree && len(files) != 0 {
					t.Fatalf("unexpected worktree files: %+v", files)
				}
			}
		})
	}
}

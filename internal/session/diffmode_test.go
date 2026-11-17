package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func diffModeInput(t *testing.T, in TagAggInput, mode string, stage int) TagAggInput {
	t.Helper()
	data, err := json.Marshal(struct {
		TagAggInput
		Mode  string `json:"diffMode"`
		Stage int    `json:"diffStage"`
	}{in, mode, stage})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func diffModeFixture(t *testing.T) (*Session, *testutil.Repo, TagAggInput) {
	t.Helper()
	testutil.Isolate(t)
	root := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
	repo.Commit("init", map[string]string{"shared.txt": "base\n"})
	repo.Git("branch", "release")
	repo.Git("checkout", "-q", "-b", "feat-YT1")
	first := repo.Commit("YT1 landed", map[string]string{"shared.txt": "base\nlanded\n"})
	repo.Commit("YT1 pending", map[string]string{"shared.txt": "base\nlanded\npending\n"})
	repo.Commit("YT2 unrelated", map[string]string{"other.txt": "other\n"})
	repo.Git("checkout", "-q", "release")
	repo.Commit("release unrelated", map[string]string{"release.txt": "release\n"})
	repo.Git("cherry-pick", first)
	repo.Git("update-ref", "refs/remotes/origin/release", "HEAD")
	repo.Git("checkout", "-q", "feat-YT1")
	for path, content := range map[string]string{
		"shared.txt":  "base\nlanded\npending\ndirty\n",
		"staged.txt":  "staged\n",
		"scratch.txt": "scratch\n",
	} {
		if err := os.WriteFile(filepath.Join(repo.Dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo.Git("add", "shared.txt")
	if err := os.WriteFile(filepath.Join(repo.Dir, "shared.txt"), []byte("base\nlanded\npending\ndirty\nunstaged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git("add", "staged.txt")
	sess, err := Open(t.Context(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.Close)
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := sess.SetRequirement(`YT\d+`); err != nil {
		t.Fatal(err)
	}
	if err := sess.SetMergeFlow([]MergeStep{{Name: "测试", Branch: "origin/release"}, {Name: "上线", Branch: "master"}}); err != nil {
		t.Fatal(err)
	}
	return sess, repo, TagAggInput{Cond: &query.Conditions{Q: "YT1", Authors: []string{}}, Base: "master", Requirement: "YT1"}
}

func addedLines(patch AggPatch) []string {
	var out []string
	for _, part := range patch.Parts {
		for _, hunk := range part.Patch.Hunks {
			for _, line := range hunk.Lines {
				if line.Kind == "+" {
					out = append(out, line.Text)
				}
			}
		}
	}
	return out
}

func TestDiffModesKeepFilesPatchesAndCountsTogether(t *testing.T) {
	sess, _, input := diffModeFixture(t)
	for _, test := range []struct {
		mode  string
		paths []string
		added []string
		stats []FileStat
	}{
		{
			mode: "worktree", paths: []string{"scratch.txt", "shared.txt", "staged.txt"},
			added: []string{"dirty", "unstaged"},
			stats: []FileStat{{Path: "scratch.txt", Add: 1}, {Path: "shared.txt", Add: 2}, {Path: "staged.txt", Add: 1}},
		},
		{
			mode: "stage", paths: []string{"shared.txt", "scratch.txt", "shared.txt", "staged.txt"},
			added: []string{"pending"},
			stats: []FileStat{{Path: "shared.txt", Add: 1}},
		},
		{
			mode: "base", paths: []string{"shared.txt", "scratch.txt", "shared.txt", "staged.txt"},
			added: []string{"landed", "pending"},
			stats: []FileStat{{Path: "shared.txt", Add: 2}},
		},
	} {
		t.Run(test.mode, func(t *testing.T) {
			in := diffModeInput(t, input, test.mode, 0)
			files, err := sess.TagAggFiles(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			if got := aggFilePaths(files); !slices.Equal(got, test.paths) {
				t.Fatalf("files: got %v, want %v", got, test.paths)
			}
			if test.mode != DiffWorktree {
				for i, file := range files {
					if file.Worktree != (i > 0) || file.Uncommitted != (i > 0) {
						t.Fatalf("committed/worktree boundary: %+v", files)
					}
				}
			}
			patch, err := sess.TagAggPatch(t.Context(), in, "repo", "shared.txt", "", false, false, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := addedLines(patch); !slices.Equal(got, test.added) {
				t.Fatalf("patch additions: got %v, want %v", got, test.added)
			}
			stats, err := sess.TagAggStats(t.Context(), in, "repo")
			if err != nil {
				t.Fatal(err)
			}
			slices.SortFunc(stats, func(a, b FileStat) int { return strings.Compare(a.Path, b.Path) })
			if !slices.Equal(stats, test.stats) {
				t.Fatalf("counts: got %+v, want %+v", stats, test.stats)
			}
		})
	}
}

func TestDiffStageSelectionDoesNotChangeProgressOrBase(t *testing.T) {
	sess, _, input := diffModeFixture(t)
	in := diffModeInput(t, input, "stage", 1)
	files, err := sess.TagAggFiles(t.Context(), in)
	if err != nil || !slices.Equal(aggFilePaths(files), []string{"shared.txt", "scratch.txt", "shared.txt", "staged.txt"}) {
		t.Fatalf("files: %+v, %v", files, err)
	}
	patch, err := sess.TagAggPatch(t.Context(), in, "repo", "shared.txt", "", false, false, false)
	if err != nil || !slices.Equal(addedLines(patch), []string{"landed", "pending"}) {
		t.Fatalf("master stage: %+v, %v", patch, err)
	}
	if info := sess.Info(); info.Master != "" || len(info.MergeFlow) != 2 || info.MergeFlow[0].Branch != "origin/release" {
		t.Fatalf("viewing changed workspace configuration: %+v", info)
	}
	in = diffModeInput(t, input, "stage", 9)
	if _, err := sess.TagAggFiles(t.Context(), in); err == nil {
		t.Fatal("deleted stage silently fell back to another target")
	}
}

func TestDiffStageSkipsMissingBranchAndEmptyWhenCovered(t *testing.T) {
	sess, repo, input := diffModeFixture(t)
	if err := sess.SetMergeFlow([]MergeStep{{Name: "验收", Branch: "missing"}}); err != nil {
		t.Fatal(err)
	}
	in := diffModeInput(t, input, "stage", 0)
	files, err := sess.TagAggFiles(t.Context(), in)
	if err != nil || len(files) != 0 {
		t.Fatalf("missing stage should be skipped: %+v, %v", files, err)
	}
	stats, err := sess.TagAggStats(t.Context(), in, "repo")
	if err != nil || len(stats) != 0 {
		t.Fatalf("missing stage stats: %+v, %v", stats, err)
	}
	patch, err := sess.TagAggPatch(t.Context(), in, "repo", "shared.txt", "", false, false, false)
	if err != nil || len(patch.Parts) != 0 {
		t.Fatalf("missing stage patch: %+v, %v", patch, err)
	}
	if err := sess.SetMergeFlow([]MergeStep{{Name: "完成", Branch: "feat-YT1"}}); err != nil {
		t.Fatal(err)
	}
	files, err = sess.TagAggFiles(t.Context(), in)
	if err != nil || !slices.Equal(aggFilePaths(files), []string{"scratch.txt", "shared.txt", "staged.txt"}) {
		t.Fatalf("covered stage worktree: %+v, %v", files, err)
	}
	repo.Git("update-ref", "refs/remotes/origin/release", "feat-YT1")
	repo.Git("branch", "-D", "release")
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := sess.SetMergeFlow([]MergeStep{{Name: "远程阶段", Branch: "origin/release"}}); err != nil {
		t.Fatal(err)
	}
	files, err = sess.TagAggFiles(t.Context(), in)
	if err != nil || !slices.Equal(aggFilePaths(files), []string{"scratch.txt", "shared.txt", "staged.txt"}) {
		t.Fatalf("remote-only stage worktree: %+v, %v", files, err)
	}
}

func TestDiffWorktreeRemainsAvailableAfterMerge(t *testing.T) {
	sess, repo, input := diffModeFixture(t)
	repo.Git("update-ref", "refs/heads/master", "feat-YT1")
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	in := diffModeInput(t, input, DiffWorktree, 0)
	files, err := sess.TagAggFiles(t.Context(), in)
	if err != nil || !slices.Equal(aggFilePaths(files), []string{"scratch.txt", "shared.txt", "staged.txt"}) {
		t.Fatalf("merged worktree files: %+v, %v", files, err)
	}
	patch, err := sess.TagAggPatch(t.Context(), in, "repo", "shared.txt", "", false, false, false)
	if err != nil || !slices.Equal(addedLines(patch), []string{"dirty", "unstaged"}) {
		t.Fatalf("merged worktree patch: %+v, %v", patch, err)
	}
	stats, err := sess.TagAggStats(t.Context(), in, "repo")
	if err != nil || len(stats) != 3 {
		t.Fatalf("merged worktree stats: %+v, %v", stats, err)
	}
}

func TestDiffStageIncludesEveryRemainingCommitPatch(t *testing.T) {
	sess, repo, input := diffModeFixture(t)
	repo.Commit("YT1 temporary file", map[string]string{"temporary.txt": "temporary\n"})
	repo.Git("rm", "temporary.txt")
	repo.Git("commit", "-qm", "YT1 remove temporary file")
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	in := diffModeInput(t, input, DiffStage, 0)
	files, err := sess.TagAggFiles(t.Context(), in)
	if err != nil || !slices.Contains(aggFilePaths(files), "temporary.txt") {
		t.Fatalf("stage lost a remaining commit's file: %+v, %v", files, err)
	}
	patch, err := sess.TagAggPatch(t.Context(), in, "repo", "temporary.txt", "", false, false, false)
	if err != nil || len(patch.Parts) != 2 {
		t.Fatalf("remaining commit patches: %+v, %v", patch, err)
	}
	stats, err := sess.TagAggStats(t.Context(), in, "repo")
	if err != nil || !slices.Contains(stats, FileStat{Path: "temporary.txt", Add: 1, Del: 1}) {
		t.Fatalf("remaining commit stats: %+v, %v", stats, err)
	}
}

package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sheaf/internal/index"
	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestMergedRequirementKeepsWorktreeProgress(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			testutil.Isolate(t)
			root := t.TempDir()
			repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
			repo.Commit("init", map[string]string{"base.txt": "base\n"})
			branch := "feat-YT03859"
			repo.Git("checkout", "-q", "-b", branch)
			repo.Commit("YT03859 work", map[string]string{"own.txt": "own\n"})
			repo.Git("checkout", "-q", "master")
			repo.Merge(branch, "Merge branch '"+branch+"'")
			worktree := filepath.Join(root, "repo-YT03859")
			repo.Git("worktree", "add", "-q", worktree, branch)
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
			if err := sess.Refresh(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := sess.SetMergeFlow([]MergeStep{{Name: "基线", Branch: "master"}}); err != nil {
				t.Fatal(err)
			}
			if err := sess.Track("YT03859"); err != nil {
				t.Fatal(err)
			}
			input := TagAggInput{Cond: &query.Conditions{Q: "YT03859", Authors: []string{}}, Requirement: "YT03859", Base: "master"}
			if explicit {
				input.Cond.Branches = []string{branch}
			}
			for _, count := range []int{0, 1, 0} {
				scratch := filepath.Join(worktree, "scratch.txt")
				if count > 0 {
					if err := os.WriteFile(scratch, []byte("unfinished\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Remove(scratch); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				sess.RefreshDirty(t.Context(), []string{worktree}, true)
				stage := StageLanded
				if count > 0 {
					stage = StageDev
				}
				result, err := sess.TagAgg(t.Context(), input)
				if err != nil || len(result.Repos) != 1 {
					t.Fatalf("overview: %+v, %v", result, err)
				}
				row := result.Repos[0]
				if row.Worktree != worktree || row.Uncommitted != count || row.Stage != stage || row.BranchFrom != FromMerged {
					t.Errorf("count=%d: merged worktree progress: %+v", count, row)
				}
				requirements, err := sess.Requirements(t.Context())
				if err != nil || len(requirements) != 1 || requirements[0].Stage != stage {
					t.Errorf("count=%d: requirement progress: %+v, %v", count, requirements, err)
				}
				input.DiffMode = DiffWorktree
				files, err := sess.TagAggFiles(t.Context(), input)
				if err != nil || len(files) != count {
					t.Errorf("count=%d: worktree diff: %+v, %v", count, files, err)
				}
				input.DiffMode = DiffStage
				files, err = sess.TagAggFiles(t.Context(), input)
				if err != nil || len(files) != count {
					t.Errorf("count=%d: landed stage diff: %+v, %v", count, files, err)
				}
			}
		})
	}
}

func TestMergedScope(t *testing.T) {
	parents := [][]int32{nil, {0}, {1}, {2}, {1}, {4, 3}, {5}, {6}, {6, 7}, {8}}
	r := &index.Repo{Time: make([]int64, len(parents)), ParOff: []uint32{0}}
	for _, ps := range parents {
		r.Par = append(r.Par, ps...)
		r.ParOff = append(r.ParOff, uint32(len(r.Par)))
	}
	for _, tc := range []struct {
		name    string
		h, base int32
		commits []int32
		want    []int32
	}{
		{"first inclusion before later merges", 3, 9, []int32{9, 6, 3, 2, 1, 0}, []int32{3, 2}},
		{"tag intersection keeps its order", 3, 9, []int32{2, 1, 3}, []int32{2, 3}},
		{"tag intersection can be empty", 3, 9, []int32{1, 0}, nil},
		{"head is the base", 9, 9, []int32{9, 3}, nil},
		{"head is on the first parent chain", 5, 9, []int32{5, 3}, nil},
		{"head not fully merged", 7, 5, []int32{7, 6}, nil},
		{"no base", 3, -1, []int32{3}, nil},
		{"no head", -1, 9, []int32{3}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergedScope(r, tc.commits, tc.h, tc.base); !slices.Equal(got, tc.want) {
				t.Errorf("mergedScope = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTagAggMerged(t *testing.T) {
	for _, tc := range []struct {
		name, mode, q, override string
		want                    []string
		merged                  bool
	}{
		{"merge commit", "merge", "", "", []string{"feature one", "feature two"}, true},
		{"different compare target", "compare-target", "", "", []string{"feature one", "feature two"}, true},
		{"feature merged main first", "merge-main", "", "", []string{"feature one", "feature two", "merge main into feature"}, true},
		{"text intersects the branch range", "merge", "feature two", "", []string{"feature two"}, true},
		{"fast forward", "ff", "", "", nil, false},
		{"partial merge", "partial", "", "", []string{"feature two"}, false},
		{"manually chosen branch", "merge", "", "feature", nil, false},
		{"manual branch with different compare target", "compare-target", "", "feature", nil, false},
		{"empty intersection", "merge", "base history", "", nil, false},
		{"base is the branch", "base", "", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Isolate(t)
			ctx := context.Background()
			root := t.TempDir()
			r := testutil.NewRepo(t, filepath.Join(root, "repo"))
			r.Commit("base init", map[string]string{"feature.txt": "initial\n"})
			r.Commit("base history", map[string]string{"feature.txt": "base\n"})
			r.Git("checkout", "-q", "-b", "feature")
			r.Commit("feature one", map[string]string{"feature.txt": "one\n"})
			r.Git("branch", "first")
			if tc.mode == "merge-main" {
				r.Git("checkout", "-q", "master")
				r.Commit("main during feature", map[string]string{"main.txt": "main\n"})
				r.Git("checkout", "-q", "feature")
				r.Merge("master", "merge main into feature")
			}
			r.Commit("feature two", map[string]string{"feature.txt": "two\n"})
			wt := filepath.Join(root, "feature-wt")
			r.Git("checkout", "-q", "master")
			r.Git("worktree", "add", "-q", wt, "feature")
			base := "master"
			switch tc.mode {
			case "compare-target":
				r.Git("checkout", "-q", "-b", "uat")
				r.Merge("feature", "land feature in uat")
				base = "uat"
			case "ff":
				r.Git("merge", "-q", "--ff-only", "feature")
			case "partial":
				r.Merge("first", "land first commit")
			default:
				r.Merge("feature", "land feature")
			}
			r.Commit("main after merge", map[string]string{"after.txt": "after\n"})
			s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			branch := "feature"
			if tc.mode == "base" {
				branch = "master"
			}
			c := query.Conditions{Q: tc.q, Branches: []string{branch}}
			{
				in := TagAggInput{Cond: &c, Base: base}
				if tc.override != "" {
					in.Branches = map[string]string{"repo": tc.override}
				}
				res, err := s.TagAgg(ctx, in)
				if err != nil {
					t.Fatal(err)
				}
				want, from := tc.want, FromTag
				if tc.merged {
					from = FromMerged
				} else if tc.override != "" {
					from = FromOverride
				}
				if len(res.Repos) == 0 {
					if len(want) != 0 || res.Commits != 0 {
						t.Errorf("no row, want %v; overview %+v", want, res)
					}
					return
				}
				a := res.Repos[0]
				if res.Commits != len(want) || a.Commits != len(want) || a.BranchFrom != from {
					t.Errorf("overview %+v, want %d commits from %s", res, len(want), from)
				}
				if from == FromMerged && a.MergedBranch != branch {
					t.Errorf("merged row lost its branch name: %+v", a)
				}
				if from == FromMerged && (a.Branch != "" || a.Auto != "" || a.Ahead != 0 || a.Behind != 0 || a.Worktree != "" || a.Uncommitted != 0) {
					t.Errorf("merged row still compares or has a working directory: %+v", a)
				}
				if from != FromMerged && a.Branch != branch {
					t.Errorf("branch changed: %+v", a)
				}
				for _, row := range []string{"", a.Key} {
					commits, err := s.TagAggCommits(in, row, true)
					if err != nil || commits.Total != len(want) {
						t.Fatalf("row=%q: commits %+v, %v", row, commits, err)
					}
					rows, err := s.Rows(commits.ResultID, 0, 100)
					if err != nil {
						t.Fatal(err)
					}
					var got []string
					for _, c := range rows {
						got = append(got, c.Subject)
					}
					slices.Sort(got)
					if !slices.Equal(got, want) {
						t.Errorf("row=%q: subjects %v, want %v", row, got, want)
					}
				}
				if from != FromMerged {
					return
				}
				files, err := s.TagAggFiles(ctx, in)
				if err != nil || len(files) != a.Files {
					t.Fatalf("files %+v, overview %+v, %v", files, a, err)
				}
				if tc.mode == "merge-main" && (res.Files != 1 || a.Files != 1 || len(files) != 1 || files[0].Path != "feature.txt") {
					t.Errorf("merging main added its files: total=%d, row=%d, files=%+v", res.Files, a.Files, files)
				}
				for _, f := range files {
					if f.Uncommitted || f.Path == "after.txt" {
						t.Errorf("files include worktree or later main history: %+v", f)
					}
				}
				patch, err := s.TagAggPatch(ctx, in, a.Key, "feature.txt", "", false, false, false)
				if err != nil || patch.Mode != "commits" {
					t.Fatalf("patch %+v, %v", patch, err)
				}
				var subjects []string
				for _, p := range patch.Parts {
					subjects = append(subjects, p.Subject)
				}
				wantPatch := []string{"feature one", "feature two"}
				if tc.q != "" {
					wantPatch = []string{"feature two"}
				}
				if !slices.Equal(subjects, wantPatch) {
					t.Errorf("patch subjects %v, want %v", subjects, wantPatch)
				}
				down, err := s.TagAggDown(in, a.Key)
				if err != nil || down.Total != 0 {
					t.Errorf("merged row has down commits: %+v, %v", down, err)
				}
			}
		})
	}
}

func TestTagAggMergedSeveralBranches(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("base", map[string]string{"base.txt": "base\n"})
	r.Git("checkout", "-q", "-b", "merged")
	r.Commit("merged work", map[string]string{"merged.txt": "merged\n"})
	r.Git("checkout", "-q", "master")
	r.Git("checkout", "-q", "-b", "pending")
	r.Commit("pending work", map[string]string{"pending.txt": "pending\n"})
	r.Git("checkout", "-q", "master")
	r.Merge("merged", "land merged")
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	c := query.Conditions{Branches: []string{"merged", "pending"}}
	for _, tc := range []struct {
		name, base, override string
	}{
		{"merged", "master", ""},
		{"override", "master", "pending"},
		{"missing base", "missing", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := TagAggInput{Cond: &c, Base: tc.base, Branches: map[string]string{RowKey("repo", "merged"): tc.override}}
			res, err := s.TagAgg(ctx, in)
			if err != nil || len(res.Repos) != 2 {
				t.Fatalf("overview %+v, %v", res, err)
			}
			for _, a := range res.Repos {
				want, from := []string{"pending work"}, FromTag
				if a.Key == RowKey("repo", "merged") {
					want = nil
					switch {
					case tc.override != "":
						from = FromOverride
					case tc.base != "missing":
						want, from = []string{"merged work"}, FromMerged
					default: // no compare target to judge it by, but the query still finds the branch's own commits
						want = []string{"merged work"}
					}
				}
				commits, err := s.TagAggCommits(in, a.Key, true)
				if err != nil {
					t.Fatal(err)
				}
				rows, err := s.Rows(commits.ResultID, 0, 100)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, row := range rows {
					got = append(got, row.Subject)
				}
				slices.Sort(got)
				if !slices.Equal(got, want) || a.Commits != len(want) || a.BranchFrom != from {
					t.Errorf("row %+v has %v, want %v from %s", a, got, want, from)
				}
			}
		})
	}
}

func TestMergedBranchNameWithoutBranchCondition(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(r *testutil.Repo)
		want  string
	}{
		{"branch still there", func(r *testutil.Repo) {}, "feature"},
		{"branch deleted", func(r *testutil.Repo) { r.Git("branch", "-D", "feature") }, "feature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Isolate(t)
			ctx := context.Background()
			root := t.TempDir()
			r := testutil.NewRepo(t, filepath.Join(root, "repo"))
			r.Commit("base", map[string]string{"a": "1"})
			r.Git("checkout", "-q", "-b", "dev")
			r.Commit("dev start", map[string]string{"d": "1"})
			r.Git("checkout", "-q", "-b", "feature")
			r.Commit("YT1 work", map[string]string{"b": "1"})
			r.Git("checkout", "-q", "dev")
			r.Merge("feature", "Merge branch 'feature' into 'dev'")
			r.Git("checkout", "-q", "master")
			r.Merge("dev", "Merge branch 'dev' into 'master'")
			tc.setup(r)
			s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			c := query.Conditions{Q: "YT1"}
			res, err := s.TagAgg(ctx, TagAggInput{Cond: &c, Base: "master"})
			if err != nil || len(res.Repos) != 1 {
				t.Fatalf("%+v, %v", res, err)
			}
			if a := res.Repos[0]; a.BranchFrom != FromMerged || a.MergedBranch != tc.want {
				t.Errorf("row %+v, want merged branch %s", a, tc.want)
			}
		})
	}
}

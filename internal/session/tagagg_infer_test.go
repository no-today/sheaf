package session

import (
	"context"
	"path/filepath"
	"testing"

	"sheaf/internal/index"
	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestInferBranch(t *testing.T) {
	for _, tc := range []struct {
		name, baseName, release, want, from string
		refs                                []index.Ref
		commits                             []int32
		checkedOut                          map[string]bool
	}{
		{
			name: "local master behind and deleted feature does not become origin release", baseName: "master", release: "release", from: FromMerged,
			refs: []index.Ref{{Name: "refs/heads/master", Commit: 0}, {Name: "refs/remotes/origin/master", Commit: 1}, {Name: "refs/remotes/origin/release", Commit: 2}},
		},
		{
			name: "another remote also proves merged", baseName: "refs/heads/master", release: "release", from: FromMerged,
			refs: []index.Ref{{Name: "refs/heads/master", Commit: 0}, {Name: "refs/remotes/upstream/master", Commit: 1}, {Name: "refs/remotes/origin/release", Commit: 2}},
		},
		{
			name: "an explicitly named remote still checks same branch on other remotes", baseName: "origin/master", release: "release", from: FromMerged,
			refs: []index.Ref{{Name: "refs/remotes/origin/master", Commit: 0}, {Name: "refs/remotes/upstream/master", Commit: 1}, {Name: "refs/remotes/origin/release", Commit: 2}},
		},
		{
			name: "nested base branch name", baseName: "team/main", release: "release", from: FromMerged,
			refs: []index.Ref{{Name: "refs/heads/team/main", Commit: 0}, {Name: "refs/remotes/upstream/team/main", Commit: 1}},
		},
		{
			name: "custom release excluded locally and remotely", baseName: "master", release: "shipping", want: "feature", from: FromInferred,
			refs: []index.Ref{{Name: "refs/heads/master", Commit: 0}, {Name: "refs/heads/shipping", Commit: 2}, {Name: "refs/remotes/origin/shipping", Commit: 2}, {Name: "refs/heads/feature", Commit: 3}},
		},
		{
			name: "remote release setting also excludes its local branch", baseName: "master", release: "origin/shipping", from: FromNone,
			refs: []index.Ref{{Name: "refs/heads/master", Commit: 0}, {Name: "refs/heads/shipping", Commit: 2}, {Name: "refs/remotes/origin/shipping", Commit: 2}, {Name: "refs/remotes/upstream/shipping", Commit: 2}},
		},
		{
			name: "pending commit still infers feature", baseName: "master", release: "release", want: "feature", from: FromInferred, commits: []int32{1, 3},
			refs: []index.Ref{{Name: "refs/heads/master", Commit: 0}, {Name: "refs/remotes/origin/master", Commit: 1}, {Name: "refs/remotes/origin/release", Commit: 2}, {Name: "refs/heads/feature", Commit: 3}},
		},
		{
			name: "feature with release in its path remains a candidate", baseName: "master", release: "release", want: "feature/release", from: FromInferred,
			refs: []index.Ref{{Name: "refs/heads/master", Commit: 0}, {Name: "refs/heads/feature/release", Commit: 3}},
		},
		{
			name: "checked out branch wins ties", baseName: "master", release: "release", want: "z", from: FromInferred, checkedOut: map[string]bool{"refs/heads/z": true},
			refs: []index.Ref{{Name: "refs/heads/master", Commit: 0}, {Name: "refs/heads/a", Commit: 3}, {Name: "refs/heads/z", Commit: 3}},
		},
		{
			name: "unrelated remote master does not mark pending commit merged", baseName: "master", release: "release", want: "origin/feature", from: FromInferred,
			refs: []index.Ref{{Name: "refs/heads/master", Commit: 0}, {Name: "refs/remotes/origin/master", Commit: 4}, {Name: "refs/remotes/origin/feature", Commit: 3}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 0 init; 1 requirement; 2 release (newest) and 3 feature both descend from 1; 4 is unrelated.
			r := &index.Repo{Time: []int64{10, 20, 40, 30, 50}, ParOff: []uint32{0, 0, 1, 2, 3, 4}, Par: []int32{0, 1, 1, 0}, Refs: tc.refs}
			commits := tc.commits
			if commits == nil {
				commits = []int32{1}
			}
			got, from := inferBranch(r, 0, tc.baseName, tc.release, commits, tc.checkedOut, nil)
			if got != tc.want || from != tc.from {
				t.Errorf("inferBranch = %q (%s), want %q (%s)", got, from, tc.want, tc.from)
			}
		})
	}
}

func TestTagAggInferenceUsesWorkspaceRelease(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"a.txt": "initial\n"})
	r.Git("branch", "-M", "master")
	r.Git("checkout", "-q", "-b", "feature")
	r.Commit("REQ work", map[string]string{"a.txt": "feature\n"})
	r.Git("checkout", "-q", "-b", "shipping")
	r.Commit("shipping moves", map[string]string{"other.txt": "other\n"})
	r.Git("checkout", "-q", "master")
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCompareTargets("shipping", "master"); err != nil {
		t.Fatal(err)
	}
	c := query.Conditions{Q: "REQ"}
	res, err := s.TagAgg(ctx, TagAggInput{Cond: &c})
	if err != nil || len(res.Repos) != 1 || res.Repos[0].Branch != "feature" || res.Repos[0].BranchFrom != FromInferred {
		t.Errorf("workspace release inferred as feature: %+v %v", res, err)
	}
}

func TestRequirementBranchDoesNotBecomeAMergeDestination(t *testing.T) {
	for _, scenario := range []string{"named remote", "long-lived source", "deleted source", "another requirement", "invalid override"} {
		t.Run(scenario, func(t *testing.T) {
			testutil.Isolate(t)
			root := t.TempDir()
			repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
			repo.Commit("init", map[string]string{"base.txt": "base\n"})
			repo.Git("branch", "release")
			repo.Git("checkout", "-q", "-b", "feat-dev")
			repo.Commit("YT03826 work", map[string]string{"own.txt": "own\n"})
			repo.Git("update-ref", "refs/remotes/origin/feat-YT03632", "HEAD")
			repo.Git("checkout", "-q", "release")
			repo.Commit("release moves", map[string]string{"other.txt": "other\n"})
			repo.Merge("feat-dev", "Merge branch 'feat-dev' into release for YT03826")
			repo.Git("update-ref", "refs/remotes/origin/release-BU2-0027", "HEAD")
			repo.Git("checkout", "-q", "master")
			want, stage := "", StageUAT
			switch scenario {
			case "named remote", "invalid override":
				repo.Git("update-ref", "refs/remotes/origin/feat-2611-YT03826", "feat-dev")
				want, stage = "origin/feat-2611-YT03826", StageUAT
			case "deleted source":
				repo.Git("branch", "-D", "feat-dev")
			case "another requirement":
				repo.Git("update-ref", "refs/remotes/origin/feat-YT038260", "feat-dev")
			}
			sess, err := Open(t.Context(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sess.Close)
			if err := sess.SetMergeFlow([]MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
				t.Fatal(err)
			}
			if err := sess.Refresh(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := sess.SetRequirement(`YT\d+`); err != nil {
				t.Fatal(err)
			}
			input := TagAggInput{Requirement: "YT03826", Cond: &query.Conditions{Q: "YT03826", Authors: []string{}}, DiffMode: DiffBase}
			if scenario == "invalid override" {
				input.Branches = map[string]string{"repo": "feat-dev"}
			}
			result, err := sess.TagAgg(t.Context(), input)
			if err != nil || len(result.Repos) != 1 {
				t.Fatalf("overview: %+v, %v", result, err)
			}
			if row := result.Repos[0]; row.Branch != want || row.Stage != stage {
				t.Errorf("branch=%q stage=%q, want %q %q", row.Branch, row.Stage, want, stage)
			}
			if want == "" {
				if row := result.Repos[0]; row.BranchFrom != FromMerged || row.MergedBranch != "" || row.Files != 0 {
					t.Errorf("missing requirement branch should retain history without diff files: %+v", row)
				}
				branches, err := sess.TagAggBranches(input, "repo")
				if err != nil || len(branches) != 0 {
					t.Errorf("unrelated branch choices: %+v, %v", branches, err)
				}
				if err := sess.Track("YT03826"); err != nil {
					t.Fatal(err)
				}
				requirements, err := sess.Requirements(t.Context())
				if err != nil || len(requirements) != 1 || requirements[0].Stage != StageUAT {
					t.Errorf("deleted branch progress: %+v, %v", requirements, err)
				}
				if err := sess.SetMergeFlow([]MergeStep{{Name: "上线", Branch: "missing"}}); err != nil {
					t.Fatal(err)
				}
				for _, mode := range []string{"", DiffBase, DiffStage, DiffWorktree} {
					input.DiffMode = mode
					files, err := sess.TagAggFiles(t.Context(), input)
					if err != nil || len(files) != 0 {
						t.Errorf("%s files for deleted branch: %+v, %v", mode, files, err)
					}
					stats, err := sess.TagAggStats(t.Context(), input, "repo")
					if err != nil || len(stats) != 0 {
						t.Errorf("%s stats for deleted branch: %+v, %v", mode, stats, err)
					}
					patch, err := sess.TagAggPatch(t.Context(), input, "repo", "own.txt", "", false, false, false)
					if err != nil || len(patch.Parts) != 0 {
						t.Errorf("%s patch for deleted branch: %+v, %v", mode, patch, err)
					}
				}
				return
			}
			patch, err := sess.TagAggPatch(t.Context(), input, "repo", "own.txt", "", false, false, false)
			if err != nil || len(addedLines(patch)) != 1 || addedLines(patch)[0] != "own" {
				t.Errorf("base diff lost requirement changes: %+v, %v", patch, err)
			}
			input.DiffMode = DiffStage
			files, err := sess.TagAggFiles(t.Context(), input)
			if err != nil || len(files) != 0 {
				t.Errorf("merged stage diff: %+v, %v", files, err)
			}
		})
	}
}

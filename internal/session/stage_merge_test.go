package session

import (
	"path/filepath"
	"testing"
	"time"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestRequirementStageAfterConflictResolution(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolved map[string]string
		want     string
		ancestry bool
	}{
		{"parent content", nil, StageLanded, false},
		{"parent content already ancestor", nil, StageLanded, true},
		{"combined parent content", map[string]string{"f": "other\nfeature\nkeep\n"}, StageLanded, false},
		{"conflict markers", map[string]string{"f": "<<<<<<< HEAD\nother\n=======\nfeature\n>>>>>>> feat\nkeep\n"}, StageLanded, false},
		{"new content", map[string]string{"f": "resolved\nkeep\n"}, MergeStage(1), false},
		{"deleted common content", map[string]string{"f": "feature\n"}, MergeStage(1), false},
		{"added file", map[string]string{"extra": "merge work\n"}, MergeStage(1), false},
		{"deleted file", map[string]string{"common": ""}, MergeStage(1), false},
		{"binary content", map[string]string{"common": "\x00merge work\n"}, MergeStage(1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Isolate(t)
			ctx := t.Context()
			root := t.TempDir()
			r := testutil.NewRepo(t, filepath.Join(root, "repo"))
			r.Commit("init", map[string]string{"f": "base\nkeep\n", "common": "common\n"})
			r.Git("checkout", "-q", "-b", "feat-MRXX-1001")
			r.Commit("MRXX-1001 feature", map[string]string{"f": "feature\nkeep\n"})
			r.Git("checkout", "-q", "-b", "integration", "master")
			r.Commit("integration work", map[string]string{"f": "other\nkeep\n"})
			r.Git("merge", "-q", "--no-ff", "--no-commit", "-X", "theirs", "feat-MRXX-1001")
			resolution := r.Commit("Merge branch 'feat-MRXX-1001' into integration", tc.resolved)
			r.Git("tag", "integration-copy", resolution)
			r.Git("checkout", "-q", "master")
			if tc.ancestry {
				r.Merge("feat-MRXX-1001", "Merge branch 'feat-MRXX-1001' into master")
			} else {
				r.Git("cherry-pick", "feat-MRXX-1001")
			}
			r.Git("branch", "-D", "integration", "feat-MRXX-1001")
			r.Git("update-ref", "refs/remotes/origin/master", "master")
			r.Git("update-ref", "refs/remotes/origin/feat-MRXX-1001", resolution)
			s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_100_000, 0) }})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.SetMergeFlow([]MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
				t.Fatal(err)
			}
			if err := s.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			if err := s.Track("MRXX-1001"); err != nil {
				t.Fatal(err)
			}
			in := TagAggInput{Cond: &query.Conditions{Q: "MRXX-1001", Authors: []string{}}, Requirement: "MRXX-1001"}
			if changed, err := s.TagAggRemerge(ctx, in); err != nil || !changed {
				t.Fatalf("classify: changed=%v err=%v", changed, err)
			}
			if _, err := s.RequirementsSettle(ctx); err != nil {
				t.Fatal(err)
			}
			reqs, err := s.Requirements(ctx)
			if err != nil || len(reqs) != 1 || reqs[0].Stage != tc.want {
				t.Log(r.Git("show", "--cc", "--format=", resolution))
				t.Fatalf("requirements=%+v err=%v want=%s", reqs, err, tc.want)
			}
			res, err := s.TagAgg(ctx, in)
			if err != nil || len(res.Repos) != 1 || res.Repos[0].Stage != tc.want {
				t.Fatalf("rows=%+v err=%v want=%s", res.Repos, err, tc.want)
			}
			commits, err := s.TagAggCommits(in, "", false)
			if err != nil {
				t.Fatalf("conflict resolutions must remain visible: commits=%+v err=%v", commits, err)
			}
			rows, err := s.Rows(commits.ResultID, 0, commits.Total)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.Hash == resolution && row.Resolved {
					found = true
				}
			}
			if !found {
				t.Fatalf("conflict resolution missing from commits: %+v", rows)
			}
		})
	}
}

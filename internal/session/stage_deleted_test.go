package session

import (
	"path/filepath"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestDeletedRequirementStageUsesCoverage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		coverage   string
		mergeOnly  bool
		want       string
		settled    string
		missingEnd bool
	}{
		{name: "unmerged", want: StagePending, settled: StagePending},
		{name: "released", coverage: "release", want: StageUAT, settled: StageUAT},
		{name: "landed", coverage: "master", want: StageLanded, settled: StageLanded},
		{name: "partly landed", coverage: "partial", want: StagePending, settled: StagePending},
		{name: "patch equivalent", coverage: "cherry-pick", want: StagePending, settled: StageLanded},
		{name: "no own commits", mergeOnly: true, want: StageUnknown, settled: StageUnknown},
		{name: "missing endpoint", coverage: "release", missingEnd: true, want: StageUAT, settled: StageUAT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Isolate(t)
			ctx := t.Context()
			root := t.TempDir()
			r := testutil.NewRepo(t, filepath.Join(root, "deleted"))
			r.Commit("init", map[string]string{"base": "base\n"})
			r.Git("branch", "release")
			r.Git("checkout", "-q", "-b", "feat-EVID-1001")
			if tc.mergeOnly {
				r.Commit("unrelated work", map[string]string{"own": "own\n"})
				r.Git("checkout", "-q", "-b", "integration", "master")
				r.Merge("feat-EVID-1001", "Merge EVID-1001")
			} else {
				r.Commit("EVID-1001 first", map[string]string{"own": "own\n"})
				r.Commit("EVID-1001 second", map[string]string{"second": "second\n"})
			}
			r.Git("tag", "saved-evidence")
			r.Git("checkout", "-q", "master")
			switch tc.coverage {
			case "master", "release":
				r.Git("checkout", "-q", tc.coverage)
				r.Merge("feat-EVID-1001", "Integrate feature")
			case "partial":
				r.Merge("feat-EVID-1001~1", "Integrate first commit")
			case "cherry-pick":
				r.Commit("advance master", map[string]string{"other": "other\n"})
				for _, commit := range []string{"feat-EVID-1001~1", "feat-EVID-1001"} {
					r.Git("cherry-pick", "--no-commit", commit)
					r.Commit("Applied change", nil)
				}
			}
			r.Git("checkout", "-q", "master")
			r.Git("branch", "-D", "feat-EVID-1001")
			if tc.mergeOnly {
				r.Git("branch", "-D", "integration")
			}
			// A landed row must not hide an unconfirmed or unfinished repository.
			landed := testutil.NewRepo(t, filepath.Join(root, "landed"))
			landed.Commit("EVID-1001 landed elsewhere", map[string]string{"landed": "done\n"})
			s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			endpoint := "master"
			if tc.missingEnd {
				endpoint = "missing"
			}
			if err := s.SetMergeFlow([]MergeStep{{Name: "release", Branch: "release"}, {Name: "end", Branch: endpoint}}); err != nil {
				t.Fatal(err)
			}
			if err := s.Track("EVID-1001"); err != nil {
				t.Fatal(err)
			}
			if err := s.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			in := TagAggInput{Requirement: "EVID-1001", Cond: &query.Conditions{Q: "EVID-1001", Authors: []string{}}}
			check := func(want string) {
				t.Helper()
				result, err := s.TagAgg(ctx, in)
				if err != nil || len(result.Repos) != 2 {
					t.Fatalf("rows=%+v err=%v", result, err)
				}
				found := false
				for _, row := range result.Repos {
					if row.Repo != "deleted" {
						continue
					}
					found = true
					commits := 2
					if tc.mergeOnly {
						commits = 1
					}
					if row.Stage != want || row.Branch != "" || row.BranchFrom != FromMerged || row.Commits != commits || row.Files != 0 {
						t.Errorf("deleted row=%+v, want stage=%s commits=%d", row, want, commits)
					}
				}
				if !found {
					t.Fatal("deleted repository missing")
				}
				reqs, err := s.Requirements(ctx)
				if err != nil || len(reqs) != 1 || reqs[0].Stage != want || reqs[0].Progress != want || reqs[0].Archived || reqs[0].Repos != 2 {
					t.Fatalf("requirements=%+v err=%v want=%s", reqs, err, want)
				}
			}
			check(tc.want)
			changed, err := s.RequirementsSettle(ctx)
			if err != nil || tc.coverage == "cherry-pick" && !changed {
				t.Fatalf("settle changed=%v err=%v", changed, err)
			}
			check(tc.settled)
		})
	}
}

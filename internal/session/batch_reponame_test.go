package session

import (
	"maps"
	"path/filepath"
	"testing"

	"sheaf/internal/testutil"
)

func TestRefreshKeepsBaseOnRootRepo(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))
	root.Commit("root", nil)
	root.Git("branch", "root-only")
	ctx := t.Context()
	s, err := Open(ctx, root.Dir, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBase("api", "root-only"); err != nil {
		t.Fatal(err)
	}
	child := testutil.NewRepo(t, filepath.Join(root.Dir, "api"))
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, repo := range s.Info().Repos {
		got[repo.Worktrees[0].Path] = repo.Base
	}
	if want := map[string]string{root.Dir: "root-only", child.Dir: ""}; !maps.Equal(got, want) {
		t.Errorf("bases by directory = %v, want %v", got, want)
	}
}

func TestBranchesPlanWithClashingRepoNames(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))
	root.Commit("root", nil)
	root.Git("branch", "root-only")
	child := testutil.NewRepo(t, filepath.Join(root.Dir, "api"))
	child.Commit("child", nil)
	child.Git("branch", "child-only")

	ctx := t.Context()
	s, err := Open(ctx, root.Dir, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ repo, branch string }{{"api", "root-only"}, {"./api", "child-only"}} {
		plan, err := s.BranchesPlan(ctx, BatchPlanInput{Repos: []string{tc.repo}, Branches: []string{"root-only", "child-only"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Rows) != 1 || plan.Rows[0].Repo != tc.repo || plan.Rows[0].Branch != tc.branch {
			t.Errorf("plan for %q = %+v, want only %q", tc.repo, plan.Rows, tc.branch)
		}
	}
}

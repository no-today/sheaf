package session

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sheaf/internal/testutil"
)

// Fetch and sync end to end on a workspace of throwaway clones, each with its own origin.
func TestBatchGit(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(tmp, "w")
	ups := map[string]*testutil.Repo{}
	clones := map[string]*testutil.Repo{}
	// A clone of an origin that has master and release; the origin then moves on (master +1, release +1) without the clone knowing.
	repo := func(name string) *testutil.Repo {
		up := testutil.NewRepo(t, filepath.Join(tmp, "up", name))
		up.Commit("init", map[string]string{"a.txt": "a\n"})
		up.Git("branch", "-M", "master")
		up.Git("branch", "release")
		origin := filepath.Join(tmp, "origin", name+".git")
		up.Git("clone", "-q", "--bare", up.Dir, origin)
		up.Git("remote", "add", "origin", origin)
		c := testutil.NewRepoAt(t, filepath.Join(root, name))
		up.Git("clone", "-q", origin, c.Dir)
		c.Git("config", "user.name", "Me")
		c.Git("config", "user.email", "me@example.com")
		c.Git("branch", "release", "origin/release")
		c.Git("branch", "feat", "master")
		for _, b := range []string{"master", "release"} {
			up.Git("checkout", "-q", b)
			up.Commit(b+" moves", map[string]string{b + ".txt": b + "\n"})
			up.Git("push", "-q", "origin", b)
		}
		ups[name], clones[name] = up, c
		return c
	}
	repo("clean").Git("checkout", "-q", "feat")
	dirty := repo("dirty")
	dirty.Git("checkout", "-q", "feat")
	os.WriteFile(filepath.Join(dirty.Dir, "a.txt"), []byte("edited\n"), 0o644)
	unpushed := repo("unpushed")
	unpushed.Commit("local only", map[string]string{"l.txt": "l\n"})
	unpushed.Git("checkout", "-q", "feat")
	repo("onbase")
	repo("elsewhere").Git("checkout", "-q", "feat")
	clones["elsewhere"].Git("worktree", "add", "-q", filepath.Join(root, "elsewhere-master"), "master")
	noOrigin := testutil.NewRepo(t, filepath.Join(root, "noorigin"))
	noOrigin.Commit("init", map[string]string{"a.txt": "a\n"})
	noOrigin.Git("branch", "-M", "master")

	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	originMaster := func(name string) string { return ups[name].Git("rev-parse", "master") }

	// 获取全部: origin/* of every repo with an origin moves, local branches stay, the repo without one is no failure.
	fr, err := s.FetchAll(ctx, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if fr.Fetched != 5 || fr.NoRemote != 1 || fr.Failed != 0 || len(fr.Rows) != 6 {
		t.Errorf("fetch %+v", fr)
	}
	// A background index task may still be listed beside it, so find the fetch by kind.
	ts := s.Tasks()
	if i := slices.IndexFunc(ts, func(t Task) bool { return t.Kind == TaskFetch }); i < 0 || ts[i].State != TaskDone || ts[i].Done != 6 || ts[i].CanStop {
		t.Errorf("fetch task %+v", ts)
	}
	if clones["clean"].Git("rev-parse", "origin/master") != originMaster("clean") || clones["clean"].Git("rev-parse", "master") == originMaster("clean") {
		t.Error("fetch moved the wrong refs")
	}
	// The sync plan needs the updated remote tips.
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	head := func(name string) string { return clones[name].Git("rev-parse", "--abbrev-ref", "HEAD") }

	// Sync the starred branches. "origin/release" counts as release, a tag drops out.
	bp, err := s.BranchesPlan(ctx, BatchPlanInput{Branches: []string{"origin/release", "master", "v1.0", "release"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(bp.Branches, []string{"release", "master"}) {
		t.Errorf("branches %v", bp.Branches)
	}
	how := map[string]string{}
	for _, r := range bp.Rows {
		k := r.Branch + " " + r.Repo
		how[k] = r.How
		if r.Skip != "" {
			how[k] += " " + r.Skip
		}
	}
	for k, w := range map[string]string{
		"release clean": "ref", "release dirty": "ref", "release onbase": "ref", "release unpushed": "ref", "release elsewhere": "ref",
		"master clean": "ref", "master onbase": "worktree", "master dirty": "ref", "master unpushed": "skip unpushed",
		"master elsewhere": "worktree", "master noorigin": "skip no_remote",
	} {
		if how[k] != w {
			t.Errorf("branches plan %s: %q, want %q", k, how[k], w)
		}
	}
	if _, ok := how["release noorigin"]; ok {
		t.Error("a repo without the branch has a row")
	}
	releaseBefore := clones["dirty"].Git("rev-parse", "release")
	ur, err := s.BranchesRun(ctx, BatchRunInput{Picks: []BatchPick{{Repo: "clean", Branch: "release"}, {Repo: "dirty", Branch: "master"}, {Repo: "elsewhere", Branch: "master"}, {Repo: "unpushed", Branch: "master"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range ur.Rows {
		got[r.Branch+" "+r.Repo] = r.Outcome
	}
	for k, w := range map[string]string{"release clean": "updated", "master dirty": "updated", "master elsewhere": "updated", "master unpushed": "skipped"} {
		if got[k] != w {
			t.Errorf("branches run %s: %q, want %q (%+v)", k, got[k], w, ur.Rows)
		}
	}
	if len(ur.Rows) != 4 {
		t.Errorf("ran %d rows, only 4 were ticked", len(ur.Rows))
	}
	if clones["clean"].Git("rev-parse", "release") != ups["clean"].Git("rev-parse", "release") {
		t.Error("clean/release not fast-forwarded")
	}
	if clones["dirty"].Git("rev-parse", "master") != originMaster("dirty") || head("dirty") != "feat" {
		t.Error("dirty/master not fast-forwarded without a checkout")
	}
	if clones["dirty"].Git("rev-parse", "release") != releaseBefore {
		t.Error("an unticked branch moved")
	}
	if got, _ := os.ReadFile(filepath.Join(dirty.Dir, "a.txt")); string(got) != "edited\n" {
		t.Error("the dirty directory was touched")
	}
}

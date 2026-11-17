package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

// stageOwnFixture has one repo per case of judging a requirement by its own commits. Every repo has origin/master
// standing where master does, as a repo with a remote has.
func stageOwnFixture(t *testing.T) (s *Session, dirs map[string]string) {
	t.Helper()
	testutil.Isolate(t)
	root := t.TempDir()
	repo := func(name string) *testutil.Repo {
		r := testutil.NewRepo(t, filepath.Join(root, name))
		r.Commit("init", map[string]string{"f": "0\n"})
		return r
	}
	origin := func(r *testutil.Repo, branches ...string) {
		for _, b := range append(branches, "master") {
			r.Git("update-ref", "refs/remotes/origin/"+b, b)
		}
	}
	dirs = map[string]string{}

	// cherry-picked and squashed into master: the original SHAs are on a pushed branch only.
	cp := repo("cp")
	cp.Git("checkout", "-q", "-b", "feat-CPXX-1001")
	pick := cp.Commit("CPXX-1001 picked", map[string]string{"cp.txt": "1\n"})
	cp.Git("checkout", "-q", "master")
	cp.Git("cherry-pick", pick)
	origin(cp, "feat-CPXX-1001")
	sq := repo("sq")
	sq.Git("checkout", "-q", "-b", "feat-SQXX-1001")
	sq.Commit("SQXX-1001 squashed", map[string]string{"sq.txt": "1\n"})
	sq.Git("checkout", "-q", "master")
	sq.Git("merge", "-q", "--squash", "feat-SQXX-1001")
	sq.Commit("SQXX-1001 squashed (squash)", nil)
	origin(sq, "feat-SQXX-1001")

	// Cherry-picked into master, and the branch then merged master: the copy is in the branch's own history.
	cm := repo("cm")
	cm.Git("checkout", "-q", "-b", "feat-CMXX-1001")
	cmPick := cm.Commit("CMXX-1001 picked", map[string]string{"cm.txt": "1\n"})
	cm.Git("checkout", "-q", "master")
	cm.Git("cherry-pick", cmPick)
	cm.Commit("later master work", map[string]string{"later.txt": "1\n"})
	cm.Git("checkout", "-q", "feat-CMXX-1001")
	cm.Merge("master", "Merge branch 'master' into cm-feat")
	cm.Git("checkout", "-q", "master")
	origin(cm, "feat-CMXX-1001")
	// The original is only under a tag, so no branch of the row can be told, and its copy sits in master.
	nb := repo("nb")
	nb.Git("checkout", "-q", "-b", "nb-tmp")
	nbPick := nb.Commit("NBXX-1001 picked", map[string]string{"nb.txt": "1\n"})
	nb.Git("checkout", "-q", "master")
	nb.Git("tag", "nb-tag", nbPick)
	nb.Git("branch", "-q", "-D", "nb-tmp")
	nb.Git("cherry-pick", nbPick)
	origin(nb)
	// Same file in master, other content: not the same change.
	df := repo("df")
	df.Git("checkout", "-q", "-b", "feat-DFXX-1001")
	df.Commit("DFXX-1001 own change", map[string]string{"df.txt": "mine\n"})
	df.Git("checkout", "-q", "master")
	df.Commit("someone else", map[string]string{"df.txt": "theirs\n"})
	origin(df, "feat-DFXX-1001")

	// A branch that mixes two requirements; only the first is in master.
	mix := repo("mix")
	mix.Git("checkout", "-q", "-b", "feat-MIXA-1001-MIXB-1001")
	a := mix.Commit("MIXA-1001 first", map[string]string{"a.txt": "1\n"})
	mix.Commit("MIXB-1001 second", map[string]string{"b.txt": "1\n"})
	mix.Git("checkout", "-q", "master")
	mix.Git("cherry-pick", a)
	origin(mix, "feat-MIXA-1001-MIXB-1001")

	// Merge commits only: a clean one and one edited while merging, neither in master.
	mg := repo("mg")
	mg.Git("checkout", "-q", "-b", "side-m")
	mg.Commit("work m", map[string]string{"m.txt": "1\n"})
	mg.Git("checkout", "-q", "-b", "feat-MRGX-1001", "master")
	mg.Merge("side-m", "Merge branch 'MRGX-1001' into other-m")
	mg.Git("checkout", "-q", "-b", "side-h", "master")
	mg.Commit("work h", map[string]string{"h.txt": "1\n"})
	mg.Git("checkout", "-q", "-b", "feat-HANDX-1001", "master")
	mg.Git("merge", "-q", "--no-ff", "--no-commit", "side-h")
	mg.Commit("Merge branch 'HANDX-1001' into other-h", map[string]string{"extra.txt": "while merging\n"})
	mg.Git("checkout", "-q", "master")
	origin(mg, "feat-MRGX-1001", "feat-HANDX-1001")
	// Their landed commits elsewhere make the numbers requirements.
	land := repo("land")
	land.Commit("MRGX-1001 landed part", map[string]string{"x.txt": "1\n"})
	land.Commit("HANDX-1001 landed part", map[string]string{"y.txt": "1\n"})
	land.Commit("DHKX-1001 landed part", map[string]string{"z.txt": "1\n"})
	// A long-lived branch with uncommitted files, and a working directory cut for a number with no commit.
	land.Git("branch", "dev-hk")
	dirs["dev-hk"] = filepath.Join(root, "land-dev-hk")
	land.Git("worktree", "add", "-q", dirs["dev-hk"], "dev-hk")
	land.Git("branch", "GHOST-1001-x")
	dirs["ghost"] = filepath.Join(root, "land-ghost")
	land.Git("worktree", "add", "-q", dirs["ghost"], "GHOST-1001-x")
	origin(land)

	s, err := Open(context.Background(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_100_000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.SetMergeFlow([]MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for k, d := range dirs {
		dirs[k], _ = filepath.EvalSymlinks(d)
	}
	return s, dirs
}

func TestRequirementOwnCommits(t *testing.T) {
	ctx := context.Background()
	s, dirs := stageOwnFixture(t)
	got := stagesOf(t, s)
	// Before git has compared patches, a commit that is no ancestor counts as not in the line.
	for _, id := range []string{"CPXX-1001", "SQXX-1001", "MIXA-1001", "NBXX-1001"} {
		if got[id].Stage != MergeStage(1) {
			t.Errorf("%s before the patch comparison: %+v", id, got[id])
		}
	}
	changed, err := s.RequirementsSettle(ctx)
	if err != nil || !changed {
		t.Fatalf("settle: %v %v", changed, err)
	}
	got = stagesOf(t, s)
	for id, want := range map[string]struct {
		stage string
		repos int
	}{
		"CPXX-1001":  {StageLanded, 1},   // cherry-picked
		"SQXX-1001":  {StageLanded, 1},   // squashed
		"CMXX-1001":  {StageLanded, 1},   // the copy is in the branch's history too
		"NBXX-1001":  {StageLanded, 1},   // deleted branch, patch equivalence confirmed
		"DFXX-1001":  {MergeStage(1), 1}, // same file, another change
		"MIXA-1001":  {StageLanded, 1},   // the other requirement on its branch does not hold it back
		"MIXB-1001":  {MergeStage(1), 1}, // and it does not borrow the first one's landing
		"MRGX-1001":  {StageLanded, 1},   // the clean merge in mg is nobody's work
		"HANDX-1001": {StageLanded, 1},   // the edited one is not classified yet
		"DHKX-1001":  {StageLanded, 1},   // uncommitted files on dev-hk belong to no requirement
	} {
		if r, ok := got[id]; !ok || r.Stage != want.stage || r.Repos != want.repos {
			t.Errorf("%s = %+v (found %v), want %s in %d repos", id, r, ok, want.stage, want.repos)
		}
	}
	if r := got["GHOST-1001"]; r.Repos != 0 {
		t.Errorf("a clean working directory holds nothing of the requirement: %+v", r)
	}

	// Classified, the edited merge is the requirement's own work and is not in master.
	in := TagAggInput{Cond: &query.Conditions{Q: "HANDX-1001", Authors: []string{}}, Requirement: "HANDX-1001"}
	if changed, err := s.TagAggRemerge(ctx, in); err != nil || !changed {
		t.Fatalf("classify: %v %v", changed, err)
	}
	if r := stagesOf(t, s)["HANDX-1001"]; r.Stage != MergeStage(1) || r.Repos != 2 {
		t.Errorf("edited merge not in master: %+v", r)
	}
	if changed, _ := s.TagAggRemerge(ctx, TagAggInput{Cond: &query.Conditions{Q: "MRGX-1001", Authors: []string{}}, Requirement: "MRGX-1001"}); changed {
		t.Error("the clean merge must not count as edited")
	}
	if r := stagesOf(t, s)["MRGX-1001"]; r.Stage != StageLanded || r.Repos != 1 {
		t.Errorf("clean merge classified: %+v", r)
	}

	// Uncommitted files: under a branch of another name they are nobody's; under the number's own they are its.
	s.dirty.set(dirs["dev-hk"], 4)
	if r := stagesOf(t, s)["DHKX-1001"]; r.Stage != StageLanded {
		t.Errorf("dev-hk: %+v", r)
	}
	s.dirty.set(dirs["ghost"], 2)
	if r := stagesOf(t, s)["GHOST-1001"]; r.Stage != StageDev || r.Repos != 1 {
		t.Errorf("dirty working directory of the number: %+v", r)
	}
}

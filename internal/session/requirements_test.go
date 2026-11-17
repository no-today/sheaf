package session

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

// requirementFixture is one repo whose branches stand at every stage, plus a long-lived branch with no number in its name.
func requirementFixture(t *testing.T) (s *Session, pendDir string) {
	t.Helper()
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"f": "0\n"})
	r.Git("branch", "release")
	feature := func(branch, msg, file string) {
		r.Git("checkout", "-q", "-b", branch, "master")
		r.Commit(msg, map[string]string{file: msg + "\n"})
		r.Git("checkout", "-q", "master")
	}
	feature("land-1001", "LAND-1001 add landing", "land.txt")
	r.Merge("land-1001", "Merge branch 'land-1001'")
	feature("uat-1001", "[UAT-1001] in uat", "uat.txt")
	r.Git("checkout", "-q", "release")
	r.Merge("uat-1001", "Merge branch 'uat-1001' into release")
	r.Git("checkout", "-q", "master")
	feature("pend-1001", "PEND-1001: pushed", "pend.txt")
	r.Git("update-ref", "refs/remotes/origin/pend-1001", "pend-1001")
	feature("pend-1002", "PEND-1002 pushed, checked out", "pend2.txt")
	r.Git("update-ref", "refs/remotes/origin/pend-1002", "pend-1002")
	feature("dev-1001", "DEV-1001 not pushed", "dev.txt")
	// A long-lived branch: the number is only in the commit titles; its merge commit names a branch, not a requirement.
	r.Git("checkout", "-q", "-b", "mine", "master")
	r.Commit("PERS-1001 personal work", map[string]string{"pers.txt": "1\n"})
	r.Git("checkout", "-q", "-b", "side", "master")
	r.Commit("side work", map[string]string{"side.txt": "1\n"})
	r.Git("checkout", "-q", "mine")
	r.Merge("side", "Merge branch 'MERGE-1009' into mine")
	r.Git("checkout", "-q", "master")
	r.Git("update-ref", "refs/remotes/origin/master", "master")
	r.Git("update-ref", "refs/remotes/origin/release", "release")
	pendDir = filepath.Join(root, "pend-1002-wt")
	r.Git("worktree", "add", "-q", pendDir, "pend-1002")

	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_100_000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.SetMergeFlow([]MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	return s, pendDir
}

func stagesOf(t *testing.T, s *Session) map[string]Requirement {
	t.Helper()
	cands, err := s.RequirementCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if err := s.Track(c.ID); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.Requirements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Requirement{}
	for _, r := range list {
		out[r.ID] = r
	}
	return out
}

func TestRequirementStages(t *testing.T) {
	s, pend2 := requirementFixture(t)
	got := stagesOf(t, s)
	for id, want := range map[string]struct{ stage, title string }{
		"LAND-1001": {StageLanded, "add landing"},
		"UAT-1001":  {StageUAT, "in uat"},
		"PEND-1001": {StagePending, "pushed"},
		"PEND-1002": {StagePending, "pushed, checked out"},
		"DEV-1001":  {StagePending, "not pushed"},
		"PERS-1001": {StagePending, "personal work"},
	} {
		if r, ok := got[id]; !ok || r.Stage != want.stage || r.Title != want.title || r.Repos != 1 {
			t.Errorf("%s = %+v (found %v), want stage %s title %q in 1 repo", id, r, ok, want.stage, want.title)
		}
	}
	if _, ok := got["MERGE-1009"]; ok || len(got) != 6 {
		t.Errorf("a merge commit title must not make a requirement: %v", got)
	}

	// Uncommitted changes in the checked-out branch's working directory take it back to developing, and back again.
	pend2, _ = filepath.EvalSymlinks(pend2) // the paths git reports
	s.bg.Wait()                             // the dirty counts the refresh started would overwrite ours
	s.dirty.set(pend2, 3)
	if r := stagesOf(t, s)["PEND-1002"]; r.Stage != StageDev {
		t.Errorf("dirty working directory: %+v", r)
	}
	s.dirty.set(pend2, 0)
	if r := stagesOf(t, s)["PEND-1002"]; r.Stage != StagePending {
		t.Errorf("clean again: %+v", r)
	}
}

func TestRequirementOrderAndCache(t *testing.T) {
	s, _ := requirementFixture(t)
	list, err := s.Requirements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rank := -1
	for i, r := range list {
		if StageRank(r.Stage) < rank {
			t.Errorf("stages must group developing first: %v", list)
		}
		rank = StageRank(r.Stage)
		if i > 0 && list[i-1].Stage == r.Stage && list[i-1].Latest < r.Latest {
			t.Errorf("newest first within a stage: %v", list)
		}
	}
	again, _ := s.Requirements(context.Background())
	if len(again) != len(list) || s.reqs.key.eng == nil {
		t.Errorf("cached list differs: %v", again)
	}
}

func TestRequirementPattern(t *testing.T) {
	s, _ := requirementFixture(t)
	if err := s.SetRequirement("[unclosed"); err == nil || s.CheckRequirement("[unclosed") == "" {
		t.Fatal("an invalid pattern must be refused")
	}
	if got := s.Info().Requirement; got != DefaultRequirement {
		t.Errorf("a refused pattern was saved: %q", got)
	}
	if err := s.SetRequirement(`PEND-\d+`); err != nil {
		t.Fatal(err)
	}
	got := stagesOf(t, s)
	if len(got) != 2 || got["PEND-1001"].ID == "" {
		t.Errorf("custom pattern: %v", got)
	}
	if err := s.SetRequirement(""); err != nil || s.Info().Requirement != DefaultRequirement {
		t.Errorf("empty restores the default: %v %q", err, s.Info().Requirement)
	}
}

func TestRequirementTitle(t *testing.T) {
	re := regexp.MustCompile(DefaultRequirement)
	for _, c := range []struct{ subject, id, want string }{
		{"ABC-1001 fix", "ABC-1001", "fix"},
		{"[ABC-1001] fix: it", "ABC-1001", "fix: it"},
		{"feat: ABC-1001 - fix", "ABC-1001", "feat: fix"},
		{"ABC-1001：修复", "ABC-1001", "修复"},
		{"ABC-100122 fix", "ABC-1001", ""}, // another number that starts the same
	} {
		got, ok := requirementTitle(re, c.subject, c.id)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%q: %q %v", c.subject, got, ok)
		}
	}
}

// A working directory cut from the base with no commit yet is developing only while it holds uncommitted files.
func TestRequirementWithoutCommits(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	a := testutil.NewRepo(t, filepath.Join(root, "a"))
	a.Commit("init", map[string]string{"f": "0\n"})
	a.Git("checkout", "-q", "-b", "NEW-2001-x")
	a.Commit("NEW-2001 landed in a", map[string]string{"a.txt": "1\n"})
	a.Git("checkout", "-q", "master")
	a.Merge("NEW-2001-x", "Merge branch 'NEW-2001-x'")
	a.Git("branch", "-q", "-d", "NEW-2001-x")
	// A number that starts like another: the short one has landed, the long one has an unmerged branch.
	a.Commit("ABC-12345 short", map[string]string{"short.txt": "1\n"})
	a.Git("checkout", "-q", "-b", "feat-ABC-123456")
	a.Commit("ABC-123456 long", map[string]string{"long.txt": "1\n"})
	a.Git("checkout", "-q", "master")
	a.Git("update-ref", "refs/remotes/origin/master", "master")
	b := testutil.NewRepo(t, filepath.Join(root, "b"))
	b.Commit("init", map[string]string{"f": "0\n"})
	b.Git("branch", "NEW-2001-b")
	b.Git("branch", "ONLY-3002")
	wt, only := filepath.Join(root, "b-wt"), filepath.Join(root, "b-only")
	b.Git("worktree", "add", "-q", wt, "NEW-2001-b")
	b.Git("worktree", "add", "-q", only, "ONLY-3002")
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_100_000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.SetMergeFlow([]MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := filepath.EvalSymlinks(wt)
	s.bg.Wait() // the dirty counts the refresh started would overwrite ours
	s.dirty.set(p, 3)
	if got := stagesOf(t, s)["NEW-2001"]; got.Stage != StageDev || got.Repos != 2 {
		t.Errorf("dirty: landed in a, uncommitted files in b: %+v", got)
	}
	s.dirty.set(p, 0)
	if got := stagesOf(t, s)["NEW-2001"]; got.Stage != StageLanded || got.Repos != 1 {
		t.Errorf("clean: the working directory in b has nothing of it: %+v", got)
	}
	got := stagesOf(t, s)
	if r := got["ONLY-3002"]; r.Stage != StageDev || r.Repos != 0 {
		t.Errorf("a clean working directory named after a number holds nothing of it: %+v", r)
	}
	po, _ := filepath.EvalSymlinks(only)
	s.dirty.set(po, 2)
	got = stagesOf(t, s)
	if r, ok := got["ONLY-3002"]; !ok || r.Stage != StageDev || r.Repos != 1 || r.Title != "" {
		t.Errorf("a number found only in a dirty branch name: %+v (found %v)", r, ok)
	}
	if r := got["ABC-12345"]; r.Stage != StageLanded || r.Title != "short" {
		t.Errorf("ABC-12345 must not take the commits of ABC-123456: %+v", r)
	}
	if r := got["ABC-123456"]; r.Stage != MergeStage(1) {
		t.Errorf("ABC-123456: %+v", r)
	}
}

func TestRequirementNameOverridesTitleAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	s, _ := requirementFixture(t)
	if err := s.SetRequirementName("LAND-1001", "  Landing page  "); err != nil {
		t.Fatal(err)
	}
	if r := stagesOf(t, s)["LAND-1001"]; r.Title != "Landing page" || !r.Named {
		t.Errorf("named: %+v", r)
	}
	if r := stagesOf(t, s)["UAT-1001"]; r.Named || r.Title != "in uat" {
		t.Errorf("another requirement changed: %+v", r)
	}
	s2, err := Open(ctx, s.Root, Options{DataDir: s.opts.DataDir, CacheDir: t.TempDir(), Now: s.opts.Now})
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if r := stagesOf(t, s2)["LAND-1001"]; r.Title != "Landing page" || !r.Named {
		t.Errorf("after reopening: %+v", r)
	}
	s2.Close()
	if err := s.SetRequirementName("LAND-1001", "   "); err != nil {
		t.Fatal(err)
	}
	if r := stagesOf(t, s)["LAND-1001"]; r.Title != "add landing" || r.Named {
		t.Errorf("cleared: %+v", r)
	}
}

func TestInfoScannedListsIgnoredReposToo(t *testing.T) {
	s, _ := requirementFixture(t)
	if got := s.Info().Scanned; len(got) != 1 || got[0] != "repo" {
		t.Fatalf("scanned %v", got)
	}
	if err := s.SetIgnored([]string{"repo"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Info().Scanned; len(got) != 1 || got[0] != "repo" {
		t.Errorf("an ignored repo must stay listed to be switched back on: %v", got)
	}
}

func TestRequirementTracking(t *testing.T) {
	ctx := context.Background()
	s, _ := requirementFixture(t)
	if list, err := s.Requirements(ctx); err != nil || len(list) != 0 {
		t.Fatalf("an untouched config tracks nothing: %v %v", list, err)
	}
	cands, err := s.RequirementCandidates(ctx)
	if err != nil || len(cands) != 6 {
		t.Fatalf("candidates = %v %v", cands, err)
	}
	for i := 1; i < len(cands); i++ {
		if cands[i-1].Latest < cands[i].Latest {
			t.Errorf("candidates newest first: %v", cands)
		}
	}
	if err := s.Track("LAND-1001"); err != nil {
		t.Fatal(err)
	}
	if err := s.Track("LAND-1001"); err != nil {
		t.Fatal(err)
	}
	if err := s.Track("NOPE-9999"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Requirements(ctx)
	if len(list) != 2 {
		t.Fatalf("tracked two: %v", list)
	}
	if r := stageOnly(list, "LAND-1001"); r.Stage != StageLanded || r.Title != "add landing" {
		t.Errorf("LAND-1001: %+v", r)
	}
	if r := stageOnly(list, "NOPE-9999"); r.Stage != StageDev || r.Repos != 0 || r.Title != "" {
		t.Errorf("a number found nowhere: %+v", r)
	}
	cands, _ = s.RequirementCandidates(ctx)
	for _, c := range cands {
		if c.ID == "LAND-1001" {
			t.Errorf("a tracked number is no candidate: %v", cands)
		}
	}
	if c := candidateOf(cands, "UAT-1001"); c.Title != "in uat" || c.Latest == 0 {
		t.Errorf("candidate: %+v", c)
	}

	if err := s.SetRequirementName("LAND-1001", "Mine"); err != nil {
		t.Fatal(err)
	}
	reopen := func() *Session {
		s2, err := Open(ctx, s.Root, Options{DataDir: s.opts.DataDir, CacheDir: t.TempDir(), Now: s.opts.Now})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s2.Close)
		if err := s2.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		return s2
	}
	s2 := reopen()
	if got := s2.Tracked(); len(got) != 2 || got[0] != "LAND-1001" || got[1] != "NOPE-9999" {
		t.Errorf("after reopening: %v", got)
	}
	if err := s.Untrack("LAND-1001"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Requirements(ctx); len(list) != 1 || list[0].ID != "NOPE-9999" {
		t.Errorf("untracked: %v", list)
	}
	if err := s.Track("LAND-1001"); err != nil { // the undo of an untrack
		t.Fatal(err)
	}
	if r := stageOnly(mustReqs(t, s), "LAND-1001"); r.Title != "Mine" || !r.Named {
		t.Errorf("the name outlives untracking: %+v", r)
	}
}

func TestNormalizeRequirement(t *testing.T) {
	s, _ := requirementFixture(t)
	for in, want := range map[string]string{"ABC-123": "ABC-123", " ab2-7 ": "AB2-7", "YT03859": "", "A-12": "", "AB123": "", "hello": "", "ABC-123 x": ""} {
		if got, ok := s.NormalizeRequirement(in); got != want || ok != (want != "") {
			t.Errorf("NormalizeRequirement(%q) = %q, %v", in, got, ok)
		}
	}
}

func mustReqs(t *testing.T, s *Session) []Requirement {
	t.Helper()
	list, err := s.Requirements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func stageOnly(list []Requirement, id string) Requirement {
	for _, r := range list {
		if r.ID == id {
			return r
		}
	}
	return Requirement{}
}

func candidateOf(list []Candidate, id string) Candidate {
	for _, c := range list {
		if c.ID == id {
			return c
		}
	}
	return Candidate{}
}

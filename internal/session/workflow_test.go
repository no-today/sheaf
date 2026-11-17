package session

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestWorkspaceMergeFlowAndArchive(t *testing.T) {
	testutil.Isolate(t)
	ctx := t.Context()
	root := t.TempDir()
	data, cache := t.TempDir(), t.TempDir()
	repositories := map[string]*testutil.Repo{}
	for _, name := range []string{"alpha", "beta"} {
		repo := testutil.NewRepo(t, filepath.Join(root, name))
		base := repo.Commit("init", map[string]string{"f": "base\n"})
		repo.Git("checkout", "-q", "-b", "feat-FLOW-1001")
		own := repo.Commit("FLOW-1001 work", map[string]string{"f": "feature\n"})
		repo.Git("checkout", "-q", "master")
		for _, branch := range []string{"qa", "stable", "prod"} {
			repo.Git("update-ref", "refs/remotes/origin/"+branch, base)
		}
		repo.Git("update-ref", "refs/remotes/origin/feature", own)
		repositories[name] = repo
	}
	repositories["alpha"].Git("update-ref", "refs/remotes/origin/qa", "feat-FLOW-1001")
	sess, err := Open(ctx, root, Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	flow := []MergeStep{{Name: "验收", Branch: "qa"}, {Name: "发布", Branch: "stable"}, {Name: "基线", Branch: "prod"}}
	if err := sess.SetMergeFlow(flow); err != nil {
		t.Fatal(err)
	}
	if err := sess.Track("FLOW-1001"); err != nil {
		t.Fatal(err)
	}
	if err := sess.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	in := TagAggInput{Cond: &query.Conditions{Q: "FLOW-1001", Authors: []string{}}, Requirement: "FLOW-1001"}
	result, err := sess.TagAgg(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range result.Repos {
		want := MergeStage(0)
		if row.Repo == "alpha" {
			want = MergeStage(1)
		}
		if row.Stage != want {
			t.Errorf("repo %s stage=%s want=%s", row.Repo, row.Stage, want)
		}
	}
	archived, progress := false, MergeStage(0)
	check := func(want string) {
		t.Helper()
		reqs, err := sess.Requirements(ctx)
		if err != nil || len(reqs) != 1 || reqs[0].Stage != want || reqs[0].Repos != 2 {
			t.Fatalf("requirements=%+v err=%v want=%s", reqs, err, want)
		}
		if !archived {
			progress = want
		}
		if reqs[0].Archived != archived || reqs[0].Progress != progress {
			t.Fatalf("archive state=%+v, want archived=%v progress=%s", reqs[0], archived, progress)
		}
	}
	check(MergeStage(0))
	repositories["beta"].Git("update-ref", "refs/remotes/origin/qa", "feat-FLOW-1001")
	if err := sess.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check(MergeStage(1))
	if err := sess.SetMergeFlow([]MergeStep{flow[1], flow[0], flow[2]}); err != nil {
		t.Fatal(err)
	}
	check(MergeStage(2))
	for _, repo := range repositories {
		repo.Git("update-ref", "refs/remotes/origin/prod", "feat-FLOW-1001")
	}
	if err := sess.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check(StageLanded)
	if err := sess.ArchiveRequirement("FLOW-1001", true); err != nil {
		t.Fatal(err)
	}
	archived = true
	check(StageArchived)
	sess.Close()
	sess, err = Open(ctx, root, Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check(StageArchived)
	repo := repositories["alpha"]
	repo.Git("checkout", "-q", "feat-FLOW-1001")
	if err := os.WriteFile(filepath.Join(repo.Dir, "f"), []byte("unfinished\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sess.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	sess.RefreshDirty(ctx, nil, true)
	progress = StageDev
	check(StageArchived)
	if err := sess.ArchiveRequirement("FLOW-1001", false); err != nil {
		t.Fatal(err)
	}
	archived = false
	check(StageDev)
	if got := sess.Info().MergeFlow; len(got) != 3 || got[0].Branch != "stable" {
		t.Fatalf("flow did not survive reopen: %+v", got)
	}
}

func TestUnpushedCommitIsWaitingForMerge(t *testing.T) {
	sess, _ := requirementFixture(t)
	got := stagesOf(t, sess)
	if got["DEV-1001"].Stage != MergeStage(0) {
		t.Errorf("DEV-1001 stage=%s want first merge target", got["DEV-1001"].Stage)
	}
}

func TestMergeFlowSkipsMissingIntermediateBranches(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
	repo.Commit("init", map[string]string{"f": "base\n"})
	repo.Git("checkout", "-q", "-b", "feat-FLOW-1001")
	repo.Commit("FLOW-1001 work", map[string]string{"f": "feature\n"})
	repo.Git("checkout", "-q", "master")
	repo.Git("branch", "qa")
	sess, err := Open(t.Context(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.Close)
	if err := sess.SetMergeFlow([]MergeStep{{Name: "验收", Branch: "qa"}, {Name: "发布", Branch: "release"}, {Name: "基线", Branch: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Track("FLOW-1001"); err != nil {
		t.Fatal(err)
	}
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		reqs, err := sess.Requirements(t.Context())
		if err != nil || len(reqs) != 1 || reqs[0].Stage != want {
			t.Fatalf("progress=%+v err=%v want=%s", reqs, err, want)
		}
	}
	check(MergeStage(0))
	repo.Git("branch", "-f", "qa", "feat-FLOW-1001")
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	check(MergeStage(2))
	repo.Git("update-ref", "refs/heads/master", "feat-FLOW-1001")
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	check(StageLanded)
	if err := sess.SetMergeFlow([]MergeStep{{Name: "验收", Branch: "qa"}, {Name: "基线", Branch: "missing"}}); err != nil {
		t.Fatal(err)
	}
	check(MergeStage(1))
}

func TestRepositoryCanCheckOnlyBaseline(t *testing.T) {
	testutil.Isolate(t)
	root, data, cache := t.TempDir(), t.TempDir(), t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		repo := testutil.NewRepo(t, filepath.Join(root, name))
		repo.Commit("init", map[string]string{"f": "base\n"})
		repo.Git("branch", "release")
		repo.Git("checkout", "-q", "-b", "feat-FLOW-1001")
		repo.Commit("FLOW-1001 work", map[string]string{"f": name + "\n"})
		repo.Git("checkout", "-q", "master")
	}
	sess, err := Open(t.Context(), root, Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	if err := sess.SetMergeFlow([]MergeStep{{Name: "发布", Branch: "release"}, {Name: "基线", Branch: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Track("FLOW-1001"); err != nil {
		t.Fatal(err)
	}
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	input := TagAggInput{Cond: &query.Conditions{Q: "FLOW-1001", Authors: []string{}}, Requirement: "FLOW-1001", DiffMode: DiffStage}
	if _, err := sess.Requirements(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := sess.SetRepoBaselineOnly("beta", true); err != nil {
		t.Fatal(err)
	}
	result, err := sess.TagAgg(t.Context(), input)
	if err != nil || len(result.Repos) != 2 {
		t.Fatalf("overview: %+v, %v", result, err)
	}
	for _, row := range result.Repos {
		want := MergeStage(0)
		if row.Repo == "beta" {
			want = MergeStage(1)
		}
		if row.Stage != want {
			t.Errorf("%s stage=%s want=%s", row.Repo, row.Stage, want)
		}
	}
	files, err := sess.TagAggFiles(t.Context(), input)
	if err != nil || len(files) != 1 || files[0].Repo != "alpha" {
		t.Fatalf("release diff: %+v, %v", files, err)
	}
	stats, err := sess.TagAggStats(t.Context(), input, "beta")
	if err != nil || len(stats) != 0 {
		t.Fatalf("skipped repository stats: %+v, %v", stats, err)
	}
	patch, err := sess.TagAggPatch(t.Context(), input, "beta", "f", "", false, false, false)
	if err != nil || len(patch.Parts) != 0 {
		t.Fatalf("skipped repository patch: %+v, %v", patch, err)
	}
	input.DiffStage = 1
	files, err = sess.TagAggFiles(t.Context(), input)
	if err != nil || len(files) != 2 {
		t.Fatalf("baseline diff: %+v, %v", files, err)
	}
	if err := sess.SetRepoBaselineOnly("alpha", true); err != nil {
		t.Fatal(err)
	}
	reqs, err := sess.Requirements(t.Context())
	if err != nil || len(reqs) != 1 || reqs[0].Stage != MergeStage(1) {
		t.Fatalf("updated cached progress: %+v, %v", reqs, err)
	}
	sess.Close()
	sess, err = Open(t.Context(), root, Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, repo := range sess.Info().Repos {
		if !repo.BaselineOnly {
			t.Errorf("setting did not survive reopening: %+v", repo)
		}
	}
	if err := sess.SetRepoBaselineOnly("alpha", false); err != nil {
		t.Fatal(err)
	}
	reqs, err = sess.Requirements(t.Context())
	if err != nil || len(reqs) != 1 || reqs[0].Stage != MergeStage(0) {
		t.Fatalf("restored flow: %+v, %v", reqs, err)
	}
}

func TestMergeFlowIncludesWorkAlreadyInCompareBase(t *testing.T) {
	for _, merged := range []bool{false, true} {
		name := "ordinary commit"
		if merged {
			name = "merge commit"
		}
		t.Run(name, func(t *testing.T) {
			testutil.Isolate(t)
			ctx := t.Context()
			root := t.TempDir()
			repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
			repo.Commit("init", map[string]string{"f": "base\n"})
			repo.Git("branch", "prod")
			if merged {
				repo.Git("checkout", "-q", "-b", "side")
				repo.Commit("unrelated work", map[string]string{"side.txt": "side\n"})
				repo.Git("checkout", "-q", "master")
				repo.Git("merge", "-q", "--no-ff", "--no-commit", "side")
			}
			first := repo.Commit("FLOW-1001 first", map[string]string{"first.txt": "first\n"})
			repo.Git("checkout", "-q", "-b", "feat-FLOW-1001")
			second := repo.Commit("FLOW-1001 second", map[string]string{"second.txt": "second\n"})
			repo.Git("checkout", "-q", "prod")
			repo.Git("cherry-pick", second)
			repo.Git("checkout", "-q", "master")
			sess, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			if err := sess.SetMergeFlow([]MergeStep{{Name: "prod", Branch: "prod"}}); err != nil {
				t.Fatal(err)
			}
			if err := sess.Track("FLOW-1001"); err != nil {
				t.Fatal(err)
			}
			if err := sess.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			in := TagAggInput{Cond: &query.Conditions{Q: "FLOW-1001", Authors: []string{}}, Requirement: "FLOW-1001"}
			check := func(want string) {
				t.Helper()
				if _, err := sess.TagAggRemerge(ctx, in); err != nil {
					t.Fatal(err)
				}
				if _, err := sess.RequirementsSettle(ctx); err != nil {
					t.Fatal(err)
				}
				reqs, err := sess.Requirements(ctx)
				if err != nil || len(reqs) != 1 || reqs[0].Stage != want {
					t.Fatalf("requirements=%+v err=%v want=%s", reqs, err, want)
				}
				result, err := sess.TagAgg(ctx, in)
				if err != nil || len(result.Repos) != 1 || result.Repos[0].Stage != want {
					t.Fatalf("rows=%+v err=%v want=%s", result.Repos, err, want)
				}
			}
			check(StagePending)
			repo.Git("checkout", "-q", "prod")
			if merged {
				repo.Git("cherry-pick", "-m", "1", first)
				repo.Merge("master", "merge baseline")
			} else {
				repo.Git("cherry-pick", first)
			}
			repo.Git("checkout", "-q", "master")
			if err := sess.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			check(StageLanded)
		})
	}
}

func TestDefaultMergeFlow(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want []MergeStep
	}{
		{"empty workspace", Config{}, []MergeStep{{Name: "main"}}},
		{"saved master", Config{Master: "origin/main"}, []MergeStep{{Name: "origin/main", Branch: "origin/main"}}},
		{"legacy release", Config{Release: "release"}, []MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}},
		{"legacy targets", Config{Release: "uat", Master: "main"}, []MergeStep{{Name: "uat", Branch: "uat"}, {Name: "main", Branch: "main"}}},
		{"custom flow", Config{Release: "release", MergeFlow: []MergeStep{{Name: "production", Branch: "prod"}}}, []MergeStep{{Name: "production", Branch: "prod"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{cfg: tc.cfg}
			if got := s.MergeFlow(); !slices.Equal(got, tc.want) {
				t.Fatalf("flow = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMergeFlowSettingsAreScopedAndMigrateCompareTargets(t *testing.T) {
	ctx := t.Context()
	data, cache := t.TempDir(), t.TempDir()
	sess, err := Open(ctx, t.TempDir(), Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	sess.mu.Lock()
	sess.cfg.Release, sess.cfg.Master = "uat", "main"
	err = sess.saveConfig()
	sess.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defaults := []MergeStep{{Name: "uat", Branch: "uat"}, {Name: "main", Branch: "main"}}
	if !slices.Equal(sess.MergeFlow(), defaults) {
		t.Fatalf("old settings not inherited: %+v", sess.MergeFlow())
	}
	flow := []MergeStep{{Name: " production ", Branch: " origin/prod "}}
	if err := sess.SetMergeFlow(flow); err != nil {
		t.Fatal(err)
	}
	flow[0].Branch = "mutated"
	expected := []MergeStep{{Name: "production", Branch: "origin/prod"}}
	for _, invalid := range [][]MergeStep{
		nil,
		{{}},
		{{Name: "one", Branch: "prod"}, {Name: "two", Branch: "prod"}},
		{{Name: "one", Branch: "prod"}, {Name: "one", Branch: "qa"}},
		{{Name: "bad\nname", Branch: "prod"}},
	} {
		if err := sess.SetMergeFlow(invalid); err == nil {
			t.Fatalf("accepted invalid flow: %+v", invalid)
		}
	}
	if err := sess.SetCompareTargets("another", "other"); err != nil {
		t.Fatal(err)
	}
	info := sess.Info()
	info.MergeFlow[0].Branch = "mutated info"
	if !slices.Equal(sess.MergeFlow(), expected) {
		t.Fatalf("custom flow changed: %+v", sess.MergeFlow())
	}
	other, err := Open(ctx, t.TempDir(), Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if slices.Equal(other.MergeFlow(), expected) {
		t.Fatal("flow leaked into another workspace")
	}
	if err := sess.ArchiveRequirement("not-tracked", true); err == nil {
		t.Fatal("archived an untracked requirement")
	}
}

func TestCompareTargetsKeepDefaultMergeFlowAfterReopen(t *testing.T) {
	ctx := t.Context()
	root, data, cache := t.TempDir(), t.TempDir(), t.TempDir()
	sess, err := Open(ctx, root, Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	expected := sess.MergeFlow()
	if err := sess.SetCompareTargets("uat", "develop"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sess.MergeFlow(), expected) {
		t.Fatalf("viewing options changed the flow: got %+v, want %+v", sess.MergeFlow(), expected)
	}
	sess.Close()
	sess, err = Open(ctx, root, Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if !slices.Equal(sess.MergeFlow(), expected) {
		t.Fatalf("flow changed after reopen: got %+v, want %+v", sess.MergeFlow(), expected)
	}
}

func TestCompareBaseKeepsMergeFlowAfterReopen(t *testing.T) {
	ctx := t.Context()
	root, data, cache := t.TempDir(), t.TempDir(), t.TempDir()
	sess, err := Open(ctx, root, Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	expected := sess.MergeFlow()
	if err := sess.SetCompareBase("origin/main"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sess.MergeFlow(), expected) {
		t.Fatal("comparison base changed the default flow")
	}

	sess.Close()
	sess, err = Open(ctx, root, Options{DataDir: data, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if sess.Info().Master != "origin/main" || !slices.Equal(sess.MergeFlow(), expected) {
		t.Fatal("comparison base or flow changed after reopening")
	}
}

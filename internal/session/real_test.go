package session

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"sheaf/internal/query"
)

// SHEAF_BENCH_ROOT=/path/to/large-workspace go test -run Real -v ./internal/session
// Temporary aggregation on real data: my last 30 days, plus SHEAF_BENCH_TAG as search text if set.
// TagAgg is index only and should stay within the 50 ms budget; TagAggFiles and TagAggPatch ask git.
func TestRealTagAgg(t *testing.T) {
	root := os.Getenv("SHEAF_BENCH_ROOT")
	if root == "" {
		t.Skip("SHEAF_BENCH_ROOT not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	bases := map[string]string{}
	for _, r := range s.eng.Index().Repos {
		for _, b := range []string{"origin/master", "origin/main", "master", "main"} {
			if _, ok := r.Ref(b); ok {
				bases[r.Name] = b
				break
			}
		}
	}
	if err := s.SetBases(bases); err != nil {
		t.Fatal(err)
	}
	conds := []query.Conditions{query.Default()}
	if q := os.Getenv("SHEAF_BENCH_TAG"); q != "" {
		conds = append(conds, query.Conditions{Q: q, Authors: []string{}})
	}
	for _, c := range conds {
		in := TagAggInput{Cond: &c}
		var best time.Duration
		var res TagAggResult
		for range 5 {
			t0 := time.Now()
			if res, err = s.TagAgg(ctx, in); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(t0); best == 0 || d < best {
				best = d
			}
		}
		from := map[string]int{}
		for _, r := range res.Repos {
			from[r.BranchFrom]++
		}
		t1 := time.Now()
		files, err := s.TagAggFiles(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		d1 := time.Since(t1)
		var d2 time.Duration
		if len(files) > 0 {
			f := files[0]
			t2 := time.Now()
			if _, err := s.TagAggPatch(ctx, in, f.Repo, f.Path, "", f.Untracked, f.Worktree, false); err != nil {
				t.Fatal(err)
			}
			d2 = time.Since(t2)
		}
		// The branch dropdown of the slowest row (the filter page reads it when a row's branch is clicked).
		var d3 time.Duration
		for _, r := range res.Repos {
			t3 := time.Now()
			if _, err := s.TagAggBranches(in, r.Key); err != nil {
				t.Fatal(err)
			}
			d3 = max(d3, time.Since(t3))
		}
		t.Logf("%+v: %d commits in %d repos (branches %v), TagAgg %v; TagAggFiles %d files %v; TagAggPatch %v; TagAggBranches max %v",
			c, res.Commits, len(res.Repos), from, best, len(files), d1, d2, d3)
		if best > 50*time.Millisecond {
			t.Errorf("TagAgg %v, over the 50 ms budget", best)
		}
	}
}

func TestRealRequirementBranches(t *testing.T) {
	root, requirement := os.Getenv("SHEAF_BENCH_ROOT"), os.Getenv("SHEAF_BENCH_TAG")
	if root == "" || requirement == "" {
		t.Skip("SHEAF_BENCH_ROOT and SHEAF_BENCH_TAG not set")
	}
	started := time.Now()
	sess, err := Open(t.Context(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.Close)
	if pattern := os.Getenv("SHEAF_BENCH_PATTERN"); pattern != "" {
		if err := sess.SetRequirement(pattern); err != nil {
			t.Fatal(err)
		}
	}
	if err := sess.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	baselineOnly := os.Getenv("SHEAF_BENCH_BASELINE_ONLY")
	if baselineOnly != "" {
		if err := sess.SetRepoBaselineOnly(baselineOnly, true); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("index ready in %v", time.Since(started).Round(time.Millisecond))
	in := TagAggInput{Cond: &query.Conditions{Q: requirement, Authors: []string{}}, Requirement: requirement, Base: "origin/master", DiffMode: DiffBase}
	var result TagAggResult
	var best time.Duration
	for range 5 {
		started = time.Now()
		result, err = sess.TagAgg(t.Context(), in)
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); best == 0 || elapsed < best {
			best = elapsed
		}
	}
	for _, row := range result.Repos {
		t.Logf("%s: branch=%q from=%s stage=%s commits=%d worktree=%q uncommitted=%d", row.Repo, row.Branch, row.BranchFrom, row.Stage, row.Commits, row.Worktree, row.Uncommitted)
	}
	t.Logf("requirement aggregation: %v", best)
	in.DiffMode = DiffWorktree
	files, err := sess.TagAggFiles(t.Context(), in)
	if err != nil || len(files) != result.Uncommitted {
		t.Fatalf("uncommitted files=%d overview=%d error=%v", len(files), result.Uncommitted, err)
	}
	t.Logf("requirement uncommitted files: %d", len(files))
	if baselineOnly != "" {
		in.DiffMode, in.DiffStage = DiffStage, 0
		files, err := sess.TagAggFiles(t.Context(), in)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if file.Repo == baselineOnly {
				t.Errorf("baseline-only repository appears in intermediate diff: %+v", file)
			}
		}
		t.Logf("intermediate stage excludes %s", baselineOnly)
	}
	if best > 50*time.Millisecond {
		t.Errorf("over the 50 ms budget: %v", best)
	}
}

// The built-in 未提交 page: Dirty re-runs after every index / status push while the page is open (git status in each
// non-clean working directory, 8 at a time).
func TestRealDirty(t *testing.T) {
	root := os.Getenv("SHEAF_BENCH_ROOT")
	if root == "" {
		t.Skip("SHEAF_BENCH_ROOT not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	var one string
	for i := range 5 {
		t0 := time.Now()
		v, err := s.Dirty(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Dirty #%d: %d groups, %d files, %v", i, len(v.Groups), len(v.Files), time.Since(t0))
		if i == 0 && len(v.Groups) > 0 {
			one = v.Groups[0].Dirs[0].Path
		}
	}
	if one != "" {
		for i := range 3 {
			s.dirty.invalidate([]string{one}) // the watch reported one directory
			t0 := time.Now()
			if _, err := s.Dirty(ctx); err != nil {
				t.Fatal(err)
			}
			t.Logf("Dirty after one directory changed #%d: %v", i, time.Since(t0))
		}
	}
}

// The requirement list on real data: the first computation (cold) must stay within 150 ms (it is computed once per index version, off the UI thread); a repeat is cached.
func TestRealRequirements(t *testing.T) {
	root := os.Getenv("SHEAF_BENCH_ROOT")
	if root == "" {
		t.Skip("SHEAF_BENCH_ROOT not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// SHEAF_BENCH_CONFIG is a workspace.json to read the ignored repos and the main branches from (never written).
	if path := os.Getenv("SHEAF_BENCH_CONFIG"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Bases   map[string]string `json:"bases"`
			Ignored []string          `json:"ignored"`
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		if err := s.SetBases(cfg.Bases); err != nil {
			t.Fatal(err)
		}
		if err := s.SetIgnored(cfg.Ignored); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	count := func(list []Requirement) (map[string]int, string) {
		stages, named := map[string]int{}, ""
		for _, r := range list {
			stages[r.Stage]++
			if r.Stage == StageDev || slices.Contains([]string{"YT01205", "YT03818", "YT03428"}, r.ID) {
				named += " " + r.ID + "=" + r.Stage
			}
		}
		return stages, named
	}
	// The default pattern and SHEAF_BENCH_REQ (the workspace's own) if set.
	for _, pattern := range []string{"", os.Getenv("SHEAF_BENCH_REQ")} {
		if err := s.SetRequirement(pattern); err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		list, err := s.Requirements(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cold := time.Since(t0)
		t1 := time.Now()
		if _, err := s.Requirements(ctx); err != nil {
			t.Fatal(err)
		}
		stages, named := count(list)
		t.Logf("pattern=%q requirements=%d stages=%v cold=%v cached=%v%s", pattern, len(list), stages, cold.Round(100*time.Microsecond), time.Since(t1).Round(time.Microsecond), named)
		if cold > 150*time.Millisecond {
			t.Errorf("over budget: %v", cold)
		}
		t2 := time.Now()
		if _, err := s.RequirementsSettle(ctx); err != nil {
			t.Fatal(err)
		}
		settle := time.Since(t2)
		if list, err = s.Requirements(ctx); err != nil {
			t.Fatal(err)
		}
		stages, named = count(list)
		t.Logf("pattern=%q settled in %v: requirements=%d stages=%v%s", pattern, settle.Round(time.Millisecond), len(list), stages, named)
	}
}

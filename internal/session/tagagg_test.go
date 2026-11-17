package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestTagAgg(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	tmp, _ := filepath.EvalSymlinks(t.TempDir()) // worktree paths come back resolved (/private/var on macOS)
	root := filepath.Join(tmp, "w")
	repo := func(name string) *testutil.Repo {
		r := testutil.NewRepo(t, filepath.Join(root, name))
		r.Commit("init", map[string]string{"a.txt": "a\n", "gone.txt": "g\n"})
		r.Git("branch", "-M", "master")
		return r
	}
	// a: YT1 on feat (checked out in a worktree, with uncommitted changes); "partial" holds only the first commit;
	// master moved on after the fork.
	a := repo("a")
	wt := filepath.Join(root, "a-feat")
	a.Git("worktree", "add", "-q", "-b", "feat", wt)
	w := testutil.NewRepoAt(t, wt)
	w.Commit("YT1 one", map[string]string{"n.txt": "n\n", "a.txt": "a1\n"})
	a.Git("branch", "partial", "feat")
	w.Commit("YT1 two", map[string]string{"n.txt": "n2\n", "gone.txt": ""})
	a.Commit("master moves", map[string]string{"m.txt": "m\n"})
	a.Git("checkout", "-q", "-b", "side")
	a.Commit("YT2 side", map[string]string{"s.txt": "s\n"})
	a.Git("checkout", "-q", "master")
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("a2\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "u.txt"), []byte("u\n"), 0o644)
	// b: YT1 is on master already
	b := repo("b")
	b.Commit("YT1 merged fix", map[string]string{"a.txt": "b1\n"})
	b.Commit("YT1 merged again", map[string]string{"a.txt": "b2\n"})
	// c: no base set: master is derived
	c := repo("c")
	c.Commit("YT1 in c", map[string]string{"c.txt": "c\n"})
	// d: two branches hold the same commit; z2 is checked out in a worktree, so it wins over z1 (which comes first by name)
	d := repo("d")
	d.Git("checkout", "-q", "-b", "z2")
	d.Commit("YT1 in d", map[string]string{"d.txt": "d\n"})
	d.Git("checkout", "-q", "-b", "z1", "z2")
	d.Git("checkout", "-q", "master")
	d.Git("worktree", "add", "-q", filepath.Join(root, "d-z2"), "z2")

	tag := aggTag{Name: "YT1", Cond: query.Conditions{Q: "YT1", Authors: []string{}}}
	picked := aggTag{Name: "picked", Cond: query.Conditions{Q: "YT1", Authors: []string{}, Branches: []string{"nope", "z1", "partial"}}}
	not := aggTag{Name: "not", Cond: query.Conditions{Q: "YT1", Authors: []string{}, Branches: []string{"partial"}, BranchNot: true}}
	two := aggTag{Name: "two", Cond: query.Conditions{Authors: []string{}, Branches: []string{"feat", "side"}}}
	empty := aggTag{Name: "none", Cond: query.Conditions{Q: "no such thing", Authors: []string{}}}
	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir()}
	s, err := Open(ctx, root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBases(map[string]string{"a": "master", "b": "master", "d": "master"}); err != nil {
		t.Fatal(err)
	}

	// Files first: git status also fills the uncommitted counts TagAgg reports.
	files, err := s.TagAggFiles(ctx, TagAggInput{Cond: &tag.Cond})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		x := f.Repo + ":" + f.Path + ":" + f.Status
		if f.Commits > 0 {
			x += ":" + string(rune('0'+f.Commits))
		}
		if f.Uncommitted {
			x += ":wc"
		}
		if f.Untracked {
			x += ":new"
		}
		got = append(got, x)
	}
	// a first (most commits, then name): gone.txt deleted by the newest commit, n.txt added then changed stays A,
	// a.txt also uncommitted, u.txt only untracked. b's a.txt by two commits; master's m.txt is not the Tag's.
	want := "a:a.txt:M:1:wc a:gone.txt:D:1 a:n.txt:A:2 a:u.txt:A:wc:new b:a.txt:M:2 c:c.txt:A:1 d:d.txt:A:1"
	if strings.Join(got, " ") != want {
		t.Errorf("files:\n got %s\nwant %s", strings.Join(got, " "), want)
	}

	res, err := s.TagAgg(ctx, TagAggInput{Cond: &tag.Cond})
	if err != nil {
		t.Fatal(err)
	}
	if res.Commits != 6 || res.Files != 3+1+1+1 || res.Uncommitted != 2 || res.Tag.Name != "" || res.ResultID == 0 {
		t.Errorf("totals: %+v", res)
	}
	rows, _ := s.Rows(res.ResultID, 0, 10)
	if len(rows) != 6 {
		t.Errorf("rows of the Tag's result: %d", len(rows))
	}

	// Refined on the filter page: other conditions, same Tag; any branch as the compare target.
	refined, err := s.TagAgg(ctx, TagAggInput{Cond: &query.Conditions{Q: "YT2", Authors: []string{}}, Base: "side"})
	if err != nil {
		t.Fatal(err)
	}
	if refined.Commits != 1 || len(refined.Repos) != 1 || refined.Repos[0].Repo != "a" || refined.Tag.Name != "" || refined.Tag.Cond.Q != "YT2" {
		t.Errorf("refined: %+v", refined)
	}
	if err := s.SetCompareTargets(" rel ", " side "); err != nil {
		t.Fatal(err)
	}
	if info := s.Info(); info.Release != "rel" || info.Master != "side" {
		t.Errorf("compare targets not saved: %q %q", info.Release, info.Master)
	}
	other, _ := s.TagAgg(ctx, TagAggInput{Cond: &tag.Cond, Base: "side"})
	for _, r := range other.Repos {
		if r.Repo == "b" && r.Skip != "no_base" {
			t.Errorf("b has no side: %+v", r)
		}
	}
	targets, _ := s.Options(query.Conditions{}, KeyTarget)
	tc := map[string]int{}
	for _, o := range targets {
		tc[o.Value] = o.Count
	}
	if tc["master"] != 4 || tc["side"] != 1 || tc["z1"] != 1 || tc["feat"] != 1 {
		t.Errorf("targets: %+v", targets)
	}
	by := map[string]AggRepo{}
	for _, r := range res.Repos {
		by[r.Repo] = r
	}
	if r := by["a"]; r.Branch != "feat" || r.BranchFrom != FromInferred || r.Ahead != 2 || r.Behind != 1 || r.Worktree != wt || r.Uncommitted != 2 || r.Files != 3 {
		t.Errorf("a: %+v", r)
	}
	if r := by["a"]; r.Auto != "feat" || r.Main != "master" {
		t.Errorf("a auto / main: %+v", r)
	}
	if r := by["b"]; r.Auto != "" {
		t.Errorf("b has no branch: %+v", r)
	}
	// candidates of a row: branches holding its Tag commits (beyond master), most first; merged rows fall back to all
	for _, tc := range []struct{ row, want string }{
		{"a", "feat:2:" + wt + " partial:1:"},
		{"b", "master:2:" + filepath.Join(root, "b")},
	} {
		bs, err := s.TagAggBranches(TagAggInput{Cond: &tag.Cond}, tc.row)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, b := range bs {
			got = append(got, fmt.Sprintf("%s:%d:%s", b.Name, b.Commits, b.Worktree))
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("branches of %s: %q, want %q", tc.row, strings.Join(got, " "), tc.want)
		}
	}
	// commits tab: per row, merges left out by default
	for _, tc := range []struct {
		row            string
		merges         bool
		total, skipped int
	}{{"", false, 6, 0}, {"a", false, 2, 0}, {"b", true, 2, 0}} {
		got, err := s.TagAggCommits(TagAggInput{Cond: &tag.Cond}, tc.row, tc.merges)
		if err != nil || got.Total != tc.total || got.Merges != tc.skipped || got.ResultID == 0 {
			t.Errorf("commits of %q: %+v %v", tc.row, got, err)
		}
	}
	over, _ := s.TagAgg(ctx, TagAggInput{Cond: &tag.Cond, Branches: map[string]string{"a": "partial"}})
	for _, r := range over.Repos {
		if r.Repo == "a" && (r.Branch != "partial" || r.Auto != "feat" || r.BranchFrom != FromOverride) {
			t.Errorf("override keeps the auto branch: %+v", r)
		}
	}
	if r := by["b"]; r.Branch != "" || r.BranchFrom != FromMerged || r.Skip != "" || r.Worktree != "" {
		t.Errorf("b: %+v", r)
	}
	if r := by["c"]; r.Base != "master" || r.BranchFrom != FromMerged || r.Skip != "" {
		t.Errorf("c: %+v", r)
	}
	if r := by["d"]; r.Branch != "z2" || r.Worktree != filepath.Join(root, "d-z2") {
		t.Errorf("d (tie goes to the checked-out branch): %+v", r)
	}

	// merged diff of a.txt: merge base to the worktree, committed and uncommitted in one
	p, err := s.TagAggPatch(ctx, TagAggInput{Cond: &tag.Cond}, "a", "a.txt", "", false, false, false)
	if err != nil || p.Mode != "range" || len(p.Parts) != 1 || !hasLine(p.Parts[0].Patch, "-", "a") || !hasLine(p.Parts[0].Patch, "+", "a2") {
		t.Errorf("a.txt: %+v %v", p, err)
	}
	if p, _ := s.TagAggPatch(ctx, TagAggInput{Cond: &tag.Cond}, "a", "u.txt", "", true, false, false); !hasLine(p.Parts[0].Patch, "+", "u") {
		t.Errorf("untracked u.txt: %+v", p)
	}
	// b has no branch diff: each Tag commit's diff, oldest first
	p, err = s.TagAggPatch(ctx, TagAggInput{Cond: &tag.Cond}, "b", "a.txt", "", false, false, false)
	if err != nil || p.Mode != "commits" || len(p.Parts) != 2 || p.Parts[0].Subject != "YT1 merged fix" || !hasLine(p.Parts[1].Patch, "+", "b2") {
		t.Errorf("b a.txt: %+v %v", p, err)
	}

	// overrides: a branch chosen in the view; a base for every repo
	o, _ := s.TagAgg(ctx, TagAggInput{Cond: &tag.Cond, Branches: map[string]string{"a": "partial", "d": "nope"}})
	for _, r := range o.Repos {
		switch r.Repo {
		case "a":
			if r.Branch != "partial" || r.BranchFrom != FromOverride || r.Ahead != 1 || r.Worktree != "" {
				t.Errorf("a overridden: %+v", r)
			}
		case "d":
			if r.Skip != SkipNoHead {
				t.Errorf("d with a missing branch: %+v", r)
			}
		}
	}
	one, _ := s.TagAgg(ctx, TagAggInput{Cond: &tag.Cond, Base: "master"})
	for _, r := range one.Repos {
		if r.Repo == "c" && (r.Skip != "" || r.BranchFrom != FromMerged) {
			t.Errorf("c with one base for all: %+v", r)
		}
	}

	// the Tag's branches: each repo takes the first one it has; excluded branches are not feature branches
	pr, _ := s.TagAgg(ctx, TagAggInput{Cond: &picked.Cond})
	for _, r := range pr.Repos {
		if want := map[string]string{"a": "partial", "d": "z1"}[r.Repo]; r.Branch != want || r.BranchFrom != FromTag {
			t.Errorf("%s from the Tag's branches: %+v", r.Repo, r)
		}
	}
	nr, _ := s.TagAgg(ctx, TagAggInput{Cond: &not.Cond})
	for _, r := range nr.Repos {
		if r.Repo == "a" && (r.Branch != "feat" || r.BranchFrom != FromInferred) {
			t.Errorf("a with partial excluded: %+v", r)
		}
	}

	// two of the Tag's branches in one repo: a row each, with that branch's commits, files and diff
	tr, _ := s.TagAgg(ctx, TagAggInput{Cond: &two.Cond})
	var rows2 []string
	for _, r := range tr.Repos {
		rows2 = append(rows2, fmt.Sprintf("%t:%s:%d:%t", r.Key == RowKey("a", r.Branch), r.Branch, r.Commits, r.Worktree != ""))
	}
	if got, want := strings.Join(rows2, " "), "true:feat:2:true true:side:1:false"; got != want {
		t.Errorf("rows of a with two branches:\n got %s\nwant %s", got, want)
	}
	tf, _ := s.TagAggFiles(ctx, TagAggInput{Cond: &two.Cond})
	var side []string
	for _, f := range tf {
		if f.Key == RowKey("a", "side") {
			side = append(side, f.Path)
		}
	}
	if strings.Join(side, " ") != "s.txt" {
		t.Errorf("files of the side row: %v", side)
	}
	if p, err := s.TagAggPatch(ctx, TagAggInput{Cond: &two.Cond}, RowKey("a", "side"), "s.txt", "", false, false, false); err != nil || p.Mode != "range" || !hasLine(p.Parts[0].Patch, "+", "s") {
		t.Errorf("side s.txt: %+v %v", p, err)
	}
	ov, _ := s.TagAgg(ctx, TagAggInput{Cond: &two.Cond, Branches: map[string]string{RowKey("a", "side"): "partial"}})
	if r := ov.Repos[1]; r.Key != RowKey("a", "side") || r.Branch != "partial" || r.BranchFrom != FromOverride || ov.Repos[0].Branch != "feat" {
		t.Errorf("override of one row: %+v", ov.Repos)
	}

	e1, _ := s.TagAgg(ctx, TagAggInput{Cond: &empty.Cond})
	e2, _ := s.TagAggFiles(ctx, TagAggInput{Cond: &empty.Cond})
	if e1.Commits != 0 || len(e1.Repos) != 0 || len(e2) != 0 {
		t.Errorf("empty filter: %+v, files %+v", e1, e2)
	}
	if _, err := s.TagAgg(ctx, TagAggInput{}); err == nil {
		t.Error("a view without conditions should be an error")
	}
}

func hasLine(p Patch, kind, text string) bool {
	for _, h := range p.Hunks {
		for _, l := range h.Lines {
			if l.Kind == kind && l.Text == text {
				return true
			}
		}
	}
	return false
}

func TestTagAggCommitsMerges(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	r.Git("branch", "-M", "master")
	r.Git("checkout", "-q", "-b", "side")
	r.Commit("YT9 side", map[string]string{"s.txt": "s\n"})
	r.Git("checkout", "-q", "master")
	r.Commit("YT9 main", map[string]string{"m.txt": "m\n"})
	r.Git("merge", "-q", "--no-ff", "-m", "YT9 merge side", "side")
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	in := TagAggInput{Cond: &query.Conditions{Q: "YT9"}}
	for _, tc := range []struct {
		merges         bool
		total, skipped int
	}{{false, 2, 1}, {true, 3, 0}} {
		got, err := s.TagAggCommits(in, "", tc.merges)
		if err != nil || got.Total != tc.total || got.Merges != tc.skipped {
			t.Errorf("merges=%v: %+v %v", tc.merges, got, err)
		}
	}
}

// A temporary filter (no id, only conditions: a branch compared from the log) and the commits its compare target has beyond the branch.
func TestTagAggTemporary(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	r.Git("branch", "-M", "master")
	r.Git("checkout", "-q", "-b", "feat")
	r.Commit("feat one", map[string]string{"f.txt": "f\n"})
	r.Git("checkout", "-q", "master")
	r.Commit("master one", map[string]string{"m1.txt": "m\n"})
	r.Commit("master two", map[string]string{"m2.txt": "m\n"})
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	cond := &query.Conditions{Authors: []string{}, Branches: []string{"feat"}}
	for name, c := range map[string]struct {
		in         TagAggInput
		ok         bool
		ahead, dwn int
	}{
		"temporary filter": {TagAggInput{Cond: cond}, true, 1, 2},
		"no cond":          {TagAggInput{}, false, 0, 0},
	} {
		res, err := s.TagAgg(ctx, c.in)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !c.ok {
			continue
		}
		if len(res.Repos) != 1 || res.Repos[0].Ahead != c.ahead || res.Repos[0].Behind != c.dwn || res.Tag.Name != "feat" {
			t.Errorf("%s: %+v", name, res)
		}
		down, err := s.TagAggDown(c.in, res.Repos[0].Key)
		if err != nil || down.Total != c.dwn {
			t.Errorf("%s down: %+v %v", name, down, err)
		}
	}
}

// A filter without a branch (e.g. only a date range) matches commits in the base and on other branches too. The file
// list must show only what the merged diff (merge-base → branch) can show, or a listed file opens as "no diff".
func TestTagAggFilesBeyondBase(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	r.Git("branch", "-M", "master")
	r.Commit("old doc", map[string]string{"doc/说明 文档.md": "x\n"})
	r.Git("checkout", "-q", "-b", "feat")
	r.Commit("feat one", map[string]string{"f.txt": "f\n"})
	r.Git("checkout", "-q", "-b", "other", "master")
	r.Commit("other branch", map[string]string{"o.txt": "o\n"}) // in neither the base nor feat
	r.Git("checkout", "-q", "master")
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBases(map[string]string{"r": "master"}); err != nil {
		t.Fatal(err)
	}
	in := TagAggInput{Branches: map[string]string{"r": "feat"}, Cond: &query.Conditions{Authors: []string{}}}
	res, err := s.TagAgg(ctx, in)
	if err != nil || len(res.Repos) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	files, err := s.TagAggFiles(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	if !slices.Equal(got, []string{"f.txt"}) || res.Repos[0].Files != 1 {
		t.Errorf("files %v, count %d; want only f.txt", got, res.Repos[0].Files)
	}
	for _, f := range files {
		p, err := s.TagAggPatch(ctx, in, res.Repos[0].Key, f.Path, "", false, false, false)
		if err != nil || len(p.Parts) != 1 || len(p.Parts[0].Patch.Hunks) == 0 {
			t.Errorf("%s: no diff: %+v %v", f.Path, p, err)
		}
	}
}

// The title bar (TagAggResult.Commits), the table (AggRepo.Commits) and the commits tab (TagAggCommits, merges hidden
// or not) count the same commits: on the row's branch and not in the base when the row compares, else every hit.
func TestTagAggCommitScope(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	r.Git("branch", "-M", "master")
	r.Commit("old doc", map[string]string{"d.txt": "x\n"})
	r.Git("checkout", "-q", "-b", "feat")
	r.Commit("feat one", map[string]string{"f.txt": "f\n"})
	r.Git("checkout", "-q", "-b", "side")
	r.Commit("side one", map[string]string{"s.txt": "s\n"})
	r.Git("checkout", "-q", "feat")
	r.Git("merge", "-q", "--no-ff", "-m", "merge side", "side")
	r.Git("checkout", "-q", "-b", "other", "master")
	r.Commit("other branch", map[string]string{"o.txt": "o\n"})
	r.Git("checkout", "-q", "master")
	r.Commit("master later", map[string]string{"m.txt": "m\n"})
	r.Git("checkout", "-q", "feat")
	r.Git("merge", "-q", "--no-ff", "-m", "merge master in", "master") // 合并主分支: its files (m.txt) are not the branch's
	r.Git("checkout", "-q", "master")
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	all := &query.Conditions{Authors: []string{}}
	for _, c := range []struct {
		name  string
		base  string
		in    TagAggInput
		want  int // every count
		files int
	}{
		{"base missing: every hit", "nope", TagAggInput{Cond: all}, 8, 6},
		{"derived base, inferred branch", "", TagAggInput{Cond: all}, 4, 2},
		{"branch picked: beyond the base", "master", TagAggInput{Cond: all, Branches: map[string]string{"r": "feat"}}, 4, 2},
		{"inferred branch: beyond the base", "master", TagAggInput{Cond: &query.Conditions{Q: "one", Authors: []string{}}}, 2, 2},
		{"branch condition", "master", TagAggInput{Cond: &query.Conditions{Authors: []string{}, Branches: []string{"other"}}}, 1, 1},
	} {
		if err := s.SetBases(map[string]string{"r": c.base}); err != nil {
			t.Fatal(err)
		}
		res, err := s.TagAgg(ctx, c.in)
		if err != nil || len(res.Repos) != 1 {
			t.Fatalf("%s: %+v %v", c.name, res, err)
		}
		row := res.Repos[0]
		if res.Commits != c.want || row.Commits != c.want || row.Files != c.files {
			t.Errorf("%s: title %d, row %d commits %d files; want %d, %d files", c.name, res.Commits, row.Commits, row.Files, c.want, c.files)
		}
		if files, err := s.TagAggFiles(ctx, c.in); err != nil || len(files) != c.files {
			t.Errorf("%s: file list %d %v; want %d", c.name, len(files), err, c.files)
		}
		for _, key := range []string{"", row.Key} {
			for _, merges := range []bool{false, true} {
				got, err := s.TagAggCommits(c.in, key, merges)
				if err != nil || got.Total+got.Merges != c.want {
					t.Errorf("%s: tab(row %q, merges %v) %+v %v; want %d", c.name, key, merges, got, err, c.want)
				}
			}
		}
	}
}

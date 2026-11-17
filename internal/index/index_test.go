package index

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"sheaf/internal/discover"
	"sheaf/internal/gitcli"
	"sheaf/internal/testutil"
)

type fixture struct {
	root string
	a    *testutil.Repo
	b    *testutil.Repo
}

// a: master with a merged feature branch, a worktree on feat-2, a tag.
// b: a small second repo.
func newFixture(t *testing.T) *fixture {
	testutil.Isolate(t)
	root := t.TempDir()
	a := testutil.NewRepo(t, filepath.Join(root, "a"))
	a.Commit("init", map[string]string{"a.txt": "1"})
	a.Git("checkout", "-q", "-b", "feat-1")
	a.Commit("YT03374 feature work", map[string]string{"src/f.go": "f"})
	a.Git("checkout", "-q", "master")
	a.Commit("master work", map[string]string{"a.txt": "2"}, testutil.Author{Name: "Li", Email: "LI@example.com"})
	a.Merge("feat-1", "Merge branch 'feat-1'")
	a.Git("tag", "-a", "v1", "-m", "v1")
	a.Git("worktree", "add", "-q", "-b", "feat-2", filepath.Join(root, "a-feat2"))
	b := testutil.NewRepo(t, filepath.Join(root, "b"))
	b.Commit("b init", map[string]string{"b.txt": "1"})
	return &fixture{root, a, b}
}

func scan(t *testing.T, root string) []discover.Repo {
	rs, err := discover.Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestBuildUpdateSnapshot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ix, err := Build(ctx, scan(t, f.root), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Repos) != 2 || ix.Commits() != 5 {
		t.Fatalf("want 2 repos / 5 commits, got %d / %d", len(ix.Repos), ix.Commits())
	}
	// newest first across repos
	for k := 1; k < len(ix.Order); k++ {
		p, c := ix.Order[k-1], ix.Order[k]
		if ix.Repos[p.Repo].Time[p.I] < ix.Repos[c.Repo].Time[c.I] {
			t.Fatal("Order not sorted by time")
		}
	}
	a := ix.Repos[0]
	master, ok := a.Ref("master")
	if !ok || len(a.Parents(master)) != 2 {
		t.Fatalf("master should be a merge: %v %v", master, ok)
	}
	if _, ok := a.Ref("refs/tags/v1"); !ok {
		t.Error("annotated tag missing from refs")
	}
	if got := strings.Join(refNames(a.RefsAt(master)), ","); got != "refs/heads/feat-2,refs/heads/master,refs/tags/v1" {
		t.Errorf("refs at master: %s", got)
	}
	// emails are lowercased
	found := false
	for _, s := range ix.Strs {
		found = found || s == "li@example.com"
	}
	if !found {
		t.Error("email not lowercased")
	}

	// incremental: one new commit in the worktree, one in b
	wt := filepath.Join(f.root, "a-feat2")
	os.WriteFile(filepath.Join(wt, "w.txt"), []byte("w"), 0o644)
	f.a.Git("-C", wt, "add", "-A")
	f.a.Git("-C", wt, "commit", "-q", "-m", "worktree commit")
	f.b.Commit("b second", map[string]string{"b.txt": "2"})
	ix2, d, err := Update(ctx, ix, scan(t, f.root), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Added != 2 || len(d.Changed) != 2 || ix2.Commits() != 7 {
		t.Fatalf("update: %+v commits=%d", d, ix2.Commits())
	}
	if ix.Commits() != 5 || ix.Repos[0].N() != 4 {
		t.Fatal("old index was mutated by Update")
	}
	// no change: repos are shared
	ix3, d3, _ := Update(ctx, ix2, scan(t, f.root), nil)
	if d3.Added != 0 || len(d3.Changed) != 0 || ix3.Repos[0] != ix2.Repos[0] {
		t.Fatalf("no-op update re-read repos: %+v", d3)
	}

	// force-push: feat-2 rewound, its commit stays indexed but is no longer live
	f.a.Git("-C", wt, "reset", "-q", "--hard", "HEAD~1")
	ix4, _, _ := Update(ctx, ix3, scan(t, f.root), nil)
	if ix4.Commits() != 6 {
		t.Fatalf("after rewind want 6 live commits, got %d", ix4.Commits())
	}

	path := filepath.Join(t.TempDir(), "idx", "index.bin")
	if err := ix4.Save(path); err != nil {
		t.Fatal(err)
	}
	ld, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if ld.Commits() != 6 || ld.Repos[0].Subject[0] != ix4.Repos[0].Subject[0] {
		t.Fatalf("snapshot round trip: %d", ld.Commits())
	}
}

func TestRangeAheadBehind(t *testing.T) {
	f := newFixture(t)
	f.a.Git("checkout", "-q", "-b", "feat-3", "v1")
	f.a.Commit("f3 one", nil)
	f.a.Commit("f3 two", nil)
	f.a.Git("checkout", "-q", "master")
	f.a.Commit("master after", nil)
	ix, err := Build(context.Background(), scan(t, f.root), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := ix.Repos[0]
	head, _ := a.Ref("feat-3")
	base, _ := a.Ref("master")
	if n := a.Range(head, base).b.count(); n != 2 {
		t.Errorf("feat-3 --not master: want 2, got %d", n)
	}
	ahead, behind := a.AheadBehind(base, head)
	g := strings.Fields(f.a.Git("rev-list", "--left-right", "--count", "feat-3...master"))
	if strconv.Itoa(ahead) != g[0] || strconv.Itoa(behind) != g[1] {
		t.Errorf("ahead/behind %d/%d, git says %v", ahead, behind, g)
	}
	if n := a.Range(head, -1).b.count(); n != 6 {
		t.Errorf("all of feat-3: want 6, got %d", n)
	}
	if a.Find(gitcli.Oid{}) != -1 {
		t.Error("Find zero oid")
	}
}

func refNames(rs []Ref) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestUpdateSome(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	repos := scan(t, f.root)
	ix, err := Build(ctx, repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.a.Commit("a moves", nil)
	f.b.Commit("b moves", nil)
	// Only b is named: a's new commit stays out until a is re-read.
	ix2, d, err := UpdateSome(ctx, ix, repos, []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Changed) != 1 || d.Changed[0] != "b" || ix2.Commits() != ix.Commits()+1 || ix2.Repos[0] != ix.Repos[0] {
		t.Fatalf("UpdateSome(b): changed %v, commits %d -> %d", d.Changed, ix.Commits(), ix2.Commits())
	}
	ix3, d, _ := UpdateSome(ctx, ix2, repos, []string{"a"})
	if len(d.Changed) != 1 || d.Changed[0] != "a" || ix3.Commits() != ix.Commits()+2 {
		t.Fatalf("UpdateSome(a): changed %v, commits %d", d.Changed, ix3.Commits())
	}
}

func TestUpdateDetectsReplacedRepo(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ix, err := Build(ctx, scan(t, f.root), nil)
	if err != nil {
		t.Fatal(err)
	}
	testutil.NewRepo(t, filepath.Join(f.root, "c")).Commit("c init", nil)
	repos := slices.DeleteFunc(scan(t, f.root), func(r discover.Repo) bool { return r.Name == "a" })
	updated, d, err := UpdateSome(ctx, ix, repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Changed, []string{"c", ""}) || len(updated.Repos) != len(ix.Repos) {
		t.Fatalf("changed = %q, repos = %d", d.Changed, len(updated.Repos))
	}
}

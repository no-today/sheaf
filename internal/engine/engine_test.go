package engine

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/index"
	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func build(t *testing.T) (*Engine, time.Time) {
	testutil.Isolate(t)
	root := t.TempDir()
	li := testutil.Author{Name: "Li", Email: "li@example.com"}
	a := testutil.NewRepo(t, filepath.Join(root, "yt-med"))
	a.Commit("init", map[string]string{"README.md": "1"})
	a.Git("checkout", "-q", "-b", "feat-YT03374")
	a.Commit("YT03374 处方审核增加超量用药校验", map[string]string{"src/audit/Audit.java": "a"})
	a.Commit("YT03374 联调测试数据，勿合并", map[string]string{"src/audit/Test.java": "t"})
	a.Git("checkout", "-q", "master")
	a.Commit("YT3374 不规范写法", map[string]string{"src/x.java": "x"}, li)
	a.Git("update-ref", "refs/remotes/origin/master", "master")
	b := testutil.NewRepo(t, filepath.Join(root, "hs-ytd"))
	b.Commit("YT03374 医生站展示超量提示", map[string]string{"web/Dialog.vue": "v"})
	b.Commit("fix 挂号", map[string]string{"web/reg.ts": "r"}, li)

	repos, err := discover.Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Build(context.Background(), repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	// fixture commits are dated from 1_700_000_000; "now" is a day after the last
	return New(ix), time.Unix(1_700_000_000+20*60, 0).Add(24 * time.Hour)
}

func TestRun(t *testing.T) {
	e, now := build(t)
	env := query.Env{Me: []string{"me@example.com"}, Now: now}
	run := func(c query.Conditions, bases Bases) *Result {
		m, err := query.Compile(c, env)
		if err != nil {
			t.Fatal(err)
		}
		return e.Run(m, bases)
	}
	subjects := func(r *Result) []string {
		var s []string
		for _, c := range r.Rows {
			s = append(s, e.ix.Repos[c.Repo].Subject[c.I])
		}
		return s
	}

	if got := run(query.Conditions{}, nil); len(got.Rows) != 6 {
		t.Fatalf("all: %v", subjects(got))
	}
	got := run(query.Conditions{Q: "YT03374 NOT 测试"}, nil)
	if len(got.Rows) != 2 || got.Rows[0].Repo == got.Rows[1].Repo {
		t.Errorf("YT03374 NOT 测试: %v", subjects(got))
	}
	// newest first across repos
	all := run(query.Conditions{}, nil)
	for k := 1; k < len(all.Rows); k++ {
		p, c := all.Rows[k-1], all.Rows[k]
		if e.ix.Repos[p.Repo].Time[p.I] < e.ix.Repos[c.Repo].Time[c.I] {
			t.Errorf("not newest first: %v", subjects(all))
		}
	}
	if n := len(run(query.Conditions{Me: true}, nil).Rows); n != 4 {
		t.Errorf("me: %d", n)
	}
	if n := len(run(query.Conditions{Authors: []string{"li@example.com"}}, nil).Rows); n != 2 {
		t.Errorf("li: %d", n)
	}
	// whole repo entries keep per-repo counts without them; ByRepo still applies dir and text entries
	r := run(query.Conditions{Q: "YT03374", Paths: []string{"hs-ytd"}}, nil)
	med, _ := e.ix.RepoByName("yt-med")
	ytd, _ := e.ix.RepoByName("hs-ytd")
	if len(r.Rows) != 1 || r.ByRepo[med] != 2 || r.ByRepo[ytd] != 1 {
		t.Errorf("repo filter: rows=%d byRepo=%v", len(r.Rows), r.ByRepo)
	}
	for _, tc := range []struct {
		name   string
		paths  []string
		want   []string // subjects, any order
		byRepo [2]int   // yt-med, hs-ytd
	}{
		{"text, any case", []string{"AUDIT/"}, []string{"YT03374 联调测试数据，勿合并", "YT03374 处方审核增加超量用药校验"}, [2]int{2, 0}},
		{"text anywhere in the path", []string{"udit/Te"}, []string{"YT03374 联调测试数据，勿合并"}, [2]int{1, 0}},
		{"dir prefix", []string{"yt-med/src"}, []string{"YT3374 不规范写法", "YT03374 联调测试数据，勿合并", "YT03374 处方审核增加超量用药校验"}, [2]int{3, 0}},
		{"dir is a prefix, not text", []string{"yt-med/audit"}, nil, [2]int{0, 0}},
		{"dir as a whole segment", []string{"yt-med/src/aud"}, nil, [2]int{0, 0}},
		{"a file", []string{"yt-med/src/x.java/"}, []string{"YT3374 不规范写法"}, [2]int{1, 0}},
		{"dir only in its repo", []string{"hs-ytd/src"}, nil, [2]int{0, 0}},
		{"text matching a repo name is not the repo", []string{"hs-yt"}, nil, [2]int{0, 0}},
		{"repo or dir", []string{"hs-ytd", "yt-med/src/audit"}, []string{"fix 挂号", "YT03374 医生站展示超量提示", "YT03374 联调测试数据，勿合并", "YT03374 处方审核增加超量用药校验"}, [2]int{2, 0}},
		{"repo or text", []string{"yt-med", "web/reg"}, []string{"fix 挂号", "YT3374 不规范写法", "YT03374 联调测试数据，勿合并", "YT03374 处方审核增加超量用药校验", "init"}, [2]int{0, 1}},
		{"two repos", []string{"yt-med", "hs-ytd"}, nil, [2]int{4, 2}},
		{"blank", []string{" "}, nil, [2]int{4, 2}},
	} {
		r := run(query.Conditions{Paths: tc.paths}, nil)
		s := subjects(r)
		slices.Sort(s)
		slices.Sort(tc.want)
		if tc.want != nil && !slices.Equal(s, tc.want) || tc.want == nil && tc.byRepo == [2]int{0, 0} && len(s) > 0 {
			t.Errorf("%s: %v", tc.name, s)
		}
		if got := [2]int{r.ByRepo[med], r.ByRepo[ytd]}; got != tc.byRepo {
			t.Errorf("%s: byRepo %v", tc.name, got)
		}
		m, _ := query.Compile(query.Conditions{Paths: tc.paths}, env)
		if n := e.Count(m, nil); n != len(r.Rows) {
			t.Errorf("%s: count %d", tc.name, n)
		}
	}
	if n := len(run(query.Conditions{Paths: []string{"yt-med", "hs-ytd"}}, nil).Rows); n != 6 {
		t.Errorf("two repos: %d", n)
	}
	// branch: without a base it is everything reachable; with base master only the feature commits
	if n := len(run(query.Conditions{Branches: []string{"feat-YT03374"}}, nil).Rows); n != 3 {
		t.Errorf("branch without base: %d", n)
	}
	if n := len(run(query.Conditions{Branches: []string{"feat-YT03374"}}, Bases{"yt-med": "master"}).Rows); n != 2 {
		t.Errorf("branch with base: %d", n)
	}
	if n := len(run(query.Conditions{Branches: []string{"feat-YT03374"}}, Bases{"yt-med": "origin/master"}).Rows); n != 2 {
		t.Errorf("branch with remote base: %d", n)
	}
	// the base itself, or its local twin of a remote base, is its whole history
	for _, base := range []string{"master", "origin/master"} {
		if n := len(run(query.Conditions{Paths: []string{"yt-med"}, Branches: []string{"master"}}, Bases{"yt-med": base}).Rows); n != 2 {
			t.Errorf("branch master with base %s: %d", base, n)
		}
	}
	// several branches: the union of each one's own commits; the base itself is its whole history
	if n := len(run(query.Conditions{Paths: []string{"yt-med"}, Branches: []string{"feat-YT03374", "master"}}, Bases{"yt-med": "master"}).Rows); n != 4 {
		t.Errorf("feat + master: %d", n)
	}
	// a name only one repo has filters that repo only; the other one has none of the branches
	if n := len(run(query.Conditions{Branches: []string{"feat-YT03374", "nope"}}, Bases{"yt-med": "master"}).Rows); n != 2 {
		t.Errorf("feat + unknown: %d", n)
	}
	// excluded branches: everything else, also in repos without them
	if n := len(run(query.Conditions{Branches: []string{"feat-YT03374"}, BranchNot: true}, Bases{"yt-med": "master"}).Rows); n != 4 {
		t.Errorf("not feat: %d", n)
	}
	// excluding the base leaves what is not merged into it
	got = run(query.Conditions{Branches: []string{"master"}, BranchNot: true}, Bases{"yt-med": "master"})
	if s := subjects(got); len(s) != 2 || s[0] != "YT03374 联调测试数据，勿合并" || s[1] != "YT03374 处方审核增加超量用药校验" {
		t.Errorf("not master: %v", s)
	}
	// excluded authors
	if n := len(run(query.Conditions{Authors: []string{"li@example.com"}, AuthorNot: true}, nil).Rows); n != 4 {
		t.Errorf("not li: %d", n)
	}
	if n := len(run(query.Conditions{Me: true, AuthorNot: true}, nil).Rows); n != 2 {
		t.Errorf("not me: %d", n)
	}
	// AuthorNot alone is no filter
	if n := len(run(query.Conditions{AuthorNot: true}, nil).Rows); n != 6 {
		t.Errorf("not nobody: %d", n)
	}
	// hours: everything is older than 7 days before a far-future now
	m, _ := query.Compile(query.Conditions{Hours: 168}, query.Env{Now: now.Add(30 * 24 * time.Hour)})
	if n := e.Count(m, nil); n != 0 {
		t.Errorf("hours: %d", n)
	}
	// a range cuts both ends of the newest-first list, ends included
	times := []int64{}
	for _, c := range all.Rows {
		times = append(times, e.ix.Repos[c.Repo].Time[c.I])
	}
	inside := func(lo, hi int64) int {
		n := 0
		for _, x := range times {
			if x >= lo && (hi == 0 || x <= hi) {
				n++
			}
		}
		return n
	}
	if inside(times[4], times[1]) == 6 || inside(times[2], 0) == 6 {
		t.Fatalf("fixture times %v cut nothing", times)
	}
	for _, tc := range []struct {
		since, until int64
		want         int
	}{
		{times[4], times[1], inside(times[4], times[1])},
		{times[2], 0, inside(times[2], 0)},
		{times[2], times[2], inside(times[2], times[2])},
		{0, times[2], inside(0, times[2])},
		{times[0] + 1, 0, 0},
		{0, times[5] - 1, 0},
		{times[1], times[4], 0}, // reversed
	} {
		m, _ := query.Compile(query.Conditions{Since: tc.since, Until: tc.until}, env)
		if n := len(e.Run(m, nil).Rows); n != tc.want {
			t.Errorf("range %d..%d: %d, want %d", tc.since, tc.until, n, tc.want)
		}
		if n := e.Count(m, nil); n != tc.want {
			t.Errorf("count range %d..%d: %d", tc.since, tc.until, n)
		}
	}
	m, _ = query.Compile(query.Conditions{Q: "超量"}, env)
	if !e.Matcher(m, nil).Has(r.Rows[0]) {
		t.Error("Matches.Has")
	}
}

func TestUTF16Range(t *testing.T) {
	s := "a😀超量b"
	start := len("a😀")
	a, b := UTF16Range(s, start, start+len("超量"))
	if a != 3 || b != 5 {
		t.Errorf("got %d,%d", a, b)
	}
}

func TestBranchKeepsCommitsAfterMerge(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("base", map[string]string{"a": "1"})
	r.Git("checkout", "-q", "-b", "merged")
	r.Commit("merged work", map[string]string{"b": "1"})
	r.Git("checkout", "-q", "master")
	r.Git("branch", "ff")
	r.Git("branch", "pending")
	r.Merge("merged", "land merged")
	r.Commit("after", map[string]string{"c": "1"})
	r.Git("checkout", "-q", "pending")
	r.Commit("pending work", map[string]string{"d": "1"})
	r.Git("checkout", "-q", "master")
	repos, err := discover.Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Build(context.Background(), repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := New(ix)
	for _, tc := range []struct {
		branches []string
		want     []string
	}{
		{[]string{"merged"}, []string{"merged work"}},
		{[]string{"pending"}, []string{"pending work"}},
		{[]string{"ff"}, nil},
		{[]string{"merged", "pending"}, []string{"merged work", "pending work"}},
	} {
		m, serr := query.Compile(query.Conditions{Branches: tc.branches}, query.Env{})
		if serr != nil {
			t.Fatal(serr)
		}
		var got []string
		for _, c := range e.Run(m, Bases{"repo": "master"}).Rows {
			got = append(got, ix.Repos[c.Repo].Subject[c.I])
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%v: %v, want %v", tc.branches, got, tc.want)
		}
	}
}

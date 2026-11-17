package engine

import (
	"context"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/index"
	"sheaf/internal/query"
)

// SHEAF_BENCH_ROOT=/path/to/large-workspace go test -run Real -v ./internal/engine
func TestRealQueries(t *testing.T) {
	root := os.Getenv("SHEAF_BENCH_ROOT")
	if root == "" {
		t.Skip("SHEAF_BENCH_ROOT not set")
	}
	ctx := context.Background()
	repos, _ := discover.Scan(ctx, root)
	ix, err := index.Build(ctx, repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	e := New(ix)
	tNew := time.Since(t0)
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("commits=%d engine.New=%v heapInuse(index+engine)=%.0fMB", ix.Commits(), tNew.Round(time.Millisecond), float64(ms.HeapInuse)/1e6)

	env := query.Env{Me: []string{discover.UserEmail(ctx)}, Now: time.Now()}
	// The busiest top-level directory of the first repo: a directory that exists, with enough commits to time.
	repoDir, most := "", 0
	for _, d := range topDirs(ix, ix.Repos[0]) {
		p := ix.Repos[0].Name + "/" + d
		if m, err := query.Compile(query.Conditions{Paths: []string{p}}, env); err == nil {
			if n := e.Count(m, Bases{}); n > most {
				repoDir, most = p, n
			}
		}
	}
	if repoDir == "" {
		t.Fatalf("%s: no directory with commits in the index", ix.Repos[0].Name)
	}
	cases := []struct {
		name string
		c    query.Conditions
	}{
		{"me, 30d", query.Conditions{Me: true, Hours: 720}},
		{"all", query.Conditions{}},
		{"term", query.Conditions{Q: "YT03374"}},
		{"OR", query.Conditions{Q: "YT03374 | YT3374 | YT-03374"}},
		{"AND NOT", query.Conditions{Q: "审核 NOT 测试"}},
		{"regex", query.Conditions{Q: "YT0?3374", Regex: true}},
		{"path", query.Conditions{Paths: []string{"audit/"}}},
		{"repo", query.Conditions{Paths: []string{repos[0].Name}}},
		{"repo/dir", query.Conditions{Paths: []string{repoDir}}},
		{"3 repos", query.Conditions{Paths: []string{repos[0].Name, repos[1].Name, repos[2].Name}}},
		{"7d range", query.Conditions{Since: time.Now().Add(-14 * 24 * time.Hour).Unix(), Until: time.Now().Add(-7 * 24 * time.Hour).Unix()}},
		{"branch", query.Conditions{Branches: []string{"feat-2610-YT03374"}}},
		{"3 branches", query.Conditions{Branches: []string{"feat-2610-YT03796", "feat-2610-YT03796-pattype5", "feat-YT03796-filters"}}},
		{"not master", query.Conditions{Branches: []string{"master"}, BranchNot: true}},
		{"not me", query.Conditions{Me: true, AuthorNot: true}},
		{"term+me+path", query.Conditions{Q: "fix", Me: true, Paths: []string{"src/"}}},
	}
	for _, c := range cases {
		m, serr := query.Compile(c.c, env)
		if serr != nil {
			t.Fatal(serr)
		}
		var d []time.Duration
		n := 0
		for k := 0; k < 15; k++ {
			s := time.Now()
			n = len(e.Run(m, Bases{}).Rows)
			d = append(d, time.Since(s))
		}
		sort.Slice(d, func(a, b int) bool { return d[a] < d[b] })
		t.Logf("%-18s hits=%-7d median=%-8v max=%v", c.name, n, d[len(d)/2].Round(100*time.Microsecond), d[len(d)-1].Round(100*time.Microsecond))
		if c.name == "repo/dir" && n == 0 {
			t.Errorf("repo/dir %s: no hits", repoDir)
		}
	}
}

func topDirs(ix *index.Index, r *index.Repo) []string {
	seen := map[uint32]bool{}
	dirs := map[string]bool{}
	for _, f := range r.File {
		if seen[f] {
			continue
		}
		seen[f] = true
		if d, _, ok := strings.Cut(ix.Paths[f], "/"); ok && d != "" {
			dirs[d] = true
		}
	}
	out := make([]string, 0, len(dirs))
	for d := range dirs {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

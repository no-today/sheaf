package index

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/gitcli"
)

// SHEAF_BENCH_ROOT=/path/to/large-workspace go test -run Real -v ./internal/index
func TestRealWorkspace(t *testing.T) {
	root := os.Getenv("SHEAF_BENCH_ROOT")
	if root == "" {
		t.Skip("SHEAF_BENCH_ROOT not set")
	}
	ctx := context.Background()
	// The app runs every git command under one ceiling (session.gitParallel); SHEAF_GIT_CAP=0 measures without it.
	gitCap := runtime.NumCPU() + 4
	if v := os.Getenv("SHEAF_GIT_CAP"); v != "" {
		gitCap, _ = strconv.Atoi(v)
	}
	if gitCap > 0 {
		ctx = gitcli.WithLimit(ctx, gitcli.NewLimit(gitCap))
	}
	t0 := time.Now()
	repos, err := discover.Scan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	tScan := time.Since(t0)
	t1 := time.Now()
	ix, err := Build(ctx, repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	tBuild := time.Since(t1)
	path := filepath.Join(t.TempDir(), "index.bin")
	t2 := time.Now()
	ix.Save(path)
	tSave := time.Since(t2)
	st, _ := os.Stat(path)
	t3 := time.Now()
	_, d, _ := Update(ctx, ix, repos, nil)
	tNoop := time.Since(t3)
	ix = nil
	runtime.GC()
	t4 := time.Now()
	ld, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	tLoad := time.Since(t4)
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	wt := 0
	for _, r := range repos {
		wt += len(r.Worktrees) - 1
	}
	t.Logf("repos=%d worktrees=%d commits=%d paths=%d", len(repos), wt, ld.Commits(), len(ld.Paths))
	t.Logf("scan=%v build=%v save=%v (%.1fMB) noop-update=%v (changed=%d) load=%v heapInuse=%.0fMB",
		tScan.Round(time.Millisecond), tBuild.Round(time.Millisecond), tSave.Round(time.Millisecond), float64(st.Size())/1e6,
		tNoop.Round(time.Millisecond), len(d.Changed), tLoad.Round(time.Millisecond), float64(ms.HeapInuse)/1e6)
	// Info() cost: ahead/behind of every worktree HEAD against master / main (uncached).
	t5 := time.Now()
	n := 0
	for k, dr := range repos {
		r := ld.Repos[k]
		base, ok := r.Ref("master")
		if !ok {
			base, ok = r.Ref("main")
		}
		for _, w := range dr.Worktrees {
			if h := r.Find(w.Head); ok && h >= 0 {
				r.AheadBehind(base, h)
				n++
			}
		}
	}
	t.Logf("ahead/behind for %d worktrees (cold, incl. oid maps)=%v", n, time.Since(t5).Round(time.Millisecond))
	runtime.KeepAlive(ld)
}

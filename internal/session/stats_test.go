package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

// The line counts of a row follow its merged diff: the branch with what is not committed yet, read again when the working
// directory changes; once merged, the sum of the row's own commits.
func TestTagAggStats(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "1\n2\n3\n", "b.txt": "b\n"})
	r.Git("branch", "-M", "master")
	r.Git("checkout", "-q", "-b", "feat-YT7")
	r.Commit("YT7 one", map[string]string{"a.txt": "1\ntwo\n3\nfour\n"})
	r.Commit("YT7 two", map[string]string{"b.txt": "b\nc\n"})

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
	in := TagAggInput{Cond: &query.Conditions{Q: "YT7", Authors: []string{}}}
	stats := func() map[string]FileStat {
		t.Helper()
		l, err := s.TagAggStats(ctx, in, "r")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]FileStat{}
		for _, f := range l {
			out[f.Path] = f
		}
		return out
	}
	if got := stats(); got["a.txt"] != (FileStat{Path: "a.txt", Add: 2, Del: 1}) || got["b.txt"] != (FileStat{Path: "b.txt", Add: 1}) {
		t.Fatalf("branch counts %+v", got)
	}

	if err := os.WriteFile(filepath.Join(r.Dir, "b.txt"), []byte("b\nc\nd\ne\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := stats(); got["b.txt"].Add != 1 {
		t.Fatalf("counts read again before the working directory was seen to change: %+v", got["b.txt"])
	}
	s.RefreshDirty(ctx, []string{r.Dir}, true)
	if got := stats(); got["b.txt"].Add != 3 {
		t.Fatalf("uncommitted lines not counted: %+v", got["b.txt"])
	}

	r.Git("checkout", "-q", "--", "b.txt")
	r.Git("checkout", "-q", "master")
	r.Git("merge", "-q", "--ff-only", "feat-YT7")
	r.Git("branch", "-q", "-D", "feat-YT7")
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stats(); got["a.txt"] != (FileStat{Path: "a.txt", Add: 2, Del: 1}) || got["b.txt"] != (FileStat{Path: "b.txt", Add: 1}) {
		t.Fatalf("merged row counts %+v", got)
	}
}

func TestStatsCacheRetriesFailedReadsAndKeepsEmptyResults(t *testing.T) {
	var cache statsCache
	key := statsKey{dir: "repo", from: "base", to: "branch"}
	failure := errors.New("numstat unavailable")
	calls := 0
	read := func() ([]FileStat, error) {
		calls++
		if calls == 1 {
			return nil, failure
		}
		return []FileStat{}, nil
	}
	if _, err := cache.load(context.Background(), key, read); !errors.Is(err, failure) {
		t.Fatalf("failed read: %v", err)
	}
	for range 2 {
		if stats, err := cache.load(context.Background(), key, read); err != nil || len(stats) != 0 {
			t.Fatalf("empty result: %+v, %v", stats, err)
		}
	}
	if calls != 2 {
		t.Fatalf("numstat read %d times, want one failure and one successful empty result", calls)
	}
}

func TestStatsCacheWaiterCanCancelWithoutStoppingTheRead(t *testing.T) {
	var cache statsCache
	key := statsKey{dir: "repo", from: "base", to: "branch"}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Errorf("shared read: %v", err)
		}
	}()
	go func() {
		_, err := cache.load(context.Background(), key, func() ([]FileStat, error) {
			close(started)
			<-release
			return []FileStat{{Path: "file.txt", Add: 1}}, nil
		})
		done <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	unexpected := func() ([]FileStat, error) {
		t.Error("a waiter started a second numstat read")
		return nil, nil
	}
	if _, err := cache.load(ctx, key, unexpected); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter: %v", err)
	}
}

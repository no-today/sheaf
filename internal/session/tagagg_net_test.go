package session

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"sheaf/internal/index"
	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestCommittedFilesDropsOnlyAddedThenDeletedPaths(t *testing.T) {
	for _, tc := range []struct {
		history, status string
	}{
		{"", ""},
		{"A", "A"},
		{"AM", "A"},
		{"AMD", ""},
		{"AD", ""},
		{"ADAD", ""},
		{"ADA", "A"},
		{"ADAM", "A"},
		{"M", "M"},
		{"MM", "M"},
		{"MD", "D"},
		{"MAD", "D"},
		{"D", "D"},
		{"DA", "A"},
		{"DAD", "D"},
		{"R", "R"},
		{"CT", "T"},
	} {
		t.Run(tc.history, func(t *testing.T) {
			ix := &index.Index{Paths: []string{"file.txt", "kept.txt"}}
			r := &index.Repo{Name: "repo", FileOff: []uint32{0}}
			var commits []int32
			for i, st := range []byte(tc.history) {
				r.File = append(r.File, 0)
				r.Stat = append(r.Stat, st)
				r.FileOff = append(r.FileOff, uint32(len(r.File)))
				commits = append(commits, int32(i))
			}
			r.File = append(r.File, 1)
			r.Stat = append(r.Stat, 'M')
			r.FileOff = append(r.FileOff, uint32(len(r.File)))
			commits = append(commits, int32(len(tc.history)))
			slices.Reverse(commits)
			want := []AggFile{{Repo: "repo", Key: "row", Path: "kept.txt", Status: "M", Commits: 1}}
			if tc.status != "" {
				want = append([]AggFile{{Repo: "repo", Key: "row", Path: "file.txt", Status: tc.status, Commits: len(tc.history)}}, want...)
			}
			if got := committedFiles(ix, r, "row", commits, true); !slices.Equal(got, want) {
				t.Fatalf("history %q: got %+v, want %+v", tc.history, got, want)
			}
		})
	}
}

func netFilesFixture(t *testing.T, mode string) (*Session, *testutil.Repo, TagAggInput) {
	t.Helper()
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"b.txt": "before\n", "c.txt": "before\n"})
	r.Git("checkout", "-q", "-b", "feat-YT7")
	r.Commit("YT7 add and edit", map[string]string{"a.txt": "temporary\n", "b.txt": "changed\n", "c.txt": "changed\n"})
	r.Commit("YT7 undo a and b", map[string]string{"a.txt": "", "b.txt": "before\n"})
	if mode != "worktree" {
		r.Git("checkout", "-q", "master")
	}
	if mode == "merged" {
		r.Merge("feat-YT7", "land feature")
		r.Git("branch", "-q", "-D", "feat-YT7")
	}
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	s.bg.Wait()
	if err := s.SetBases(map[string]string{"repo": "master"}); err != nil {
		t.Fatal(err)
	}
	return s, r, TagAggInput{Cond: &query.Conditions{Q: "YT7", Authors: []string{}}}
}

func aggFilePaths(files []AggFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

func TestTagAggFilesNetChanges(t *testing.T) {
	for _, mode := range []string{"worktree", "branch", "merged"} {
		t.Run(mode, func(t *testing.T) {
			s, _, in := netFilesFixture(t, mode)
			ctx := context.Background()
			files, err := s.TagAggFiles(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"c.txt"}
			if mode == "merged" {
				want = []string{"b.txt", "c.txt"}
			}
			if got := aggFilePaths(files); !slices.Equal(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			if mode == "merged" {
				return
			}
			before := s.rec.LastID()
			stats, err := s.TagAggStats(ctx, in, "repo")
			if err != nil || len(stats) != 1 || stats[0] != (FileStat{Path: "c.txt", Add: 1, Del: 1}) {
				t.Fatalf("net counts: %+v, %v", stats, err)
			}
			if s.rec.LastID() != before {
				t.Fatal("the file list did not share its numstat cache with line counts")
			}
		})
	}
}

func TestTagAggFilesEmptyNetDiff(t *testing.T) {
	s, r, in := netFilesFixture(t, "worktree")
	if err := os.WriteFile(filepath.Join(r.Dir, "c.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := s.TagAggFiles(context.Background(), in)
	if err != nil || len(files) != 0 {
		t.Fatalf("expected no net changes: %+v, %v", files, err)
	}
	before := s.rec.LastID()
	stats, err := s.TagAggStats(context.Background(), in, "repo")
	if err != nil || len(stats) != 0 || s.rec.LastID() != before {
		t.Fatalf("empty net diff was not cached: %+v, %v", stats, err)
	}
}

func TestTagAggFilesNetWorktree(t *testing.T) {
	s, r, in := netFilesFixture(t, "worktree")
	for path, text := range map[string]string{"a.txt": "untracked again\n", "c.txt": "before\n", "empty.txt": "", "staged.txt": "staged\n"} {
		if err := os.WriteFile(filepath.Join(r.Dir, path), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r.Git("add", "staged.txt")
	files, err := s.TagAggFiles(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := aggFilePaths(files), []string{"a.txt", "empty.txt", "staged.txt"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, f := range files {
		if !f.Uncommitted || f.Untracked != (f.Path != "staged.txt") {
			t.Errorf("worktree flags: %+v", f)
		}
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "c.txt"), []byte("different again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.RefreshDirty(context.Background(), []string{r.Dir}, true)
	files, err = s.TagAggFiles(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := aggFilePaths(files), []string{"a.txt", "c.txt", "empty.txt", "staged.txt"}; !slices.Equal(got, want) {
		t.Fatalf("after invalidation: got %v, want %v", got, want)
	}
}

func TestTagAggFilesKeepsBinaryAndModeChanges(t *testing.T) {
	testutil.Isolate(t)
	root := filepath.Join(t.TempDir(), "ws")
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"binary.dat": "\x00before\n", "script.sh": "echo test\n"})
	r.Git("checkout", "-q", "-b", "feat-YT7")
	r.Git("config", "core.filemode", "true")
	if err := os.Chmod(filepath.Join(r.Dir, "script.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.Commit("YT7 binary and mode", map[string]string{"binary.dat": "\x00after\n"})
	s, err := Open(context.Background(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	in := TagAggInput{Cond: &query.Conditions{Q: "YT7", Authors: []string{}}}
	files, err := s.TagAggFiles(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := aggFilePaths(files), []string{"binary.dat", "script.sh"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	stats, err := s.TagAggStats(context.Background(), in, "repo")
	if err != nil || len(stats) != 2 || !stats[0].Binary || stats[1] != (FileStat{Path: "script.sh"}) {
		t.Fatalf("non-text counts: %+v, %v", stats, err)
	}
}

func TestTagAggFilesKeepsFilesWhenStatsFail(t *testing.T) {
	s, r, in := netFilesFixture(t, "branch")
	if err := os.Rename(r.Dir, r.Dir+"-offline"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TagAggStats(context.Background(), in, "repo"); err == nil {
		t.Fatal("expected numstat to fail for an unavailable repository")
	}
	files, err := s.TagAggFiles(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := aggFilePaths(files), []string{"b.txt", "c.txt"}; !slices.Equal(got, want) {
		t.Fatalf("failed stats hid files: got %v, want %v", got, want)
	}
}

func TestTagAggFilesAndStatsShareConcurrentRead(t *testing.T) {
	s, _, in := netFilesFixture(t, "branch")
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if i == 0 {
				if _, err := s.TagAggFiles(context.Background(), in); err != nil {
					t.Error(err)
				}
			} else if _, err := s.TagAggStats(context.Background(), in, "repo"); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	n := 0
	for _, e := range s.rec.Events() {
		if slices.Contains(e.Args, "--numstat") && strings.Contains(strings.Join(e.Args, " "), "...") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("concurrent file list and line counts ran numstat %d times, want 1", n)
	}
}

func TestPrefetchedRequirementOpensWithoutGit(t *testing.T) {
	s, r, in := netFilesFixture(t, "worktree")
	if err := os.WriteFile(filepath.Join(r.Dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in.DiffMode = DiffBase
	ctx := context.Background()
	s.RefreshDirty(ctx, []string{r.Dir}, true) // what the watch does when the file appears
	s.PrefetchTagAgg(ctx, []TagAggInput{in})
	before := len(s.rec.Events())
	files, err := s.TagAggFiles(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TagAggStats(ctx, in, "repo"); err != nil {
		t.Fatal(err)
	}
	if ran := s.rec.Events()[before:]; len(ran) > 0 {
		t.Fatalf("opening a prefetched requirement ran git %d times: %v", len(ran), ran[0].Args)
	}
	if got, want := aggFilePaths(files), []string{"c.txt", "new.txt"}; !slices.Equal(got, want) {
		t.Fatalf("files: got %v, want %v", got, want)
	}
}

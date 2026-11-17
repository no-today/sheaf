package watch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

func TestClassify(t *testing.T) {
	repos := []Repo{
		{Name: "app", GitDir: "/ws/app/.git", Worktrees: []string{"/ws/app", "/ws/app-feat"}},
		{Name: "app/sub", GitDir: "/ws/app/.git/modules/sub", Worktrees: []string{"/ws/app/sub"}},
		{Name: "lib", GitDir: "/ws/lib/.git", Worktrees: []string{"/ws/lib"}},
	}
	for _, tc := range []struct {
		name string
		dirs []string
		want Changes
	}{
		{"HEAD / index / packed-refs", []string{"/ws/lib/.git"}, Changes{Repos: []string{"lib"}, Worktrees: []string{"/ws/lib"}}},
		{"refs", []string{"/ws/lib/.git/refs/heads/feature"}, Changes{Repos: []string{"lib"}}},
		{"linked worktree HEAD", []string{"/ws/app/.git/worktrees/app-feat"}, Changes{Repos: []string{"app"}, Worktrees: []string{"/ws/app", "/ws/app-feat"}}},
		{"objects only", []string{"/ws/app/.git/objects/ab", "/ws/app/.git/logs/refs"}, Changes{}},
		{"worktree file", []string{"/ws/app-feat/src/x"}, Changes{Worktrees: []string{"/ws/app-feat"}}},
		{"worktree root covers its .git", []string{"/ws/lib"}, Changes{Repos: []string{"lib"}, Worktrees: []string{"/ws/lib"}}},
		{"submodule work dir, deepest wins", []string{"/ws/app/sub/src"}, Changes{Worktrees: []string{"/ws/app/sub"}}},
		{"git dir covers its modules", []string{"/ws/app/.git"}, Changes{Repos: []string{"app", "app/sub"}, Worktrees: []string{"/ws/app", "/ws/app/sub"}}},
		{"submodule refs", []string{"/ws/app/.git/modules/sub/refs/heads"}, Changes{Repos: []string{"app/sub"}}},
		{"sibling with shared prefix is not inside", []string{"/ws/library"}, Changes{Rescan: true}},
		{"workspace root covers every repo beneath", []string{"/ws"}, Changes{Repos: []string{"app", "app/sub", "lib"}, Worktrees: []string{"/ws/app", "/ws/app-feat", "/ws/app/sub", "/ws/lib"}, Rescan: true}},
		{"dedupe", []string{"/ws/lib/a", "/ws/lib/b", "/ws/lib/.git/refs"}, Changes{Repos: []string{"lib"}, Worktrees: []string{"/ws/lib"}}},
		{"a dir above the worktree", []string{"/ws/app/sub/.."}, Changes{Repos: []string{"app", "app/sub"}, Worktrees: []string{"/ws/app", "/ws/app-feat", "/ws/app/sub"}}},
	} {
		got := Classify(repos, tc.dirs)
		if !slices.Equal(got.Repos, tc.want.Repos) || !slices.Equal(got.Worktrees, tc.want.Worktrees) || got.Rescan != tc.want.Rescan {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestIgnore(t *testing.T) {
	for p, want := range map[string]bool{
		"node_modules": true, "app/target": true, "app/.git/objects": true, "app/.git/modules/x/logs": true,
		"src/objects": false, "app/.git/refs": false, "app/.git": false,
	} {
		if ignore(p, true) != want {
			t.Errorf("ignore(%q) = %v", p, !want)
		}
	}
}

func TestRoots(t *testing.T) {
	got := Roots([]string{"/ws/b", "/ws", "/elsewhere/wt", "/ws/a/.git", "/wsx"})
	if want := []string{"/elsewhere/wt", "/ws", "/wsx"}; !slices.Equal(got, want) {
		t.Errorf("Roots = %v, want %v", got, want)
	}
}

// The real watch on a fixture: every kind of change arrives and is classified; Close leaves no goroutine behind.
func TestFiles(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "app"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	wt := filepath.Join(t.TempDir(), "app-feat") // outside the root: its own watch
	r.Git("worktree", "add", "-q", "-b", "feat", wt)
	repos := []Repo{{Name: "app", GitDir: filepath.Join(r.Dir, ".git"), Worktrees: []string{r.Dir, wt}}}

	warm, _ := Files(context.Background(), []string{t.TempDir()}) // mygo starts its shared goroutines once
	warm.Close()
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()

	src, err := Files(context.Background(), Roots([]string{root, wt}))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	expect := func(name string, act func(), want func(Changes) bool) {
		t.Helper()
		act()
		var all []string
		deadline := time.After(3 * time.Second)
		for {
			select {
			case dirs := <-src.Events():
				all = append(all, dirs...)
				if want(Classify(repos, all)) {
					return
				}
			case <-deadline:
				t.Errorf("%s: no matching change, saw %v -> %+v", name, all, Classify(repos, all))
				return
			}
		}
	}
	hasRepo := func(c Changes) bool { return slices.Contains(c.Repos, "app") }
	hasWt := func(p string) func(Changes) bool {
		return func(c Changes) bool { return slices.Contains(c.Worktrees, p) }
	}
	expect("commit (refs)", func() { r.Commit("two", map[string]string{"b.txt": "b\n"}) }, hasRepo)
	expect("checkout (HEAD)", func() { r.Git("checkout", "-q", "-b", "other") }, hasRepo)
	expect("pack-refs", func() { r.Git("pack-refs", "--all") }, hasRepo)
	expect("main work dir", func() { os.WriteFile(filepath.Join(r.Dir, "c.txt"), []byte("c"), 0o644) }, hasWt(r.Dir))
	expect("linked work dir", func() { os.WriteFile(filepath.Join(wt, "d.txt"), []byte("d"), 0o644) }, hasWt(wt))
	expect("stage in linked worktree (its index)", func() { testutil.NewRepoAt(t, wt).Git("add", "d.txt") }, func(c Changes) bool { return hasRepo(c) && hasWt(wt)(c) })

	src.Close()
	for i := 0; i < 50 && runtime.NumGoroutine() > before; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("goroutines leaked: %d before, %d after Close", before, n)
	}
	for range src.Events() { // buffered batches drain, then the channel is closed
	}
}

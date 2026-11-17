package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"sheaf/internal/testutil"
)

func TestOnBase(t *testing.T) {
	for name, c := range map[string]struct {
		base, branch string
		want         bool
	}{
		"remote base":  {"origin/master", "master", true},
		"local base":   {"main", "main", true},
		"feature":      {"origin/master", "feat-x", false},
		"no base":      {"", "master", false},
		"detached":     {"origin/master", "", false},
		"other remote": {"origin/master", "origin", false},
	} {
		if got := onBase(c.base, c.branch); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestGroupDirty(t *testing.T) {
	dirs := []DirtyDir{
		{Repo: "a", Path: "/a1", Dirty: 1, LastChange: 100},
		{Repo: "a", Path: "/a2", Dirty: 2, LastChange: 300},
		{Repo: "crm", Path: "/crm", Dirty: 1, LastChange: 900},
		{Repo: "b", Path: "/c", Dirty: 1, LastChange: 50},
		{Repo: "crm", Path: "/crm2", Dirty: 4, LastChange: 200},
	}
	type g struct {
		name  string
		paths []string
		files int
	}
	var have []g
	for _, x := range groupDirty(dirs) {
		var ps []string
		for _, d := range x.Dirs {
			ps = append(ps, d.Path)
		}
		have = append(have, g{x.Name, ps, x.Files})
	}
	want := []g{{"crm", []string{"/crm", "/crm2"}, 5}, {"a", []string{"/a2", "/a1"}, 3}, {"b", []string{"/c"}, 1}}
	if !reflect.DeepEqual(have, want) {
		t.Fatalf("%+v", have)
	}
	if g := groupDirty(nil); len(g) != 0 {
		t.Errorf("empty input produced dirty groups: %#v", g)
	}
}

func TestDirtyIncludesTrackedRequirements(t *testing.T) {
	testutil.Isolate(t)
	ctx := t.Context()
	root := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
	repo.Commit("init", map[string]string{"a.txt": "original\n"})
	for _, branch := range []string{"feat-yt1001", "feat-YT1002", "feat-YT10010", "dev-hk", "feat-YT1003", "feat-YT1002-YT1001"} {
		dir := filepath.Join(root, branch)
		repo.Git("worktree", "add", "-q", "-b", branch, dir, "master")
		if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte(branch+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo.Dir, "dirty.txt"), []byte("main edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.bg.Wait()
	if err := s.SetRequirement(`YT\d+`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"YT1001", "YT1002"} {
		if err := s.Track(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ArchiveRequirement("YT1002", true); err != nil {
		t.Fatal(err)
	}
	check := func(want map[string]string) {
		t.Helper()
		v, err := s.Dirty(ctx)
		if err != nil {
			t.Fatal(err)
		}
		branches := map[string]string{}
		paths := map[string]bool{}
		for _, group := range v.Groups {
			for _, dir := range group.Dirs {
				branches[dir.Branch] = dir.Requirement
				paths[dir.Path] = true
			}
		}
		if !reflect.DeepEqual(branches, want) {
			t.Errorf("dirty ownership %v, want %v", branches, want)
		}
		for _, repo := range s.Info().Repos {
			for _, dir := range repo.Worktrees {
				if dir.Requirement != want[dir.Branch] {
					t.Errorf("Info ownership of %s: %q, want %q", dir.Branch, dir.Requirement, want[dir.Branch])
				}
			}
		}
		if len(v.Files) != len(want) {
			t.Errorf("%d dirty files, want %d", len(v.Files), len(want))
		}
		for _, file := range v.Files {
			if !paths[file.Key] {
				t.Errorf("file without a directory: %+v", file)
			}
		}
	}
	want := map[string]string{
		"master": "", "dev-hk": "", "feat-YT10010": "", "feat-YT1003": "",
		"feat-yt1001": "YT1001", "feat-YT1002": "YT1002", "feat-YT1002-YT1001": "YT1001",
	}
	check(want)
	if err := s.Track("YT1003"); err != nil {
		t.Fatal(err)
	}
	want["feat-YT1003"] = "YT1003"
	check(want)
	if err := s.Untrack("YT1001"); err != nil {
		t.Fatal(err)
	}
	want["feat-yt1001"], want["feat-YT1002-YT1001"] = "", "YT1002"
	check(want)
	if err := s.Untrack("YT1002"); err != nil {
		t.Fatal(err)
	}
	want["feat-YT1002"], want["feat-YT1002-YT1001"] = "", ""
	check(want)
}

func TestDirtyListInvalidation(t *testing.T) {
	l := dirtyList{lastChange: 1}
	for _, tc := range []struct {
		name       string
		invalidate []string
		all        bool
		wantKept   []string
	}{
		{"nothing changed", []string{}, false, []string{"/a", "/b"}},
		{"one directory", []string{"/a"}, false, []string{"/b"}},
		{"unknown directory", []string{"/z"}, false, []string{"/a", "/b"}},
		{"full refresh", nil, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var d dirtyTracker
			for _, p := range []string{"/a", "/b"} {
				_, _, e := d.listOf(p)
				d.storeList(p, e, l)
			}
			if tc.all {
				d.invalidate(nil)
			} else {
				d.invalidate(tc.invalidate)
			}
			var kept []string
			for _, p := range []string{"/a", "/b"} {
				if _, ok, _ := d.listOf(p); ok {
					kept = append(kept, p)
				}
			}
			if !slices.Equal(kept, tc.wantKept) {
				t.Errorf("kept %v, want %v", kept, tc.wantKept)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{"one directory", []string{"/a"}},
		{"full refresh", nil},
	} {
		t.Run("run that began before a change is not stored/"+tc.name, func(t *testing.T) {
			var d dirtyTracker
			_, _, e := d.listOf("/a")
			d.invalidate(tc.paths)
			d.storeList("/a", e, l)
			if _, ok, _ := d.listOf("/a"); ok {
				t.Error("stale list stored")
			}
		})
	}
}

// A change seen by the watch costs one git status: the count run's status is reused by the 未提交 page's list.
func TestDirtyStatusOnce(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	testutil.NewRepo(t, filepath.Join(root, "app")).Commit("init", map[string]string{"a.txt": "a\n"})
	dir := filepath.Join(root, "app")
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.bg.Wait()
	statuses := func() int {
		n := 0
		for _, e := range s.rec.Events() {
			if e.Dir == dir && len(e.Args) > 0 && e.Args[0] == "status" {
				n++
			}
		}
		return n
	}
	for i, name := range []string{"b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644)
		before := statuses()
		s.RefreshDirty(ctx, []string{dir}, true) // what the watch does
		v, err := s.Dirty(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Files) != i+1 || !v.Files[i].Untracked {
			t.Fatalf("change %d: files %+v", i, v.Files)
		}
		if n := statuses() - before; n != 1 {
			t.Errorf("change %d: %d git status runs, want 1", i, n)
		}
	}
	if _, err := s.Dirty(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.dirty.takeStatus(dir, 0); ok {
		t.Error("the kept status is handed out once")
	}
}

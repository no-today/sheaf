package session

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

func TestBrokenConfigVanishingBetweenReadAndRename(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true}
	path := filepath.Join(opts.DataDir, "workspaces", WorkspaceID(root), "workspace.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"master":`), 0600); err != nil {
		t.Fatal(err)
	}
	opts.Now = func() time.Time { // the stamp is taken after the read: the file vanishes right there
		os.Remove(path)
		return time.Now()
	}
	s, err := Open(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.cfg.Bases == nil {
		t.Fatal("Bases must be initialised like a missing file")
	}
	if err := s.SetBase("r", "main"); err != nil { // the write that used to panic
		t.Fatal(err)
	}
	if s.Info().ConfigBackup != "" { // nothing was moved aside: the file is simply gone
		t.Fatal(s.Info().ConfigBackup)
	}
}

func TestRefreshReportsAndClearsUnreadableRepo(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	good := testutil.NewRepo(t, filepath.Join(root, "good"))
	good.Commit("one", map[string]string{"a": "a"})
	bad := filepath.Join(root, "bad", ".git")
	if err := os.MkdirAll(bad, 0755); err != nil {
		t.Fatal(err)
	}
	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true}
	s, err := Open(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	refresh := func() State {
		t.Helper()
		if err := s.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		return s.Info().State
	}
	st := refresh()
	if !st.Ready || st.Error != "" || st.Commits != 1 || len(st.Failed) != 1 || st.Failed[0].Repo != "bad" || st.Failed[0].Error == "" {
		t.Fatalf("partial update: %+v", st)
	}
	testutil.NewRepo(t, filepath.Dir(bad))
	if st := refresh(); !st.Ready || len(st.Failed) != 0 {
		t.Fatalf("recovery: %+v", st)
	}
}

func TestIncrementalRepoFailures(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		testutil.NewRepo(t, filepath.Join(root, name)).Commit("one", map[string]string{"f": name})
	}
	s, err := Open(t.Context(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	move := func(name, from, to string) {
		t.Helper()
		if err := os.Rename(filepath.Join(root, name, from), filepath.Join(root, name, to)); err != nil {
			t.Fatal(err)
		}
	}
	check := func(err error, commits int, want ...string) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		st := s.Info().State
		var names []string
		for _, f := range st.Failed {
			if f.Error == "" {
				t.Errorf("empty error for %s", f.Repo)
			}
			names = append(names, f.Repo)
		}
		if !slices.Equal(names, want) || st.Commits != commits {
			t.Errorf("failed %v, commits %d; want %v, %d", names, st.Commits, want, commits)
		}
	}
	move("b", ".git", "saved")
	check(s.UpdateRepos(t.Context(), []string{"b"}), 2, "b")
	move("a", ".git", "saved")
	check(s.UpdateRepos(t.Context(), []string{"a"}), 2, "a", "b")
	move("b", "saved", ".git")
	check(s.UpdateRepos(t.Context(), []string{"b"}), 2, "a")
	check(s.SetIgnored([]string{"b"}), 1, "a")
	check(s.SetIgnored([]string{"a"}), 1)
	check(s.SetIgnored(nil), 1, "a") // adding the still-broken repo back yields no index change
	move("a", "saved", ".git")
	check(s.SetIgnored(nil), 2) // retrack retries repos missing from the index
}

func TestBrokenWorkspaceConfig(t *testing.T) {
	testutil.Isolate(t)
	for _, kind := range []string{"truncated", "wrong type", "directory", "missing", "backup exists"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			now := time.Date(2025, 12, 31, 15, 4, 5, 0, time.Local)
			opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true, Now: func() time.Time { return now }}
			path := filepath.Join(opts.DataDir, "workspaces", WorkspaceID(root), "workspace.json")
			backup := path + ".broken-20251231-150405"
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			raw := `{"master":"custom",`
			if kind == "wrong type" {
				raw = `{"master":"custom","tracked":false}`
			}
			switch kind {
			case "missing":
			case "directory":
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "backup exists" {
				if err := os.WriteFile(backup, []byte("first"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s, err := Open(context.Background(), root, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			info := s.Info()
			if info.Master != "" || s.cfg.Bases == nil {
				t.Fatalf("invalid defaults: %+v", s.cfg)
			}
			if kind == "missing" {
				if info.ConfigBackup != "" {
					t.Fatal(info.ConfigBackup)
				}
				if _, err := os.Stat(backup); !os.IsNotExist(err) {
					t.Fatalf("unexpected backup: %v", err)
				}
				return
			}
			if kind == "backup exists" {
				// a second break in the same second renames to a free name instead of failing or overwriting
				if info.ConfigBackup != backup+"-1" {
					t.Fatalf("backup = %q", info.ConfigBackup)
				}
				if b, err := os.ReadFile(backup); err != nil || string(b) != "first" {
					t.Fatalf("first backup: %q, %v", b, err)
				}
				if b, err := os.ReadFile(backup + "-1"); err != nil || string(b) != raw {
					t.Fatalf("second backup: %q, %v", b, err)
				}
				return
			}
			if info.ConfigBackup != backup {
				t.Fatalf("backup = %q, want %q", info.ConfigBackup, backup)
			}
			if err := s.SetFavs([]string{"main"}); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				if st, err := os.Stat(backup); err != nil || !st.IsDir() {
					t.Fatalf("missing directory backup: %v", err)
				}
				return
			}
			if b, err := os.ReadFile(backup); err != nil || string(b) != raw {
				t.Fatalf("backup contents: %q, %v", b, err)
			}
		})
	}
}

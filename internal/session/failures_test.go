package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

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

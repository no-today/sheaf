package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

// The glab configuration directory is per workspace, empty by default, kept in workspace.json, and checked without being run.
func TestGlabConfigSetting(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	testutil.NewRepo(t, filepath.Join(root, "a")).Commit("init", map[string]string{"f": "1\n"})
	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_000_000, 0) }}
	s, err := Open(ctx, root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() // its refresh writes the cache: done before the temporary directories go
	if got := s.Info().GlabConfig; got != "" {
		t.Errorf("default %q, want empty (the system's glab login)", got)
	}
	dir := t.TempDir()
	if err := s.SetGlabConfig("  " + dir + " "); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(ctx, root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := s2.Info().GlabConfig; got != dir {
		t.Errorf("reopened %q, want %q", got, dir)
	}
	for dir, ok := range map[string]bool{"": true, dir: true, filepath.Join(dir, "nope"): false} {
		if got := s.CheckGlabConfig(dir); (got == "") != ok {
			t.Errorf("CheckGlabConfig(%q) = %q", dir, got)
		}
	}
}

package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestLegacyDirtyRequirementsAreIgnoredWithoutRewritingConfig(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
	repo.Commit("init", map[string]string{"a.txt": "original\n"})
	repo.Commit("YT1001 landed", map[string]string{"landed.txt": "landed\n"})
	if err := os.WriteFile(filepath.Join(repo.Dir, "dirty.txt"), []byte("main edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	configPath := filepath.Join(data, "workspaces", workspaceID(root), "workspace.json")
	before, err := json.Marshal(map[string]any{
		"requirement":       `YT\d+`,
		"tracked":           []string{"YT1001"},
		"dirtyRequirements": map[string]string{repo.Dir: "YT1001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), root, Options{DataDir: data, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.bg.Wait()
	t.Cleanup(s.Close)
	list, err := s.Requirements(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := stageOnly(list, "YT1001"); got.Stage != StageLanded || got.Repos != 1 {
		t.Errorf("legacy assignment affected requirement: %+v", got)
	}
	in := TagAggInput{Cond: &query.Conditions{Q: "YT1001", Authors: []string{}}, Requirement: "YT1001", DiffMode: DiffWorktree}
	files, err := s.TagAggFiles(t.Context(), in)
	if err != nil || len(files) != 0 {
		t.Errorf("legacy assignment affected requirement files: %+v, %v", files, err)
	}
	v, err := s.Dirty(t.Context())
	if err != nil || len(v.Groups) != 1 || len(v.Groups[0].Dirs) != 1 || len(v.Files) != 1 {
		t.Errorf("main branch changes missing: %+v, %v", v, err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("opening the session rewrote the legacy config: %s", after)
	}
}

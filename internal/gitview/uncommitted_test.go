package gitview

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sheaf/internal/gitcli"

	"sheaf/internal/testutil"
)

func TestWorktreeStatusFiles(t *testing.T) {
	t.Setenv("SHEAF_HOME", t.TempDir())
	for _, tc := range []struct {
		name    string
		changes []gitcli.Change
		want    []UncommittedFile
	}{
		{"clean", nil, []UncommittedFile{}},
		{"staged and unstaged", []gitcli.Change{
			{Path: "both", Area: "unstaged", Status: 'M'},
			{Path: "both", Area: "staged", Status: 'M'},
		}, []UncommittedFile{
			{Path: "both", Area: "staged", Status: "M"},
			{Path: "both", Area: "unstaged", Status: "M"},
		}},
		{"untracked", []gitcli.Change{{Path: "new file", Area: "untracked", Status: 'A'}},
			[]UncommittedFile{{Path: "new file", Area: "untracked", Status: "A"}}},
		{"rename", []gitcli.Change{{Path: "new", OldPath: "old", Area: "staged", Status: 'R'}},
			[]UncommittedFile{{Path: "new", OldPath: "old", Area: "staged", Status: "R"}}},
		{"copy", []gitcli.Change{{Path: "copy", OldPath: "original", Area: "staged", Status: 'C'}},
			[]UncommittedFile{{Path: "copy", OldPath: "original", Area: "staged", Status: "C"}}},
		{"delete", []gitcli.Change{{Path: "gone", Area: "unstaged", Status: 'D'}},
			[]UncommittedFile{{Path: "gone", Area: "unstaged", Status: "D"}}},
		{"path order", []gitcli.Change{
			{Path: "z", Area: "staged", Status: 'A'},
			{Path: "a", Area: "untracked", Status: 'A'},
		}, []UncommittedFile{
			{Path: "a", Area: "untracked", Status: "A"},
			{Path: "z", Area: "staged", Status: "A"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := WorktreeStatus{changes: slices.Clone(tc.changes)}
			got := status.Files()
			if got == nil || !slices.Equal(got, tc.want) {
				t.Fatalf("Files() = %+v, want %+v", got, tc.want)
			}
			if len(got) > 0 {
				got[0].Path = "changed"
			}
			if !slices.Equal(status.changes, tc.changes) || !slices.Equal(status.Files(), tc.want) {
				t.Fatal("Files mutated the cached status")
			}
		})
	}
}

func TestUncommitted(t *testing.T) {
	testutil.Isolate(t)
	t.Setenv("SHEAF_HOME", t.TempDir())
	ctx := context.Background()
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "r"))
	r.Commit("init", map[string]string{"a.txt": "1\n2\n"})
	os.WriteFile(filepath.Join(r.Dir, "a.txt"), []byte("1\n2\n3\n"), 0o644)
	r.Git("add", "a.txt")
	os.WriteFile(filepath.Join(r.Dir, "a.txt"), []byte("1\n3\n"), 0o644)
	os.WriteFile(filepath.Join(r.Dir, "new.txt"), []byte("x\ny\n"), 0o644)
	os.WriteFile(filepath.Join(r.Dir, "blob.bin"), []byte("a\x00b"), 0o644)

	fs, err := Uncommitted(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileStatus{
		{Path: "a.txt", Area: "staged", Status: "M", Add: 1},
		{Path: "a.txt", Area: "unstaged", Status: "M", Del: 1},
		{Path: "blob.bin", Area: "untracked", Status: "A", Binary: true},
		{Path: "new.txt", Area: "untracked", Status: "A", Add: 2},
	}
	if len(fs) != len(want) {
		t.Fatalf("got %+v", fs)
	}
	for i := range want {
		if fs[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, fs[i], want[i])
		}
	}

	p, err := UncommittedPatch(ctx, r.Dir, "new.txt", "", "untracked")
	if err != nil || len(p.Hunks) != 1 || p.Hunks[0].Header != "@@ -0,0 +1,2 @@" || p.Hunks[0].Lines[1].Text != "y" || p.Hunks[0].Lines[1].New != 2 {
		t.Errorf("untracked patch: %+v %v", p, err)
	}
	if p, _ := UncommittedPatch(ctx, r.Dir, "blob.bin", "", "untracked"); !p.Binary {
		t.Error("untracked binary not detected")
	}
	if _, err := UncommittedPatch(ctx, r.Dir, "../outside", "", "untracked"); err == nil {
		t.Error("path outside the worktree was read")
	}
	if p, _ := UncommittedPatch(ctx, r.Dir, "a.txt", "", "unstaged"); len(p.Hunks) != 1 {
		t.Errorf("unstaged patch: %+v", p)
	}
	// head: staged (+3) and unstaged (-2) together against HEAD
	if p, err := UncommittedPatch(ctx, r.Dir, "a.txt", "", "head"); err != nil || len(p.Hunks) != 1 || p.Hunks[0].Header != "@@ -1,2 +1,2 @@" {
		t.Errorf("head patch: %+v %v", p, err)
	}
}

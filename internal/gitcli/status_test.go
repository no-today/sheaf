package gitcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

func TestParseStatusV2(t *testing.T) {
	in := "1 MM N... 100644 100644 100644 aaa bbb both.txt\x00" +
		"2 R. N... 100644 100644 100644 aaa aaa R100 new.txt\x00old.txt\x00" +
		"1 .D N... 100644 100644 000000 aaa aaa gone.txt\x00" +
		"1 .M S.M. 160000 160000 160000 aaa aaa sub\x00" +
		"u UU N... 100644 100644 100644 100644 a b c conflict.txt\x00" +
		"? dir/new file.txt\x00"
	want := []Change{
		{Path: "both.txt", Area: Staged, Status: 'M'},
		{Path: "both.txt", Area: Unstaged, Status: 'M'},
		{Path: "new.txt", OldPath: "old.txt", Area: Staged, Status: 'R'},
		{Path: "gone.txt", Area: Unstaged, Status: 'D'},
		{Path: "sub", Area: Unstaged, Status: 'M', Submodule: true},
		{Path: "conflict.txt", Area: Unstaged, Status: 'U'},
		{Path: "dir/new file.txt", Area: Untracked, Status: 'A'},
	}
	got := parseStatusV2([]byte(in))
	if !slices.Equal(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if n := DirtyCount(got); n != 6 {
		t.Errorf("DirtyCount = %d, want 6", n)
	}
}

func write(t *testing.T, dir, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
	if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStatusWithHead(t *testing.T) {
	testutil.Isolate(t)
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	write(t, r.Dir, "new.txt", "new\n")
	changes, head, err := StatusWithHead(t.Context(), r.Dir)
	if err != nil || !head.IsZero() || len(changes) != 1 || changes[0].Area != Untracked {
		t.Fatalf("unborn HEAD: changes=%+v head=%s err=%v", changes, head, err)
	}
	oid := r.Commit("init", nil)
	for _, detached := range []bool{false, true} {
		if detached {
			r.Git("checkout", "--detach", "-q", oid)
		}
		changes, head, err = StatusWithHead(t.Context(), r.Dir)
		if err != nil || head.String() != oid || len(changes) != 0 {
			t.Fatalf("detached=%v: changes=%+v head=%s err=%v", detached, changes, head, err)
		}
	}
}

func TestStatusAndDiff(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"both.txt": "one\n", "old.txt": "keep\nthese\nlines\n", "gone.txt": "bye\n"})

	write(t, r.Dir, "both.txt", "one\nstaged\n")
	r.Git("add", "both.txt")
	write(t, r.Dir, "both.txt", "one\nstaged\nunstaged\n")
	r.Git("mv", "old.txt", "new.txt")
	os.Remove(filepath.Join(r.Dir, "gone.txt"))
	write(t, r.Dir, "bin.dat", "\x00\x01\x02")
	r.Git("add", "bin.dat")
	write(t, r.Dir, "u.txt", "fresh\n")

	// Status and diff must not rewrite the index even when its stat data is stale.
	future := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(r.Dir, "both.txt"), future, future)
	idx := filepath.Join(r.Dir, ".git", "index")
	before, _ := os.ReadFile(idx)

	cs, err := Status(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Change{}
	for _, c := range cs {
		got[c.Area+" "+c.Path] = c
	}
	for key, st := range map[string]byte{"staged both.txt": 'M', "unstaged both.txt": 'M', "staged new.txt": 'R', "unstaged gone.txt": 'D', "staged bin.dat": 'A', "untracked u.txt": 'A'} {
		if got[key].Status != st {
			t.Errorf("%s: status %q, want %q (all: %+v)", key, got[key].Status, st, cs)
		}
	}
	if got["staged new.txt"].OldPath != "old.txt" {
		t.Errorf("rename old path: %+v", got["staged new.txt"])
	}

	staged, err := DiffPatch(ctx, r.Dir, true, "both.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if lines := added(staged); lines != "staged" {
		t.Errorf("staged diff adds %q", lines)
	}
	unstaged, _ := DiffPatch(ctx, r.Dir, false, "both.txt", "")
	if lines := added(unstaged); lines != "unstaged" {
		t.Errorf("unstaged diff adds %q", lines)
	}
	ren, _ := DiffPatch(ctx, r.Dir, true, "new.txt", "old.txt")
	if len(ren.Hunks) != 0 || ren.Binary {
		t.Errorf("pure rename should have no hunks: %+v", ren)
	}
	del, _ := DiffPatch(ctx, r.Dir, false, "gone.txt", "")
	if len(del.Hunks) != 1 || del.Hunks[0].Lines[0].Kind != '-' {
		t.Errorf("deletion diff: %+v", del)
	}
	bin, _ := DiffPatch(ctx, r.Dir, true, "bin.dat", "")
	if !bin.Binary {
		t.Errorf("binary not detected: %+v", bin)
	}
	ns, _ := DiffNumstat(ctx, r.Dir, true)
	if !slices.ContainsFunc(ns, func(n NumStat) bool { return n.Path == "new.txt" && n.OldPath == "old.txt" }) {
		t.Errorf("staged numstat misses rename: %+v", ns)
	}

	if after, _ := os.ReadFile(idx); !bytes.Equal(before, after) {
		t.Error("the index file was rewritten")
	}
}

func added(p Patch) string {
	var out []string
	for _, h := range p.Hunks {
		for _, l := range h.Lines {
			if l.Kind == '+' {
				out = append(out, l.Text)
			}
		}
	}
	return strings.Join(out, "\n")
}

func TestStatusSubmoduleAndWorktreeConfig(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	lib := testutil.NewRepo(t, filepath.Join(root, "lib"))
	lib.Commit("lib", map[string]string{"l.txt": "l\n"})
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	r.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, "sub")
	r.Commit("add sub", nil)
	write(t, r.Dir, "sub/l.txt", "changed\n")

	cs, err := Status(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Path != "sub" || !cs[0].Submodule || cs[0].Area != Unstaged {
		t.Errorf("submodule change: %+v", cs)
	}

	// A linked worktree with per-worktree config (extensions.worktreeConfig).
	r.Git("config", "extensions.worktreeConfig", "true")
	wt := filepath.Join(root, "wt")
	r.Git("worktree", "add", "-q", "-b", "feat", wt)
	testutil.NewRepoAt(t, wt).Git("config", "--worktree", "user.name", "Other")
	write(t, wt, "a.txt", "a\nwt\n")
	cs, err = Status(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Path != "a.txt" || cs[0].Area != Unstaged {
		t.Errorf("worktreeConfig worktree: %+v", cs)
	}
	p, err := DiffPatch(ctx, wt, false, "a.txt", "")
	if err != nil || added(p) != "wt" {
		t.Errorf("worktree diff: %q %v", added(p), err)
	}
}

package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sheaf/internal/testutil"
)

func TestLogRefsShow(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	c1 := r.Commit("init", map[string]string{"a.txt": "one\ntwo\n", "dir/b.txt": "b\n"})
	r.Git("tag", "-a", "v1", "-m", "release")
	r.Git("checkout", "-q", "-b", "feat")
	r.Git("mv", "a.txt", "renamed.txt")
	c2 := r.Commit("YT03374 rename and edit", map[string]string{"renamed.txt": "one\ntwo\nthree\n", "tab\tname.txt": "x\n"}, testutil.Author{Name: "李娜", Email: "li.na@example.com"})
	r.Git("checkout", "-q", "master")
	r.Commit("on master", map[string]string{"dir/b.txt": ""})
	m := r.Merge("feat", "Merge branch 'feat'")

	refs, err := Refs(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Oid{}
	for _, ref := range refs {
		byName[ref.Name] = ref.Oid
	}
	if byName["refs/tags/v1"].String() != c1 {
		t.Errorf("annotated tag not peeled: %v", byName["refs/tags/v1"])
	}
	if byName["refs/heads/master"].String() != m || byName["refs/heads/feat"].String() != c2 {
		t.Errorf("branch refs wrong: %v", byName)
	}

	var got []*RawCommit
	tips := []Oid{byName["refs/heads/master"]}
	if err := Log(ctx, r.Dir, tips, nil, func(c *RawCommit) error { got = append(got, c); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 commits, got %d", len(got))
	}
	find := func(h string) *RawCommit {
		for _, c := range got {
			if c.Oid.String() == h {
				return c
			}
		}
		t.Fatalf("commit %s missing", h)
		return nil
	}
	rc := find(c2)
	if rc.Author != "李娜" || rc.Email != "li.na@example.com" || rc.Subject != "YT03374 rename and edit" {
		t.Errorf("header: %+v", rc)
	}
	var sawRename, sawTab bool
	for _, f := range rc.Files {
		if f.Status == 'R' && f.OldPath == "a.txt" && f.Path == "renamed.txt" {
			sawRename = true
		}
		if f.Path == "tab\tname.txt" {
			sawTab = true
		}
	}
	if !sawRename || !sawTab {
		t.Errorf("files: %+v", rc.Files)
	}
	// merge lists files against its first parent
	if mc := find(m); len(mc.Parents) != 2 || len(mc.Files) == 0 {
		t.Errorf("merge: %+v", mc)
	}

	// exclude: only commits on feat not on c1
	var n int
	Log(ctx, r.Dir, []Oid{byName["refs/heads/feat"]}, []Oid{byName["refs/tags/v1"]}, func(*RawCommit) error { n++; return nil })
	if n != 1 {
		t.Errorf("feat..v1: want 1, got %d", n)
	}

	o2, _ := ParseOid(c2)
	ns, err := ShowNumstat(ctx, r.Dir, o2)
	if err != nil {
		t.Fatal(err)
	}
	var renamed *NumStat
	for i := range ns {
		if ns[i].Path == "renamed.txt" {
			renamed = &ns[i]
		}
	}
	if renamed == nil || renamed.OldPath != "a.txt" || renamed.Add != 1 || renamed.Del != 0 {
		t.Errorf("numstat: %+v", ns)
	}

	p, err := ShowPatch(ctx, r.Dir, o2, "renamed.txt", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hunks) != 1 {
		t.Fatalf("patch hunks: %+v", p)
	}
	last := p.Hunks[0].Lines[len(p.Hunks[0].Lines)-1]
	if last.Kind != '+' || last.Text != "three" || last.New != 3 {
		t.Errorf("patch last line: %+v", last)
	}

	msg, _ := ShowMessage(ctx, r.Dir, o2)
	if msg != "YT03374 rename and edit" {
		t.Errorf("message %q", msg)
	}
}

func TestWorktrees(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"a": "1"})
	r.Git("worktree", "add", "-q", "-b", "feat-x", filepath.Join(root, "repo-x"))
	ws, err := Worktrees(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 || ws[1].Branch != "refs/heads/feat-x" || ws[1].Head.IsZero() {
		t.Errorf("worktrees: %+v", ws)
	}
}

func TestUnquote(t *testing.T) {
	for in, want := range map[string]string{`"tab\tname"`: "tab\tname", `plain`: "plain", `"\346\265\213"`: "测"} {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%s) = %q, want %q", in, got, want)
		}
	}
}

// Read commands use stable output flags and avoid optional index locks.
func TestCmdFlags(t *testing.T) {
	c := Cmd(context.Background(), "/r", "log")
	want := []string{"git", "-C", "/r", "--no-optional-locks", "-c", "core.quotePath=false", "-c", "color.ui=false", "log"}
	if !slices.Equal(c.Args, want) {
		t.Errorf("args %v", c.Args)
	}
	for _, e := range []string{"LC_ALL=C", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0"} {
		if !slices.Contains(c.Env, e) {
			t.Errorf("env lacks %s", e)
		}
	}
	for _, args := range [][]string{{"cat-file", "--batch"}} {
		c := Cmd(context.Background(), "/r", args...)
		if c.Err != nil || KindOf(args) != KindRead || !slices.Equal(c.Args[len(c.Args)-len(args):], args) {
			t.Errorf("batch commit read: args %v, error %v", c.Args, c.Err)
		}
		if c := cmd(context.Background(), "/r", true, args); c.Err == nil {
			t.Errorf("read allowed through Write: %v", args)
		}
	}
	for _, args := range [][]string{{"cat-file"}, {"cat-file", "-p", "HEAD"}, {"cat-file", "--batch", "--filters"}} {
		if c := Cmd(context.Background(), "/r", args...); c.Err == nil {
			t.Errorf("unexpected cat-file form allowed: %v", args)
		}
	}
	args := []string{"merge", "--ff-only", "--end-of-options", "refs/remotes/origin/main"}
	write := cmd(context.Background(), "/r", true, args)
	if write.Err != nil || !slices.Equal(write.Args[len(write.Args)-len(args):], args) {
		t.Errorf("fast-forward write: args %v, error %v", write.Args, write.Err)
	}
	for _, rejected := range [][]string{
		{"merge", "--no-edit", "--end-of-options", "main"},
		{"switch", "--end-of-options", "main"},
		{"merge", "--abort"}, {"merge-tree", "--write-tree", "a", "b"},
		{"commit-tree", "tree"}, {"update-ref", "ref", "oid"}, {"worktree", "add", "path", "branch"},
	} {
		if c := cmd(context.Background(), "/r", true, rejected); c.Err == nil {
			t.Errorf("write allows removed command %v", rejected)
		}
	}
}

func TestIgnoreSpace(t *testing.T) {
	testutil.Isolate(t)
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	c1 := r.Commit("init", map[string]string{"a.txt": "one\ntwo\n"})
	c2 := r.Commit("reindent", map[string]string{"a.txt": "one \n  two\nthree\n"})
	o1, _ := ParseOid(c1)
	o2, _ := ParseOid(c2)
	count := func(p Patch) (n int) {
		for _, h := range p.Hunks {
			for _, l := range h.Lines {
				if l.Kind == '+' || l.Kind == '-' {
					n++
				}
			}
		}
		return n
	}
	for name, get := range map[string]func(context.Context) (Patch, error){
		"show":      func(ctx context.Context) (Patch, error) { return ShowPatch(ctx, r.Dir, o2, "a.txt", "") },
		"threedot":  func(ctx context.Context) (Patch, error) { return DiffMergeBasePatch(ctx, r.Dir, o1, o2, "a.txt", "") },
		"mergebase": func(ctx context.Context) (Patch, error) { return DiffMergeBaseWorktree(ctx, r.Dir, o1, "a.txt", "") },
	} {
		all, err := get(context.Background())
		if err != nil {
			t.Fatal(name, err)
		}
		ws, err := get(IgnoreSpace(context.Background(), true))
		if err != nil {
			t.Fatal(name, err)
		}
		if count(all) != 5 || count(ws) != 1 { // two lines rewritten + one added; only the added one survives -w
			t.Errorf("%s: %d changed lines, with -w %d", name, count(all), count(ws))
		}
	}
	// uncommitted
	r.Git("reset", "-q", "--hard", c1)
	os.WriteFile(filepath.Join(r.Dir, "a.txt"), []byte("one\n\ttwo\n"), 0o644)
	if p, _ := DiffPatch(context.Background(), r.Dir, false, "a.txt", ""); count(p) != 2 {
		t.Errorf("uncommitted: %+v", p)
	}
	if p, _ := DiffPatch(IgnoreSpace(context.Background(), true), r.Dir, false, "a.txt", ""); count(p) != 0 {
		t.Errorf("uncommitted with -w: %+v", p)
	}
}

func TestCommandLine(t *testing.T) {
	tests := []struct {
		dir  string
		args []string
		want string
	}{
		{"/r", []string{"status", "-z"}, "LC_ALL=C GIT_OPTIONAL_LOCKS=0 GIT_TERMINAL_PROMPT=0 GIT_PAGER=cat git -C /r --no-optional-locks -c core.quotePath=false -c color.ui=false status -z"},
		{"", []string{"log", "--format=%H %s", "it's"}, `LC_ALL=C GIT_OPTIONAL_LOCKS=0 GIT_TERMINAL_PROMPT=0 GIT_PAGER=cat git --no-optional-locks -c core.quotePath=false -c color.ui=false log '--format=%H %s' 'it'\''s'`},
		{"/a b", []string{"diff", "a...b", ""}, "LC_ALL=C GIT_OPTIONAL_LOCKS=0 GIT_TERMINAL_PROMPT=0 GIT_PAGER=cat git -C '/a b' --no-optional-locks -c core.quotePath=false -c color.ui=false diff a...b ''"},
	}
	for _, tc := range tests {
		if got := CommandLine(tc.dir, tc.args); got != tc.want {
			t.Errorf("CommandLine(%q, %q)\n got  %s\n want %s", tc.dir, tc.args, got, tc.want)
		}
	}
}

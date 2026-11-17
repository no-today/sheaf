package gitops

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sheaf/internal/gitcli"
	"sheaf/internal/testutil"
)

func TestDecideUpdate(t *testing.T) {
	base := UpdateRow{Repo: "r", Branch: "release", Local: true, Remote: "refs/remotes/origin/release"}
	with := func(f func(*UpdateRow)) UpdateRow { r := base; f(&r); return r }
	tests := []struct {
		name string
		row  UpdateRow
		how  string
		skip string
	}{
		{"not checked out", base, HowRef, ""},
		{"checked out", with(func(r *UpdateRow) { r.Worktree = "/w" }), HowWorktree, ""},
		{"checked out, dirty", with(func(r *UpdateRow) { r.Worktree, r.Dirty = "/w", 1 }), HowSkip, SkipDirty},
		{"checked out, mid-merge", with(func(r *UpdateRow) { r.Worktree, r.Busy = "/w", true }), HowSkip, SkipMerging},
		{"dirty elsewhere does not matter", with(func(r *UpdateRow) { r.Dirty = 3 }), HowRef, ""},
		{"unpushed commits", with(func(r *UpdateRow) { r.Ahead = 2 }), HowSkip, SkipUnpushed},
		{"no remote branch", with(func(r *UpdateRow) { r.Remote = "" }), HowSkip, SkipNoRemote},
		{"no local branch", with(func(r *UpdateRow) { r.Local = false }), HowSkip, SkipNoLocal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			how, skip := DecideUpdate(tc.row)
			if how != tc.how || skip != tc.skip {
				t.Errorf("got %s %s, want %s %s", how, skip, tc.how, tc.skip)
			}
		})
	}
}

// batchFixture: a bare origin with master and release, a working clone ("up") that pushes to it, and the clone under test ("repo",
// on master). Nothing is fetched into repo after the clone, so origin runs ahead of origin/* until a test fetches.
type batchFixture struct {
	t      *testing.T
	up     *testutil.Repo
	repo   *testutil.Repo
	origin string
	rec    *gitcli.Recorder
	ctx    context.Context
}

func newBatchFixture(t *testing.T) *batchFixture {
	testutil.Isolate(t)
	root := t.TempDir()
	up := testutil.NewRepo(t, filepath.Join(root, "up"))
	up.Commit("init", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	up.Git("branch", "-M", "master")
	up.Git("branch", "release")
	origin := filepath.Join(root, "origin.git")
	up.Git("clone", "-q", "--bare", up.Dir, origin)
	up.Git("remote", "add", "origin", origin)
	r := testutil.NewRepoAt(t, filepath.Join(root, "repo"))
	up.Git("clone", "-q", origin, r.Dir)
	r.Git("config", "user.name", "Me")
	r.Git("config", "user.email", "me@example.com")
	r.Git("branch", "release", "origin/release")
	r.Git("branch", "feat", "master")
	rec := &gitcli.Recorder{}
	return &batchFixture{t: t, up: up, repo: r, origin: origin, rec: rec, ctx: gitcli.WithSource(gitcli.WithRecorder(context.Background(), rec), "test")}
}

// push adds n commits to branch of origin, from up.
func (f *batchFixture) push(branch string, n int) {
	f.up.Git("checkout", "-q", branch)
	for i := range n {
		f.up.Commit(branch+" moves", map[string]string{"m-" + branch + ".txt": strings.Repeat("x", i+1) + "\n"})
	}
	f.up.Git("push", "-q", "origin", branch)
	f.up.Git("checkout", "-q", "master")
}

func (f *batchFixture) update(branch, worktree string) UpdateRow {
	return UpdateRow{Repo: "repo", Dir: f.repo.Dir, Branch: branch, Local: true, Remote: "refs/remotes/origin/" + branch, Worktree: worktree}
}

func (f *batchFixture) head() string { return f.repo.Git("rev-parse", "--abbrev-ref", "HEAD") }

// writes are the git commands of the log that changed something, as "subcommand args…".
func (f *batchFixture) writes() []string {
	var out []string
	for _, e := range f.rec.Events() {
		if e.Kind == gitcli.KindWrite {
			out = append(out, strings.Join(e.Args, " "))
		}
	}
	slices.Reverse(out) // Events is newest first
	return out
}

func TestRunUpdate(t *testing.T) {
	t.Run("a branch nothing has checked out is fast-forwarded without a checkout", func(t *testing.T) {
		f := newBatchFixture(t)
		f.push("release", 2)
		res := RunUpdate(f.ctx, []UpdateRow{f.update("release", "")}, Progress{})[0]
		if res.Outcome != Updated || res.Commits != 2 || res.Error != "" {
			t.Fatalf("%+v", res)
		}
		if f.repo.Git("rev-parse", "release") != f.up.Git("rev-parse", "release") || f.head() != "master" {
			t.Error("release not fast-forwarded, or the checkout moved")
		}
		if f.repo.Git("rev-parse", "origin/release") != f.repo.Git("rev-parse", "release") {
			t.Error("origin/release not updated")
		}
		want := "fetch --no-tags --end-of-options origin refs/heads/release:refs/heads/release"
		if got := f.writes(); !slices.Equal(got, []string{want}) {
			t.Errorf("writes %q, want %q", got, want)
		}
	})

	t.Run("a branch checked out in a worktree is fast-forwarded there", func(t *testing.T) {
		f := newBatchFixture(t)
		f.push("master", 1)
		res := RunUpdate(f.ctx, []UpdateRow{f.update("master", f.repo.Dir)}, Progress{})[0]
		if res.Outcome != Updated || res.Commits != 1 {
			t.Fatalf("%+v", res)
		}
		if f.repo.Git("rev-parse", "HEAD") != f.up.Git("rev-parse", "master") {
			t.Error("not fast-forwarded")
		}
		got := f.writes()
		if len(got) != 2 || !strings.HasPrefix(got[0], "fetch --no-tags --end-of-options origin master") || got[1] != "merge --ff-only --end-of-options refs/remotes/origin/master" {
			t.Errorf("writes %q", got)
		}
	})

	t.Run("a worktree with uncommitted changes is skipped", func(t *testing.T) {
		f := newBatchFixture(t)
		f.push("master", 1)
		before := f.repo.Git("rev-parse", "master")
		os.WriteFile(filepath.Join(f.repo.Dir, "a.txt"), []byte("edited\n"), 0o644)
		res := RunUpdate(f.ctx, []UpdateRow{f.update("master", f.repo.Dir)}, Progress{})[0] // Dirty 0: the sheet is old
		if res.Outcome != Skipped || res.Skip != SkipDirty || f.repo.Git("rev-parse", "master") != before {
			t.Fatalf("%+v", res)
		}
		if got := f.writes(); len(got) != 0 {
			t.Errorf("writes %q", got)
		}
	})

	t.Run("a branch with commits of its own is not moved", func(t *testing.T) {
		f := newBatchFixture(t)
		f.repo.Git("checkout", "-q", "release")
		mine := f.repo.Commit("local only", map[string]string{"l.txt": "l\n"})
		f.repo.Git("checkout", "-q", "master")
		f.push("release", 1)
		// The sheet saw it in step with origin (Ahead 0): git refuses the fetch, which is a skip, not a failure.
		res := RunUpdate(f.ctx, []UpdateRow{f.update("release", "")}, Progress{})[0]
		if res.Outcome != Skipped || res.Skip != SkipUnpushed {
			t.Fatalf("%+v", res)
		}
		if f.repo.Git("rev-parse", "release") != mine {
			t.Error("release moved")
		}
		// And when the sheet knew, nothing runs at all.
		row := f.update("release", "")
		row.Ahead = 1
		n := len(f.writes())
		if res := RunUpdate(f.ctx, []UpdateRow{row}, Progress{})[0]; res.Outcome != Skipped || res.Skip != SkipUnpushed || len(f.writes()) != n {
			t.Errorf("%+v", res)
		}
	})

	t.Run("up to date", func(t *testing.T) {
		f := newBatchFixture(t)
		res := RunUpdate(f.ctx, []UpdateRow{f.update("release", "")}, Progress{})[0]
		if res.Outcome != UpToDate {
			t.Fatalf("%+v", res)
		}
	})

	t.Run("the rest of the repo's rows still run after a skip", func(t *testing.T) {
		f := newBatchFixture(t)
		f.push("master", 1)
		f.push("release", 1)
		os.WriteFile(filepath.Join(f.repo.Dir, "a.txt"), []byte("edited\n"), 0o644)
		res := RunUpdate(f.ctx, []UpdateRow{f.update("master", f.repo.Dir), f.update("release", "")}, Progress{})
		if res[0].Outcome != Skipped || res[1].Outcome != Updated {
			t.Errorf("%+v", res)
		}
	})
}

func TestFetchAll(t *testing.T) {
	f := newBatchFixture(t)
	bare := testutil.NewRepo(t, filepath.Join(filepath.Dir(f.repo.Dir), "no-origin"))
	bare.Commit("init", map[string]string{"a.txt": "a\n"})
	f.push("master", 1)
	res := FetchAll(f.ctx, []FetchTarget{{Repo: "repo", Dir: f.repo.Dir}, {Repo: "no-origin", Dir: bare.Dir}}, Progress{})
	if res[0].Outcome != Fetched || res[1].Outcome != NoRemote || res[1].Error != "" {
		t.Fatalf("%+v", res)
	}
	if f.repo.Git("rev-parse", "origin/master") != f.up.Git("rev-parse", "master") {
		t.Error("origin/master not updated")
	}
	if f.repo.Git("rev-parse", "master") == f.up.Git("rev-parse", "master") {
		t.Error("the local branch moved")
	}
	if got := f.writes(); !slices.Equal(got, []string{"fetch --end-of-options origin"}) {
		t.Errorf("writes %q", got)
	}
	os.RemoveAll(f.origin)
	if res := FetchAll(f.ctx, []FetchTarget{{Repo: "repo", Dir: f.repo.Dir}}, Progress{}); res[0].Outcome != Failed || res[0].Error == "" {
		t.Errorf("%+v", res)
	}
}

func TestBusy(t *testing.T) {
	f := newBatchFixture(t)
	if Busy(f.repo.Dir) {
		t.Fatal("busy while idle")
	}
	for _, name := range []string{"MERGE_HEAD", "rebase-merge", "CHERRY_PICK_HEAD"} {
		p := filepath.Join(f.repo.Dir, ".git", name)
		os.WriteFile(p, []byte("x"), 0o644)
		if !Busy(f.repo.Dir) {
			t.Errorf("not busy with %s", name)
		}
		os.Remove(p)
	}
}

// Stopping a batch leaves the row asked about and the rows after it skipped as stopped, without running them.
func TestBatchStopsBeforeARow(t *testing.T) {
	var done []int
	p := Progress{Start: func(i int) bool { return i == 0 }, Done: func(i int) { done = append(done, i) }}
	res := RunMR(context.Background(), []MRRow{{Key: "a", Missing: true}, {Key: "b"}, {Key: "c"}}, p)
	if res[0].Skip == SkipStopped {
		t.Errorf("the first row did not run: %+v", res[0])
	}
	for _, r := range res[1:] {
		if r.Outcome != Skipped || r.Skip != SkipStopped {
			t.Errorf("row %s ran after the stop: %+v", r.Key, r)
		}
	}
	if len(done) != 3 {
		t.Errorf("done reported %v, want every row", done)
	}
	rows := []UpdateRow{{Repo: "r", Dir: "/nowhere", Branch: "x"}}
	if r := RunUpdate(context.Background(), rows, Progress{Start: func(int) bool { return false }})[0]; r.Outcome != Skipped || r.Skip != SkipStopped {
		t.Errorf("update row ran after the stop: %+v", r)
	}
}

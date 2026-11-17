// Package testutil builds throwaway git repositories for tests.
package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate points git at an empty global config so the user's settings never
// leak into tests. Call it first in every test that runs git.
func Isolate(t testing.TB) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(cfg, []byte("[user]\n\tname = Me\n\temail = me@example.com\n[init]\n\tdefaultBranch = master\n[protocol \"file\"]\n\tallow = always\n"), 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

type Repo struct {
	t    testing.TB
	Dir  string
	tick int64
}

// NewRepo runs git init in dir (created if missing).
func NewRepo(t testing.TB, dir string) *Repo {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	r := &Repo{t: t, Dir: dir, tick: 1_700_000_000}
	r.Git("init", "-q")
	return r
}

// NewRepoAt wraps an existing working tree (a linked worktree, say) without git init.
func NewRepoAt(t testing.TB, dir string) *Repo {
	return &Repo{t: t, Dir: dir, tick: 1_700_000_000}
}

// Git runs git in the repo and returns trimmed stdout; it fails the test on error.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	return run(r.t, r.Dir, nil, args...)
}

func run(t testing.TB, dir string, env []string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Author overrides the author of the next Commit.
type Author struct{ Name, Email string }

// Commit writes files (path -> content; "" deletes), commits with msg and a
// strictly increasing timestamp, and returns the full hash.
func (r *Repo) Commit(msg string, files map[string]string, who ...Author) string {
	r.t.Helper()
	for p, content := range files {
		full := filepath.Join(r.Dir, p)
		if content == "" {
			os.Remove(full)
			continue
		}
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	r.Git("add", "-A")
	r.tick += 60
	date := fmt.Sprintf("@%d +0800", r.tick)
	env := []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	if len(who) > 0 {
		env = append(env, "GIT_AUTHOR_NAME="+who[0].Name, "GIT_AUTHOR_EMAIL="+who[0].Email)
	}
	run(r.t, r.Dir, env, "commit", "-q", "--allow-empty", "-m", msg)
	return r.Git("rev-parse", "HEAD")
}

func (r *Repo) Merge(branch, msg string) string {
	r.t.Helper()
	r.tick += 60
	date := fmt.Sprintf("@%d +0800", r.tick)
	run(r.t, r.Dir, []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}, "merge", "-q", "--no-ff", "-m", msg, branch)
	return r.Git("rev-parse", "HEAD")
}

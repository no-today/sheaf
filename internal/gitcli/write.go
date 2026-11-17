package gitcli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// The writes Sheaf makes, each a fixed command line, and the reads they need.

// Version is git's version as major, minor.
func Version(ctx context.Context) (int, int, error) {
	out, err := Output(ctx, "", "version")
	if err != nil {
		return 0, 0, err
	}
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		return 0, 0, fmt.Errorf("git version: cannot read %q", strings.TrimSpace(string(out)))
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major, minor, nil
}

// ResolveCommit is the commit rev names; ok is false when there is none.
func ResolveCommit(ctx context.Context, dir, rev string) (Oid, bool, error) {
	out, err := Output(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if exitCode(err) == 1 {
		return Oid{}, false, nil
	}
	if err != nil {
		return Oid{}, false, err
	}
	o, err := ParseOid(strings.TrimSpace(string(out)))
	return o, err == nil, err
}

// HeadRef is the branch checked out in a worktree (refs/heads/x); "" when HEAD is detached.
func HeadRef(ctx context.Context, worktree string) (string, error) {
	out, err := Output(ctx, worktree, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return "", err
	}
	if s := strings.TrimSpace(string(out)); strings.HasPrefix(s, "refs/") {
		return s, nil
	}
	return "", nil
}

// Fetch updates the remote-tracking branch of one branch of remote (refs/remotes/<remote>/<branch>).
func Fetch(ctx context.Context, dir, remote, branch string) error {
	_, err := Write(ctx, dir, "fetch", "--no-tags", "--end-of-options", remote, branch)
	return err
}

// HasRemote reports whether the repo has a remote called name (a repo without origin has nothing to fetch).
func HasRemote(ctx context.Context, dir, name string) (bool, error) {
	_, err := Output(ctx, dir, "config", "--get", "remote."+name+".url")
	if exitCode(err) == 1 { // the key is not set
		return false, nil
	}
	return err == nil, err
}

// FetchRemote updates every remote-tracking branch of remote (git fetch <remote>); local branches and working directories stay.
func FetchRemote(ctx context.Context, dir, remote string) error {
	_, err := Write(ctx, dir, "fetch", "--end-of-options", remote)
	return err
}

// FetchInto fast-forwards the local branch from the same branch of remote without a checkout (git fetch <remote> <b>:<b>, with
// the full ref names so a tag of the same name is never taken). There is no "+": git itself refuses to move the branch
// anywhere but forward, and refuses a branch checked out in a worktree.
func FetchInto(ctx context.Context, dir, remote, branch string) error {
	ref := "refs/heads/" + branch
	_, err := Write(ctx, dir, "fetch", "--no-tags", "--end-of-options", remote, ref+":"+ref)
	return err
}

// Push sends the local branch to the same name on remote and makes that its upstream. No force: git refuses a push that is
// not a fast-forward, and the whitelist (pushPlain) refuses every form that could force or delete.
func Push(ctx context.Context, dir, remote, branch string) error {
	ref := "refs/heads/" + branch
	_, err := Write(ctx, dir, "push", "--set-upstream", "--end-of-options", remote, ref+":"+ref)
	return err
}

// FastForward moves the branch checked out in worktree to rev, only if that is a fast-forward (git merge --ff-only).
func FastForward(ctx context.Context, worktree, rev string) error {
	_, err := Write(ctx, worktree, "merge", "--ff-only", "--end-of-options", rev)
	return err
}

// DiscardFile puts one uncommitted file back to HEAD, in the index and the working directory: its staged and unstaged
// changes go, and a file HEAD lacks (a new file that was staged) is removed. oldPath, when set, is the other side of a
// staged rename and is put back too.
func DiscardFile(ctx context.Context, worktree, path, oldPath string) error {
	args := []string{"restore", "--source=HEAD", "--staged", "--worktree", "--", literalMagic + path}
	if oldPath != "" && oldPath != path {
		args = append(args, literalMagic+oldPath)
	}
	_, err := Write(ctx, worktree, args...)
	return err
}

// RemoveUntracked deletes one untracked file. git clean leaves tracked files alone, and without -d / -x no directory or
// ignored file either.
func RemoveUntracked(ctx context.Context, worktree, path string) error {
	_, err := Write(ctx, worktree, "clean", "-f", "--", literalMagic+path)
	return err
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 0
}

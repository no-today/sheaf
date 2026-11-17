package gitops

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"sheaf/internal/gitcli"
)

// Branch sync only fast-forwards. A worktree with uncommitted changes is skipped.

// How a batch row goes.
const (
	HowRef      = "ref"      // fast-forward a branch with no worktree (fetch <b>:<b>)
	HowWorktree = "worktree" // fast-forward in the worktree that has the branch checked out
	HowSkip     = "skip"
)

// Why a batch row is skipped.
const (
	SkipDirty    = "dirty"     // its worktree has uncommitted changes
	SkipMerging  = "merging"   // its worktree has an operation in progress
	SkipNoLocal  = "no_local"  // there is no local branch to update
	SkipNoRemote = "no_remote" // there is no remote branch to follow
	SkipUnpushed = "unpushed"  // the local branch has commits the remote branch lacks: it cannot be fast-forwarded
	SkipStopped  = "stopped"   // the user stopped the batch before this row began
)

// Outcomes of a batch row.
const (
	Skipped  = "skipped"
	UpToDate = "up_to_date"
	Failed   = "failed"
	Updated  = "updated" // fast-forwarded
	Fetched  = "fetched" // 获取全部: fetched
	NoRemote = "no_remote"
)

// parallelRepos bounds how many repos are written at once; the rows of one repo run one after another.
const parallelRepos = 4

// parallelFetch bounds the repos fetched at once: a fetch waits for the network, not for the disk.
const parallelFetch = 6

// splitRemoteRef cuts refs/remotes/origin/release/1 into origin and release/1.
func splitRemoteRef(ref string) (remote, branch string, ok bool) {
	rest, ok := strings.CutPrefix(ref, "refs/remotes/")
	if !ok {
		return "", "", false
	}
	remote, branch, ok = strings.Cut(rest, "/")
	return remote, branch, ok && branch != ""
}

// Busy reports whether a worktree is in the middle of a merge, rebase, cherry-pick or revert, from the files alone: HEAD is
// not where the user left it, so nothing here moves it.
func Busy(worktree string) bool {
	g := gitDir(worktree)
	for _, f := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"} {
		if exists(filepath.Join(g, f)) {
			return true
		}
	}
	return false
}

// Merging reports whether a worktree is in the middle of a merge (its MERGE_HEAD exists), from the files alone.
func Merging(worktree string) bool {
	return exists(filepath.Join(gitDir(worktree), "MERGE_HEAD"))
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// gitDir is a worktree's own git directory: .git, or where the .git file of a linked worktree points.
func gitDir(worktree string) string {
	dot := filepath.Join(worktree, ".git")
	b, err := os.ReadFile(dot)
	if err != nil {
		return dot // a directory (or missing)
	}
	p, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return dot
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(worktree, p)
	}
	return p
}

// ---- 获取全部 ----

// FetchTarget is a repo to fetch.
type FetchTarget struct {
	Repo string
	Dir  string
}

// FetchResult is what happened to one repo of 获取全部.
type FetchResult struct {
	Repo    string
	Outcome string // fetched | no_remote | failed | skipped (stopped before it began)
	Error   string
}

// Progress follows a batch row by row; either func may be nil, and both may be called from several goroutines. Start is
// asked before row i begins: false stops the batch there, leaving that row and the ones not yet begun skipped as
// stopped, while rows already running finish. Done reports row i once it has its result.
type Progress struct {
	Start func(i int) bool
	Done  func(i int)
}

func (p Progress) start(i int) bool { return p.Start == nil || p.Start(i) }

func (p Progress) done(i int) {
	if p.Done != nil {
		p.Done(i)
	}
}

// FetchAll runs git fetch origin in every repo, a few at a time. A repo without origin is not an error: it has nothing to fetch.
// A fetch changes no branch, so cancelling ctx may cut one short: it ends failed.
func FetchAll(ctx context.Context, repos []FetchTarget, p Progress) []FetchResult {
	out := make([]FetchResult, len(repos))
	sem := make(chan struct{}, parallelFetch)
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if !p.start(i) {
				out[i] = FetchResult{Repo: r.Repo, Outcome: Skipped} // stopped
			} else {
				out[i] = fetchOne(ctx, r)
			}
			p.done(i)
		}()
	}
	wg.Wait()
	return out
}

func fetchOne(ctx context.Context, r FetchTarget) FetchResult {
	res := FetchResult{Repo: r.Repo}
	has, err := gitcli.HasRemote(ctx, r.Dir, "origin")
	switch {
	case err != nil:
		res.Outcome, res.Error = Failed, err.Error()
	case !has:
		res.Outcome = NoRemote
	default:
		if err := gitcli.FetchRemote(ctx, r.Dir, "origin"); err != nil {
			res.Outcome, res.Error = Failed, err.Error()
		} else {
			res.Outcome = Fetched
		}
	}
	return res
}

// BatchResult is what happened to one row of a batch action.
type BatchResult struct {
	Repo     string
	Branch   string
	How      string
	Outcome  string // updated | up_to_date | skipped | failed
	Skip     string
	Commits  int // fast-forwarded over
	Worktree string
	Error    string
}

// ---- 更新本地分支 ----

// UpdateRow is one local branch of one repo and the remote branch it follows.
type UpdateRow struct {
	Repo     string
	Dir      string // the repo's main working directory (where git runs when nothing has the branch checked out)
	Branch   string // short name of the local branch
	Local    bool   // refs/heads/<Branch> exists
	Remote   string // full ref (refs/remotes/origin/<Branch>); "" = none
	Worktree string // the worktree that has the branch checked out; "" = none
	Dirty    int    // uncommitted files of Worktree
	Busy     bool   // Worktree is mid-merge, mid-rebase…
	Ahead    int    // commits the branch has that Remote lacks (the index, before any fetch)
	Behind   int
}

// DecideUpdate is how a branch is brought up to its remote branch; skip says why it is not.
func DecideUpdate(r UpdateRow) (how, skip string) {
	switch {
	case !r.Local:
		return HowSkip, SkipNoLocal
	case r.Remote == "":
		return HowSkip, SkipNoRemote
	case r.Worktree != "" && r.Busy:
		return HowSkip, SkipMerging
	case r.Worktree != "" && r.Dirty > 0:
		return HowSkip, SkipDirty
	case r.Ahead > 0:
		return HowSkip, SkipUnpushed
	case r.Worktree != "":
		return HowWorktree, ""
	}
	return HowRef, ""
}

// UpdateCommands is what a row runs, as shown before it does.
func UpdateCommands(r UpdateRow, how string) []string {
	remote, branch, _ := splitRemoteRef(r.Remote)
	switch how {
	case HowRef:
		return []string{"git fetch --no-tags " + remote + " " + branch + ":" + branch}
	case HowWorktree:
		return []string{"git fetch --no-tags " + remote + " " + branch, "git -C …/" + filepath.Base(r.Worktree) + " merge --ff-only " + strings.TrimPrefix(r.Remote, "refs/remotes/")}
	}
	return []string{}
}

// RunUpdate brings each branch up to its remote branch without changing what is checked out where, a few repos at a time (the
// rows of one repo run one after another). The branch of a worktree moves with merge --ff-only there; any other with fetch <b>:<b>.
func RunUpdate(ctx context.Context, rows []UpdateRow, p Progress) []BatchResult {
	out := make([]BatchResult, len(rows))
	byRepo := map[string][]int{}
	var dirs []string
	for i, r := range rows {
		if byRepo[r.Dir] == nil {
			dirs = append(dirs, r.Dir)
		}
		byRepo[r.Dir] = append(byRepo[r.Dir], i)
	}
	sem := make(chan struct{}, parallelRepos)
	var wg sync.WaitGroup
	for _, d := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			for _, i := range byRepo[d] {
				if p.start(i) {
					out[i] = runUpdate(ctx, rows[i])
				} else {
					r := rows[i]
					out[i] = BatchResult{Repo: r.Repo, Branch: r.Branch, Worktree: r.Worktree, How: HowSkip, Outcome: Skipped, Skip: SkipStopped}
				}
				p.done(i)
			}
		}()
	}
	wg.Wait()
	return out
}

func runUpdate(ctx context.Context, r UpdateRow) BatchResult {
	res := BatchResult{Repo: r.Repo, Branch: r.Branch, Worktree: r.Worktree}
	res.How, res.Skip = DecideUpdate(r)
	if res.How == HowSkip {
		res.Outcome = Skipped
		return res
	}
	remote, rbranch, _ := splitRemoteRef(r.Remote)
	before, ok, err := gitcli.ResolveCommit(ctx, r.Dir, "refs/heads/"+r.Branch)
	if err != nil || !ok {
		return failedBatch(res, cmp.Or(err, fmt.Errorf("branch %s not found", r.Branch)))
	}
	if res.How == HowRef {
		return updateRef(ctx, r, res, remote, rbranch, before)
	}
	return updateWorktree(ctx, r, res, remote, rbranch, before)
}

// updateRef: fetch <b>:<b>. A refusal because the branch has commits of its own is a skip, not a failure.
func updateRef(ctx context.Context, r UpdateRow, res BatchResult, remote, rbranch string, before gitcli.Oid) BatchResult {
	if err := gitcli.FetchInto(ctx, r.Dir, remote, rbranch); err != nil {
		if remoteOid, ok, rerr := gitcli.ResolveCommit(ctx, r.Dir, r.Remote); rerr == nil && ok {
			if n, cerr := gitcli.Count(ctx, r.Dir, remoteOid, before); cerr == nil && n > 0 {
				res.Outcome, res.Skip = Skipped, SkipUnpushed
				return res
			}
		}
		return failedBatch(res, err)
	}
	after, ok, err := gitcli.ResolveCommit(ctx, r.Dir, "refs/heads/"+r.Branch)
	if err != nil || !ok {
		return failedBatch(res, cmp.Or(err, fmt.Errorf("branch %s not found", r.Branch)))
	}
	if after == before {
		res.Outcome = UpToDate
		return res
	}
	n, err := gitcli.Count(ctx, r.Dir, before, after)
	if err != nil {
		return failedBatch(res, err)
	}
	res.Outcome, res.Commits = Updated, n
	return res
}

// updateWorktree: fetch the remote branch, then fast-forward in the worktree after checking again that it is still on the branch,
// idle and clean.
func updateWorktree(ctx context.Context, r UpdateRow, res BatchResult, remote, rbranch string, before gitcli.Oid) BatchResult {
	ref, err := gitcli.HeadRef(ctx, r.Worktree)
	if err != nil {
		return failedBatch(res, err)
	}
	if ref != "refs/heads/"+r.Branch {
		return failedBatch(res, fmt.Errorf("%s is no longer on %s", r.Worktree, r.Branch))
	}
	if Busy(r.Worktree) {
		res.Outcome, res.Skip = Skipped, SkipMerging
		return res
	}
	st, err := gitcli.Status(ctx, r.Worktree)
	if err != nil {
		return failedBatch(res, err)
	}
	if gitcli.DirtyCount(st) > 0 {
		res.Outcome, res.Skip = Skipped, SkipDirty
		return res
	}
	if err := gitcli.Fetch(ctx, r.Dir, remote, rbranch); err != nil {
		return failedBatch(res, fmt.Errorf("fetch %s/%s: %w", remote, rbranch, err))
	}
	remoteOid, ok, err := gitcli.ResolveCommit(ctx, r.Dir, r.Remote)
	if err != nil || !ok {
		return failedBatch(res, cmp.Or(err, fmt.Errorf("%s not found", r.Remote)))
	}
	ahead, err := gitcli.Count(ctx, r.Dir, remoteOid, before)
	if err != nil {
		return failedBatch(res, err)
	}
	if ahead > 0 {
		res.Outcome, res.Skip = Skipped, SkipUnpushed
		return res
	}
	behind, err := gitcli.Count(ctx, r.Dir, before, remoteOid)
	if err != nil {
		return failedBatch(res, err)
	}
	if behind == 0 {
		res.Outcome = UpToDate
		return res
	}
	if err := gitcli.FastForward(ctx, r.Worktree, r.Remote); err != nil {
		return failedBatch(res, err)
	}
	res.Outcome, res.Commits = Updated, behind
	return res
}

func failedBatch(res BatchResult, err error) BatchResult {
	res.Outcome, res.Error = Failed, err.Error()
	return res
}

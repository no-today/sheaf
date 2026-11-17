package session

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/gitops"
	"sheaf/internal/index"
)

// Fetch and sync plans are read-only. Sync checks the repository again before writing through gitops.

// writeContext lets a confirmed write finish when its page goes away, with a timeout for stalled git prompts.
func writeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
}

// afterWrite reads the index and the touched worktrees again, in the background.
func (s *Session) afterWrite(source string, worktrees []string) {
	ctx := s.git(s.ctx, source)
	s.Go(func() {
		if len(worktrees) > 0 {
			s.RefreshDirty(ctx, worktrees, true)
		}
		s.Refresh(ctx)
	})
}

// batchRepos are the tracked repos named, in the order of the scan; every one when names is empty.
func (s *Session) batchRepos(names []string) []discover.Repo {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []discover.Repo
	for _, r := range s.repos {
		if len(names) == 0 || slices.Contains(names, r.Name) {
			out = append(out, r)
		}
	}
	return out
}

// FetchAll runs git fetch origin in each repo named (every tracked repo when none is), then reads the index again. It
// is a task named subject; stopping it cuts the fetches short, which leaves every branch as it was.
func (s *Session) FetchAll(ctx context.Context, names []string, subject string) (FetchRun, error) {
	ctx, cancel := writeContext(s.git(ctx, "batchFetch"))
	defer cancel()
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var targets []gitops.FetchTarget
	var rows []string
	for _, r := range s.batchRepos(names) {
		targets = append(targets, gitops.FetchTarget{Repo: r.Name, Dir: r.Dir})
		rows = append(rows, r.Name)
	}
	task := s.startTask(TaskFetch, subject, rows, stop)
	s.writes.Lock()
	defer s.writes.Unlock()
	run := FetchRun{Rows: []FetchRow{}}
	results := gitops.FetchAll(ctx, targets, gitops.Progress{Start: task.begin})
	for i, r := range results {
		if r.Outcome == gitops.Failed && task.stopped() {
			r.Outcome = gitops.Skipped // cut short by the stop
		}
		run.Rows = append(run.Rows, FetchRow{Repo: r.Repo, Outcome: r.Outcome, Error: r.Error})
		state := TaskDone
		switch r.Outcome {
		case gitops.Fetched:
			run.Fetched++
		case gitops.NoRemote:
			run.NoRemote++
			state = TaskSkipped
		case gitops.Skipped:
			run.Stopped++
			state = TaskSkipped
		default:
			run.Failed++
			state = TaskFailed
		}
		task.row(i, state, cmp.Or(r.Error, r.Outcome))
	}
	task.end(nil, false)
	s.afterWrite("batchFetch", nil)
	return run, nil
}

// refCommit is the commit a full ref name points at in the index.
func refCommit(refs []index.Ref, full string) (int32, bool) {
	i := slices.IndexFunc(refs, func(r index.Ref) bool { return r.Name == full })
	if i < 0 {
		return 0, false
	}
	return refs[i].Commit, true
}

// indexedRepos takes the indexed versions of the named repos from the same snapshot.
func (s *Session) indexedRepos(repos []discover.Repo) (map[string]*index.Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eng == nil {
		return nil, errNotReady
	}
	ix := s.eng.Index()
	idx := make(map[string]*index.Repo, len(repos))
	for _, d := range repos {
		if id, ok := ix.RepoByName(d.Name); ok {
			idx[d.Name] = ix.Repos[id]
		}
	}
	return idx, nil
}

// aheadBehind of a local branch against the remote branch it follows (the index, before any fetch).
func aheadBehind(r *index.Repo, local, remote string) (ahead, behind int) {
	head, ok1 := refCommit(r.Refs, "refs/heads/"+local)
	base, ok2 := refCommit(r.Refs, remote)
	if !ok1 || !ok2 {
		return 0, 0
	}
	return r.AheadBehind(base, head)
}

// updateBranches are the names a plan looks for: origin/x is x, blanks and repeats go.
func updateBranches(favs []string) []string {
	var out []string
	for _, f := range favs {
		if b := strings.TrimSpace(strings.TrimPrefix(f, "origin/")); b != "" && !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	return out
}

// updateRows reads each branch of each repo that has it: the remote branch it follows, the worktree that has it checked out and
// how far it is from the remote branch. Rows are grouped by branch, in the order given.
func (s *Session) updateRows(ctx context.Context, names, branches []string) ([]gitops.UpdateRow, error) {
	repos := s.batchRepos(names)
	idx, err := s.indexedRepos(repos)
	if err != nil {
		return nil, err
	}
	var rows []gitops.UpdateRow
	for _, b := range branches {
		for _, d := range repos {
			r := idx[d.Name]
			if r == nil {
				continue
			}
			if _, ok := refCommit(r.Refs, "refs/heads/"+b); !ok {
				continue
			}
			row := gitops.UpdateRow{Repo: d.Name, Dir: d.Dir, Branch: b, Local: true}
			if _, ok := refCommit(r.Refs, "refs/remotes/origin/"+b); ok {
				row.Remote = "refs/remotes/origin/" + b
				row.Ahead, row.Behind = aheadBehind(r, b, row.Remote)
			}
			if i := slices.IndexFunc(d.Worktrees, func(w discover.Worktree) bool { return w.Branch == "refs/heads/"+b }); i >= 0 {
				row.Worktree = d.Worktrees[i].Path
				row.Busy = gitops.Busy(row.Worktree)
				l, err := s.dirtyList(ctx, d.Name, row.Worktree)
				if err != nil {
					return nil, err
				}
				row.Dirty = len(l.files)
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// BranchesPlan is what 更新本地分支 would do for each starred branch in each repo that has it.
func (s *Session) BranchesPlan(ctx context.Context, in BatchPlanInput) (BatchPlan, error) {
	ctx = s.git(ctx, "branchesPlan")
	out := BatchPlan{Rows: []BatchPlanRow{}, Branches: []string{}}
	rows, err := s.updateRows(ctx, in.Repos, updateBranches(in.Branches))
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		how, skip := gitops.DecideUpdate(r)
		out.Rows = append(out.Rows, BatchPlanRow{Repo: r.Repo, Branch: r.Branch, Remote: strings.TrimPrefix(r.Remote, "refs/remotes/"), How: how, Skip: skip,
			Worktree: r.Worktree, Dirty: r.Dirty, Ahead: r.Ahead, Behind: r.Behind, Commands: gitops.UpdateCommands(r, how)})
		if how != gitops.HowSkip {
			out.Run++
		}
		if !slices.Contains(out.Branches, r.Branch) {
			out.Branches = append(out.Branches, r.Branch)
		}
	}
	return out, nil
}

// BranchesRun brings each ticked branch up to its remote branch, as BranchesPlan says at the moment it runs.
func (s *Session) BranchesRun(ctx context.Context, in BatchRunInput) (BatchRun, error) {
	ctx, cancel := writeContext(s.git(ctx, "branchesRun"))
	defer cancel()
	s.writes.Lock()
	defer s.writes.Unlock()
	var names, branches []string
	for _, p := range in.Picks {
		names = append(names, p.Repo)
		branches = append(branches, p.Branch)
	}
	run := BatchRun{Rows: []BatchRow{}}
	if len(in.Picks) == 0 {
		return run, nil
	}
	var labels []string
	for _, p := range in.Picks {
		labels = append(labels, p.Repo+" · "+p.Branch)
	}
	task := s.startTask(TaskSync, in.Subject, labels, func() {}) // begin sees the stop before each row
	rows, err := s.updateRows(ctx, names, updateBranches(branches))
	if err != nil {
		task.end(err, false)
		return run, err
	}
	rows = slices.DeleteFunc(rows, func(r gitops.UpdateRow) bool {
		return !slices.Contains(in.Picks, BatchPick{Repo: r.Repo, Branch: r.Branch})
	})
	// The task's rows follow the picks; the batch's follow updateRows.
	at := func(i int) int { return slices.Index(in.Picks, BatchPick{Repo: rows[i].Repo, Branch: rows[i].Branch}) }
	var touched []string
	results := gitops.RunUpdate(ctx, rows, gitops.Progress{Start: func(i int) bool { return task.begin(at(i)) }})
	for i, r := range results {
		run.add(r)
		task.row(at(i), batchState(r.Outcome), cmp.Or(r.Error, r.Skip, r.Outcome))
		if r.Outcome == gitops.Updated && r.Worktree != "" {
			touched = append(touched, r.Worktree)
		}
	}
	task.end(nil, false)
	s.afterWrite("branchesRun", touched)
	return run, nil
}

// batchState is the task row state of a batch outcome.
func batchState(outcome string) string {
	switch outcome {
	case gitops.Failed:
		return TaskFailed
	case gitops.Skipped:
		return TaskSkipped
	}
	return TaskDone
}

func (run *BatchRun) add(r gitops.BatchResult) {
	run.Rows = append(run.Rows, BatchRow{Repo: r.Repo, Branch: r.Branch, How: r.How, Outcome: r.Outcome, Skip: r.Skip, Commits: r.Commits,
		Worktree: r.Worktree, Error: r.Error})
	switch r.Outcome {
	case gitops.Updated:
		run.Updated++
	case gitops.UpToDate:
		run.UpToDate++
	case gitops.Skipped:
		run.Skipped++
	default:
		run.Failed++
	}
}

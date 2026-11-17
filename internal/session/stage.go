package session

import (
	"slices"

	"sheaf/internal/index"
)

// The stage of a row of the Tag view: how far the row's own commits have got.
const (
	StageLanded   = "landed"
	StageUAT      = "merge:1"
	StagePending  = "merge:0"
	StageDev      = "dev"
	StageUnknown  = "unknown"
	StageArchived = "archived"
)

func (s *Session) ownCommits(a *aggRepo) []int32 {
	var own []int32
	for _, commit := range a.commits {
		if len(a.r.Parents(commit)) < 2 {
			own = append(own, commit)
		} else if change, ok := s.remerge.get(a.remergeKey(commit)); ok && change.Own {
			own = append(own, commit)
		}
	}
	return own
}

func (s *Session) planStage(a *aggRepo) {
	v := &a.view
	a.own, a.noOwn, a.jobs = s.ownCommits(a), false, nil
	if v.Uncommitted > 0 {
		v.Stage = StageDev
		return
	}
	if len(a.own) == 0 {
		if a.deleted {
			v.Stage = StageUnknown
		} else {
			a.noOwn, v.Stage = true, StageLanded
		}
		return
	}
	flow := s.MergeFlow()
	v.Stage = MergeStage(len(flow) - 1)
	next := StageLanded
	baselineOnly := s.repoBaselineOnly(a.r.Name)
	for step := len(flow) - 1; step >= 0; step-- {
		if baselineOnly && step < len(flow)-1 {
			continue
		}
		branch := flow[step].Branch
		if branch == "" {
			branch = a.view.Main
		}
		line := releaseTip(a.r, branch)
		if line < 0 {
			if step == len(flow)-1 {
				next = MergeStage(step)
			}
			continue
		}
		v.Stage = MergeStage(step)
		ancestors := s.reachOf(a.r, []int32{line})
		patch := s.patchCover(a, line, ancestors)
		if slices.ContainsFunc(a.own, func(commit int32) bool { return !ancestors.Has(commit) && !patch(commit) }) {
			next = v.Stage
			continue
		}
		v.Stage = next
		return
	}
}

// patchCover says which of the row's own commits that line (a commit) does not reach by ancestry it has anyway: as a
// commit with the same patch-id, by cherry-pick or squash of one commit. The answer is the cached one; without it the
// comparison is queued in a.jobs and nothing is covered.
func (s *Session) patchCover(a *aggRepo, line int32, anc index.Set) func(int32) bool {
	none := func(int32) bool { return false }
	if line < 0 {
		return none
	}
	r := a.r
	job := patchJob{r: r, line: line, paths: a.paths}
	for _, c := range a.own {
		if anc.Has(c) || len(r.Parents(c)) > 1 {
			continue
		}
		job.commits = append(job.commits, c)
	}
	if len(job.commits) == 0 {
		return none
	}
	if len(s.pending(job)) > 0 {
		a.jobs = append(a.jobs, job)
	}
	return func(c int32) bool {
		covered, _ := s.cover.get(s.coverKey(r, line, c))
		return covered
	}
}

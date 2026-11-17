package session

import (
	"context"
	"fmt"
	"slices"

	"sheaf/internal/gitcli"
	"sheaf/internal/gitview"
)

const (
	DiffBase     = "base"
	DiffWorktree = "worktree"
	DiffStage    = "stage"
)

func (s *Session) diffPlan(ctx context.Context, in TagAggInput) (*aggPlan, error) {
	switch in.DiffMode {
	case "", DiffBase, DiffWorktree:
	case DiffStage:
		if _, err := s.TagAggRemerge(ctx, in); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown diff mode %q", in.DiffMode)
	}
	p, err := s.aggPlan(in)
	if err != nil {
		return nil, err
	}
	flow := s.MergeFlow()
	if in.DiffMode == DiffStage && (in.DiffStage < 0 || in.DiffStage >= len(flow)) {
		return nil, fmt.Errorf("diff stage %d no longer exists", in.DiffStage+1)
	}
	for index := range p.repos {
		a := &p.repos[index]
		a.mode = in.DiffMode
		if a.deleted {
			a.commits, a.scope, a.files, a.hands = nil, nil, nil, nil
			a.base, a.branch, a.view.Worktree = -1, -1, ""
			continue
		}
		switch in.DiffMode {
		case DiffStage:
			if s.repoBaselineOnly(a.r.Name) && in.DiffStage < len(flow)-1 {
				a.commits, a.scope, a.files, a.hands = nil, nil, nil, nil
				a.base, a.branch, a.view.Worktree = -1, -1, ""
				continue
			}
			if err := s.stageDiff(ctx, a, flow[in.DiffStage]); err != nil {
				return nil, err
			}
		}
	}
	return p, nil
}

func (s *Session) stageDiff(ctx context.Context, a *aggRepo, step MergeStep) error {
	target := step.Branch
	if target == "" {
		target = a.view.Main
	}
	line := releaseTip(a.r, target)
	if line < 0 {
		a.commits, a.scope, a.files, a.hands = nil, nil, nil, nil
		a.base, a.branch, a.view.Worktree = -1, -1, ""
		return nil
	}
	a.own = s.ownCommits(a)
	if a.branch >= 0 {
		onBranch := s.reachOf(a.r, []int32{a.branch})
		a.own = slices.DeleteFunc(a.own, func(commit int32) bool { return !onBranch.Has(commit) })
	}
	ancestors := s.reachOf(a.r, []int32{line})
	job := patchJob{r: a.r, line: line, paths: a.paths}
	for _, commit := range a.own {
		if !ancestors.Has(commit) && len(a.r.Parents(commit)) < 2 {
			job.commits = append(job.commits, commit)
		}
	}
	s.runPatchJob(ctx, job)
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(s.pending(job)) > 0 {
		return fmt.Errorf("%s: could not check patches against %q", a.r.Name, target)
	}
	a.commits = slices.DeleteFunc(slices.Clone(a.own), func(commit int32) bool {
		covered, _ := s.cover.get(s.coverKey(a.r, line, commit))
		return ancestors.Has(commit) || covered
	})
	if a.branch < 0 {
		var oids []gitcli.Oid
		for _, commit := range a.commits {
			if len(a.r.Parents(commit)) < 2 {
				oids = append(oids, a.r.Oid(commit))
			}
		}
		ids, err := s.patchIDs(ctx, a.r, oids)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		a.commits = slices.DeleteFunc(a.commits, func(commit int32) bool {
			id := ids[a.r.Oid(commit)]
			if id == "" {
				return false
			}
			duplicate := seen[id]
			seen[id] = true
			return duplicate
		})
	}
	a.scope = a.commits
	a.files = slices.DeleteFunc(slices.Clone(a.commits), func(commit int32) bool { return len(a.r.Parents(commit)) > 1 })
	a.hands = nil
	for _, commit := range a.commits {
		if change, ok := s.remerge.get(a.remergeKey(commit)); ok && len(change.Files) > 0 {
			if a.hands == nil {
				a.hands = map[int32][]string{}
			}
			a.hands[commit] = change.Files
		}
	}
	a.base, a.branch = -1, -1
	return nil
}

func (s *Session) worktreeDiffFiles(ctx context.Context, a *aggRepo) ([]AggFile, error) {
	if a.view.Worktree == "" {
		return []AggFile{}, nil
	}
	list, err := s.dirtyList(ctx, a.r.Name, a.view.Worktree)
	if err != nil {
		return nil, err
	}
	files := slices.Clone(list.files)
	for index := range files {
		files[index].Key = a.view.Key
	}
	return files, nil
}

func (s *Session) worktreeDiffPatch(ctx context.Context, a *aggRepo, path, oldPath string, untracked bool) (AggPatch, error) {
	if a.view.Worktree == "" {
		return AggPatch{Mode: "range"}, nil
	}
	area := gitcli.Head
	if untracked {
		area = gitcli.Untracked
	}
	patch, err := gitview.UncommittedPatch(ctx, a.view.Worktree, path, oldPath, area)
	if err != nil {
		return AggPatch{}, err
	}
	return AggPatch{Mode: "range", Parts: []AggPart{{Patch: patchOf(patch, path)}}}, nil
}

func patchStat(path string, patch Patch) FileStat {
	stat := FileStat{Path: path, Binary: patch.Binary}
	for _, hunk := range patch.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case "+":
				stat.Add++
			case "-":
				stat.Del++
			}
		}
	}
	return stat
}

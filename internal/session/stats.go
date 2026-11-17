package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"sheaf/internal/gitcli"
	"sheaf/internal/gitview"
)

// FileStat is how many lines the merged diff of one file of the Tag view adds and deletes (TagAggStats).
type FileStat struct {
	Path   string `json:"path"`
	Add    int    `json:"add"`
	Del    int    `json:"del"`
	Binary bool   `json:"binary"`
}

// statsCache keeps the counts of each row, sharing an in-flight read between the file list and line counts.
type statsCache struct {
	mu sync.Mutex
	m  map[statsKey]*statsRead
}

type statsRead struct {
	done  chan struct{}
	files []FileStat
	err   error
}

type statsKey struct {
	dir, from, to string
	epoch         uint64 // the working directory's invalidations (dirtyTracker.epochOf); 0 without one
}

// load reads k once for every caller that asks while the read is in flight.
func (c *statsCache) load(ctx context.Context, k statsKey, read func() ([]FileStat, error)) ([]FileStat, error) {
	c.mu.Lock()
	if v, ok := c.m[k]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-v.done:
			// The read was another caller's and its context ended: read again for this one.
			if v.err != nil && ctx.Err() == nil && (errors.Is(v.err, context.Canceled) || errors.Is(v.err, context.DeadlineExceeded)) {
				return c.load(ctx, k, read)
			}
			return v.files, v.err
		}
	}
	v := &statsRead{done: make(chan struct{})}
	if c.m == nil {
		c.m = map[statsKey]*statsRead{}
	}
	c.m[k] = v
	c.mu.Unlock()

	v.files, v.err = read()
	c.mu.Lock()
	if v.err != nil {
		delete(c.m, k)
	}
	close(v.done)
	c.mu.Unlock()
	return v.files, v.err
}

// TagAggStats counts a row's lines. Base and stage modes count committed changes only; worktree mode counts HEAD to the
// working directory, including untracked files. Edited merge parts are counted separately only in stage mode.
func (s *Session) TagAggStats(ctx context.Context, in TagAggInput, row string) ([]FileStat, error) {
	ctx = s.git(ctx, "aggStats")
	p, err := s.diffPlan(ctx, in)
	if err != nil {
		return nil, err
	}
	a, ok := p.row(row)
	if !ok {
		return nil, fmt.Errorf("tag %s has no row %q", p.tag.Name, row)
	}
	stats, err := s.rowStats(ctx, &a)
	if err != nil || in.DiffMode != DiffBase {
		return stats, err
	}
	files := withResolved(a.r.Name, a.view.Key, committedFiles(p.eng.Index(), a.r, a.view.Key, a.files, true), a.hands)
	paths := make(map[string]bool, len(files))
	for _, file := range files {
		paths[file.Path] = true
	}
	return slices.DeleteFunc(slices.Clone(stats), func(stat FileStat) bool { return !paths[stat.Path] }), nil
}

func (s *Session) rowStats(ctx context.Context, a *aggRepo) ([]FileStat, error) {
	var key statsKey
	var read func() ([]FileStat, error)
	switch {
	case a.mode == DiffWorktree:
		if a.view.Worktree == "" {
			return []FileStat{}, nil
		}
		wt := a.view.Worktree
		head := ""
		if a.branch >= 0 {
			head = a.r.Oid(a.branch).String()
		}
		key = statsKey{dir: wt, from: head, to: "head-worktree", epoch: s.dirty.epochOf(wt)}
		read = func() ([]FileStat, error) {
			files, err := s.worktreeDiffFiles(ctx, a)
			if err != nil {
				return nil, err
			}
			stats, err := statsOf(gitview.WorktreeNumstat(ctx, wt))
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if file.Untracked {
					patch, err := s.worktreeDiffPatch(ctx, a, file.Path, file.OldPath, true)
					if err != nil {
						return nil, err
					}
					stats = append(stats, patchStat(file.Path, patch.Parts[0].Patch))
				}
			}
			return stats, nil
		}
	case a.base >= 0 && a.branch >= 0 && a.view.Worktree != "" && a.mode != DiffBase && a.mode != DiffStage:
		wt := a.view.Worktree
		key = statsKey{dir: wt, from: a.r.Oid(a.base).String(), to: "worktree", epoch: s.dirty.epochOf(wt)}
		read = func() ([]FileStat, error) { return statsOf(gitview.MergedNumstat(ctx, a.r, a.base, wt)) }
	case a.base >= 0 && a.branch >= 0:
		key = statsKey{dir: a.r.Dir, from: a.r.Oid(a.base).String(), to: a.r.Oid(a.branch).String()}
		read = func() ([]FileStat, error) { return statsOf(gitview.CompareNumstat(ctx, a.r, a.base, a.branch)) }
	default:
		var ids strings.Builder
		for _, c := range a.files {
			ids.WriteString(a.r.Oid(c).String())
		}
		key = statsKey{dir: a.r.Dir, from: "commits", to: ids.String()}
		commits := a.files
		read = func() ([]FileStat, error) { return statsOf(gitview.CommitsNumstat(ctx, a.r, commits)) }
	}
	l, err := s.stats.load(ctx, key, read)
	if err != nil {
		return nil, fmt.Errorf("line counts of %s: %w", a.view.Repo, err)
	}
	if a.mode == DiffStage {
		l = slices.Clone(l)
		for commit, paths := range a.hands {
			for _, path := range paths {
				part, err := a.resolvedPart(ctx, commit, path)
				if err != nil {
					return nil, err
				}
				stat := patchStat(path, part.Patch)
				index := slices.IndexFunc(l, func(existing FileStat) bool { return existing.Path == path })
				if index < 0 {
					l = append(l, stat)
				} else {
					l[index].Add += stat.Add
					l[index].Del += stat.Del
					l[index].Binary = l[index].Binary || stat.Binary
				}
			}
		}
	}
	return l, nil
}

// statsOf sums the counts by path: a file several commits changed comes once.
func statsOf(ns []gitcli.NumStat, err error) ([]FileStat, error) {
	if err != nil {
		return nil, err
	}
	at := map[string]int{}
	out := []FileStat{}
	for _, n := range ns {
		i, ok := at[n.Path]
		if !ok {
			i = len(out)
			at[n.Path] = i
			out = append(out, FileStat{Path: n.Path})
		}
		out[i].Add += n.Add
		out[i].Del += n.Del
		out[i].Binary = out[i].Binary || n.Binary
	}
	return out, nil
}

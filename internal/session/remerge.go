package session

import (
	"context"
	"slices"
	"strings"
	"sync"

	"sheaf/internal/gitcli"
	"sheaf/internal/gitview"
)

// remergeParallel bounds the repos TagAggRemerge classifies at once.
const remergeParallel = 4

// remergeCache keeps a merge's resolved files and whether it added work beyond its parents. A commit never changes, so
// an entry never goes stale.
type remergeCache struct {
	mu  sync.Mutex
	m   map[remergeKey]gitcli.MergeChanges
	gen uint64 // counts the entries put: the stages computed before them may have changed
}

func (c *remergeCache) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

type remergeKey struct {
	dir string
	oid gitcli.Oid
}

func (c *remergeCache) get(k remergeKey) (gitcli.MergeChanges, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.m[k]
	return l, ok
}

func (c *remergeCache) put(k remergeKey, l gitcli.MergeChanges) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[remergeKey]gitcli.MergeChanges{}
	}
	c.m[k] = l
	c.gen++
}

func (a *aggRepo) remergeKey(c int32) remergeKey { return remergeKey{a.r.Dir, a.r.Oid(c)} }

// planRemerge sorts the merge commits the file list leaves out by what the cache knows: edited ones into a.hands, the
// unclassified ones into a.unknown for TagAggRemerge. Clean ones are dropped.
func (s *Session) planRemerge(a *aggRepo) {
	a.files = slices.DeleteFunc(slices.Clone(a.files), func(c int32) bool { return len(a.r.Parents(c)) > 1 })
	for _, c := range a.commits {
		if len(a.r.Parents(c)) < 2 {
			continue
		}
		change, ok := s.remerge.get(a.remergeKey(c))
		switch {
		case !ok:
			a.unknown = append(a.unknown, c)
		case len(change.Files) > 0 && slices.Contains(a.scope, c):
			if a.hands == nil {
				a.hands = map[int32][]string{}
			}
			a.hands[c] = change.Files
		}
	}
}

// handPaths are the files the edited merges of the row changed, in no particular order, without repeats.
func (a *aggRepo) handPaths() []string {
	var out []string
	for _, files := range a.hands {
		for _, f := range files {
			if !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}

// TagAggRemerge classifies the merge commits TagAgg could not yet (clean, or edited while merging) and says whether any
// turned out edited, which changes the file list, the commits tab and the file counts: the caller reads them again. Run it
// after TagAgg so the first screen does not wait for git. Repos git fails on (older than 2.36) stay unclassified.
func (s *Session) TagAggRemerge(ctx context.Context, in TagAggInput) (bool, error) {
	p, err := s.aggPlan(in)
	if err != nil {
		return false, err
	}
	ctx = s.git(ctx, "remerge")
	var todo []aggRepo
	for _, a := range p.repos {
		if len(a.unknown) > 0 {
			todo = append(todo, a)
		}
	}
	var mu sync.Mutex
	changed := false
	err = parallel(ctx, len(todo), remergeParallel, func(i int) {
		a := todo[i]
		got, err := gitview.RemergeFiles(ctx, a.r, a.unknown)
		if err != nil {
			return
		}
		for c, change := range got {
			s.remerge.put(a.remergeKey(c), change)
			if len(change.Files) > 0 {
				mu.Lock()
				changed = true
				mu.Unlock()
			}
		}
		if len(got) > 0 {
			s.scheduleDerived()
		}
	})
	if err == nil {
		err = ctx.Err()
	}
	return changed, err
}

// withResolved adds the files of the edited merges to files (by path): a file already listed counts the merge as one more
// commit, a file only they changed is new, status M (git does not say more about a merge's own edits).
func withResolved(repo, key string, files []AggFile, hands map[int32][]string) []AggFile {
	if len(hands) == 0 {
		return files
	}
	at := map[string]int{}
	out := slices.Clone(files)
	for i, f := range out {
		at[f.Path] = i
	}
	for _, list := range hands {
		for _, path := range list {
			if i, ok := at[path]; ok {
				out[i].Commits++
				continue
			}
			at[path] = len(out)
			out = append(out, AggFile{Repo: repo, Key: key, Path: path, Status: "M", Commits: 1})
		}
	}
	slices.SortFunc(out, func(x, y AggFile) int { return strings.Compare(x.Path, y.Path) })
	return out
}

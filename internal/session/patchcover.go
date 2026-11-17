package session

import (
	"context"
	"slices"
	"sync"

	"sheaf/internal/gitcli"
	"sheaf/internal/gitview"
	"sheaf/internal/index"
)

// patchSlack is how much older than the oldest commit asked about a patch-equivalent copy is still looked for: clocks
// of the people who picked it differ, and git stops walking history at the first commit older than the limit.
const patchSlack = 24 * 3600

// coverCache keeps what patch comparison found out. A commit never changes, so its patch-id is kept for good; whether a
// line has a patch-equivalent of a commit is kept per line tip, which a new commit on the line replaces.
type coverCache struct {
	mu    sync.Mutex
	ids   map[idKey]string // "" = the commit changes nothing
	cover map[coverKey]bool
	used  map[coverKey]bool // protects answers observed in this session from age-based pruning
	gen   uint64            // counts the answers put
}

type idKey struct {
	dir string
	oid gitcli.Oid
}

type coverKey struct {
	dir        string
	line, from gitcli.Oid
}

func (c *coverCache) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *coverCache) get(k coverKey) (covered, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	covered, ok = c.cover[k]
	if ok {
		c.use(k)
	}
	return
}

// use is called with c.mu held.
func (c *coverCache) use(k coverKey) {
	if c.used == nil {
		c.used = map[coverKey]bool{}
	}
	c.used[k] = true
}

// patchJob asks whether the line has a patch-equivalent of each of commits.
type patchJob struct {
	r       *index.Repo
	line    int32
	commits []int32
	paths   []string // the index's file names
}

func (j patchJob) key() jobKey { return jobKey{j.r.Dir, j.r.Oid(j.line)} }

type jobKey struct {
	dir  string
	line gitcli.Oid
}

func (s *Session) coverKey(r *index.Repo, line, c int32) coverKey {
	return coverKey{r.Dir, r.Oid(line), r.Oid(c)}
}

// pending is the commits of j whose answer is not known yet.
func (s *Session) pending(j patchJob) []int32 {
	return slices.DeleteFunc(slices.Clone(j.commits), func(c int32) bool {
		_, ok := s.cover.get(s.coverKey(j.r, j.line, c))
		return ok
	})
}

// patchIDs gives the patch-id of each of oids, asking git for the ones not known yet.
func (s *Session) patchIDs(ctx context.Context, r *index.Repo, oids []gitcli.Oid) (map[gitcli.Oid]string, error) {
	out := make(map[gitcli.Oid]string, len(oids))
	var lack []gitcli.Oid
	s.cover.mu.Lock()
	for _, o := range oids {
		if id, ok := s.cover.ids[idKey{r.Dir, o}]; ok {
			out[o] = id
		} else {
			lack = append(lack, o)
		}
	}
	s.cover.mu.Unlock()
	if len(lack) == 0 {
		return out, nil
	}
	got, err := gitview.PatchIDs(ctx, r, lack)
	if err != nil {
		return nil, err
	}
	defer s.scheduleDerived()
	s.cover.mu.Lock()
	defer s.cover.mu.Unlock()
	if s.cover.ids == nil {
		s.cover.ids = map[idKey]string{}
	}
	for _, o := range lack {
		s.cover.ids[idKey{r.Dir, o}] = got[o]
		out[o] = got[o]
	}
	return out, nil
}

// runPatchJob answers j: a commit is covered when a non-merge commit of the line has its patch-id. Reports whether
// any answer came in.
func (s *Session) runPatchJob(ctx context.Context, j patchJob) bool {
	r := j.r
	todo := s.pending(j)
	if len(todo) == 0 {
		return false
	}
	oids := make([]gitcli.Oid, len(todo))
	for i, c := range todo {
		oids[i] = r.Oid(c)
	}
	own, err := s.patchIDs(ctx, r, oids)
	if err != nil {
		return false
	}
	// The copy touched the same files, and no earlier than the original.
	since := int64(1<<62 - 1)
	var paths []string
	seen := map[uint32]bool{}
	for _, c := range todo {
		if own[r.Oid(c)] == "" {
			continue
		}
		since = min(since, r.Time[c])
		files, _ := r.Files(c)
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				paths = append(paths, j.paths[f])
			}
		}
	}
	onLine := map[string]bool{}
	if len(paths) > 0 {
		cand, err := gitview.PatchCandidates(ctx, r, j.line, since-patchSlack, paths)
		if err != nil {
			return false
		}
		ids, err := s.patchIDs(ctx, r, cand)
		if err != nil {
			return false
		}
		for _, id := range ids {
			if id != "" {
				onLine[id] = true
			}
		}
	}
	defer s.scheduleDerived()
	s.cover.mu.Lock()
	defer s.cover.mu.Unlock()
	if s.cover.cover == nil {
		s.cover.cover = map[coverKey]bool{}
	}
	for _, c := range todo {
		id := own[r.Oid(c)]
		key := s.coverKey(r, j.line, c)
		s.cover.cover[key] = id == "" || onLine[id] // nothing to land is landed
		s.cover.use(key)
	}
	s.cover.gen++
	return true
}

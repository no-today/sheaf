package session

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"sheaf/internal/index"
)

// reachCache keeps the commits reachable from a set of heads. A commit never changes, so the set of a head list never
// goes stale; a moved branch is another key. Many rows ask for the same few lines (the main branch, the release
// branch, everything on origin).
type reachCache struct {
	mu sync.Mutex
	m  map[reachKey]index.Set
}

type reachKey struct {
	r     *index.Repo
	heads string
}

// reachLimit bounds the cache: past it, everything is dropped and rebuilt on demand.
const reachLimit = 4096

func (s *Session) reachOf(r *index.Repo, heads []int32) index.Set {
	heads = slices.Clone(heads)
	slices.Sort(heads)
	heads = slices.Compact(heads)
	k := reachKey{r, fmt.Sprint(heads)}
	c := &s.reach
	c.mu.Lock()
	set, ok := c.m[k]
	c.mu.Unlock()
	if ok {
		return set
	}
	set = r.RangeAll(heads, -1)
	c.mu.Lock()
	if c.m == nil || len(c.m) >= reachLimit {
		c.m = map[reachKey]index.Set{}
	}
	c.m[k] = set
	c.mu.Unlock()
	return set
}

// originHeads are the tips of every branch on origin.
func originHeads(r *index.Repo) []int32 {
	var heads []int32
	for _, ref := range r.Refs {
		if strings.HasPrefix(ref.Name, "refs/remotes/origin/") && !strings.HasSuffix(ref.Name, "/HEAD") {
			heads = append(heads, ref.Commit)
		}
	}
	return heads
}

// baseHeads are the base and the remote branches of the same name: the main line as far as it is known.
func baseHeads(r *index.Repo, base int32, baseName string) []int32 {
	heads := []int32{base}
	baseBranch := branchName(r, baseName)
	for _, ref := range r.Refs {
		if b, ok := remoteBranch(ref.Name); ok && b == baseBranch {
			heads = append(heads, ref.Commit)
		}
	}
	return heads
}

package index

import (
	"context"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strings"
	"sync"

	"sheaf/internal/discover"
	"sheaf/internal/gitcli"
)

// Progress reports how many repos are done during Build or Update.
type Progress func(done, total int)

// RepoError names one repo whose read failed.
type RepoError struct {
	Repo string
	Err  error
}

// Delta says what an Update changed.
type Delta struct {
	Changed []string    // repo names re-read (new, changed or removed)
	Added   int         // commits added
	Failed  []RepoError // repos whose read failed: their old history is kept, or they are left out on a first build
}

func Build(ctx context.Context, repos []discover.Repo, progress Progress) (*Index, error) {
	ix, _, err := Update(ctx, nil, repos, progress)
	return ix, err
}

// Update returns a new Index for repos, reading only commits that are new
// since old. Unchanged repos are shared with old. old may be nil.
func Update(ctx context.Context, old *Index, repos []discover.Repo, progress Progress) (*Index, Delta, error) {
	return update(ctx, old, repos, nil, progress)
}

// UpdateSome is Update that re-reads only the repos named in only (whose refs
// a file watch saw move); the others are shared with old as they are.
func UpdateSome(ctx context.Context, old *Index, repos []discover.Repo, only []string) (*Index, Delta, error) {
	set := make(map[string]bool, len(only))
	for _, n := range only {
		set[n] = true
	}
	return update(ctx, old, repos, set, nil)
}

func update(ctx context.Context, old *Index, repos []discover.Repo, only map[string]bool, progress Progress) (*Index, Delta, error) {
	b := newBuilder(old)
	prev := map[string]*Repo{}
	if old != nil {
		for _, r := range old.Repos {
			prev[r.GitDir] = r
		}
	}
	out := make([]*Repo, len(repos))
	added := make([]int, len(repos))
	changed := make([]bool, len(repos))
	errs := make([]error, len(repos))
	var done int
	var mu sync.Mutex
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for k, dr := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if p := prev[dr.GitDir]; only != nil && !only[dr.Name] && p != nil {
				out[k] = p
			} else {
				out[k], added[k], changed[k], errs[k] = b.repo(ctx, p, dr)
			}
			mu.Lock()
			done++
			if progress != nil {
				progress(done, len(repos))
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, Delta{}, err
	}
	ix := &Index{Strs: *b.strs, Paths: *b.paths}
	var d Delta
	for k, r := range out {
		if errs[k] != nil {
			// one broken repo must not hide the others: keep its old history if any
			d.Failed = append(d.Failed, RepoError{Repo: repos[k].Name, Err: errs[k]})
			if p := prev[repos[k].GitDir]; p != nil {
				r = p
			} else {
				continue
			}
		}
		ix.Repos = append(ix.Repos, r)
		delete(prev, r.GitDir)
		if changed[k] {
			d.Changed = append(d.Changed, r.Name)
		}
		d.Added += added[k]
	}
	if len(prev) > 0 {
		d.Changed = append(d.Changed, "") // a repo went away
	}
	ix.finish()
	return ix, d, nil
}

// tips: every ref plus each worktree HEAD (a detached HEAD has no ref).
func tips(ctx context.Context, dr discover.Repo) (map[string]gitcli.Oid, []gitcli.Ref, error) {
	refs, err := gitcli.Refs(ctx, dr.Dir)
	if err != nil {
		return nil, nil, err
	}
	t := make(map[string]gitcli.Oid, len(refs)+len(dr.Worktrees))
	for _, r := range refs {
		t[r.Name] = r.Oid
	}
	for _, w := range dr.Worktrees {
		if !w.Head.IsZero() {
			t["HEAD:"+w.Path] = w.Head
		}
	}
	return t, refs, nil
}

func (b *builder) repo(ctx context.Context, old *Repo, dr discover.Repo) (*Repo, int, bool, error) {
	now, refs, err := tips(ctx, dr)
	if err != nil {
		return nil, 0, false, err
	}
	if old != nil && maps.Equal(old.Tips, now) {
		if old.Name == dr.Name && old.Dir == dr.Dir {
			return old, 0, false, nil
		}
		cp := old.shallowCopy()
		cp.Name, cp.Dir = dr.Name, dr.Dir
		return cp, 0, false, nil
	}
	var r *Repo
	var exclude []gitcli.Oid
	if old != nil {
		r = old.forAppend()
		for _, o := range old.Tips {
			exclude = append(exclude, o)
		}
	} else {
		r = &Repo{ParOff: []uint32{0}, FileOff: []uint32{0}}
	}
	r.Name, r.Dir, r.GitDir = dr.Name, dr.Dir, dr.GitDir
	before := r.N()
	if err := b.read(ctx, r, unique(now), exclude); err != nil {
		if old == nil {
			return nil, 0, false, err
		}
		// e.g. an old tip was garbage-collected: read the repo from scratch
		r = &Repo{Name: dr.Name, Dir: dr.Dir, GitDir: dr.GitDir, ParOff: []uint32{0}, FileOff: []uint32{0}}
		before = 0
		if err := b.read(ctx, r, unique(now), nil); err != nil {
			return nil, 0, false, err
		}
	}
	r.Tips = now
	r.Refs = r.Refs[:0]
	for _, ref := range refs {
		if i := r.Find(ref.Oid); i >= 0 {
			r.Refs = append(r.Refs, Ref{Name: ref.Name, Commit: i})
		}
	}
	slices.SortFunc(r.Refs, func(a, b Ref) int { return strings.Compare(a.Name, b.Name) })
	r.computeLive()
	return r, r.N() - before, true, nil
}

func (b *builder) read(ctx context.Context, r *Repo, include, exclude []gitcli.Oid) error {
	start := r.N()
	var pend []gitcli.Oid
	pendOff := []int{0}
	err := gitcli.Log(ctx, r.Dir, include, exclude, func(c *gitcli.RawCommit) error {
		r.Hash = append(r.Hash, c.Oid[:]...)
		r.Time = append(r.Time, c.Time)
		r.Author = append(r.Author, b.str(c.Author))
		r.Email = append(r.Email, b.str(strings.ToLower(c.Email)))
		r.Subject = append(r.Subject, c.Subject)
		for _, f := range c.Files {
			r.File = append(r.File, b.path(f.Path))
			r.Stat = append(r.Stat, f.Status)
		}
		r.FileOff = append(r.FileOff, uint32(len(r.File)))
		pend = append(pend, c.Parents...)
		pendOff = append(pendOff, len(pend))
		return nil
	})
	if err != nil {
		return fmt.Errorf("index %s: %w", r.Name, err)
	}
	// parents may be read after their children, so resolve once all are in
	r.oidOnce = sync.Once{}
	r.byOid = nil
	for k := 0; k < r.N()-start; k++ {
		for _, p := range pend[pendOff[k]:pendOff[k+1]] {
			if i := r.Find(p); i >= 0 { // missing parents (shallow clones) are dropped
				r.Par = append(r.Par, i)
			}
		}
		r.ParOff = append(r.ParOff, uint32(len(r.Par)))
	}
	return nil
}

func unique(t map[string]gitcli.Oid) []gitcli.Oid {
	seen := map[gitcli.Oid]bool{}
	var out []gitcli.Oid
	for _, o := range t {
		if !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	return out
}

// forAppend copies the columns so appending never touches old's arrays.
func (r *Repo) forAppend() *Repo {
	return &Repo{
		Hash: slices.Clip(r.Hash), Time: slices.Clip(r.Time), Author: slices.Clip(r.Author), Email: slices.Clip(r.Email),
		Subject: slices.Clip(r.Subject), ParOff: slices.Clip(r.ParOff), Par: slices.Clip(r.Par),
		FileOff: slices.Clip(r.FileOff), File: slices.Clip(r.File), Stat: slices.Clip(r.Stat),
	}
}

func (r *Repo) shallowCopy() *Repo {
	return &Repo{Name: r.Name, Dir: r.Dir, GitDir: r.GitDir, Hash: r.Hash, Time: r.Time, Author: r.Author, Email: r.Email,
		Subject: r.Subject, ParOff: r.ParOff, Par: r.Par, FileOff: r.FileOff, File: r.File, Stat: r.Stat,
		Refs: r.Refs, Tips: r.Tips, live: r.live}
}

// builder interns strings into the arrays shared by all repos.
type builder struct {
	mu      sync.Mutex
	strs    *[]string
	paths   *[]string
	strIdx  map[string]uint32
	pathIdx map[string]uint32
}

func newBuilder(old *Index) *builder {
	var strs, paths []string
	if old != nil {
		strs, paths = slices.Clip(old.Strs), slices.Clip(old.Paths)
	}
	b := &builder{strs: &strs, paths: &paths, strIdx: make(map[string]uint32, len(strs)), pathIdx: make(map[string]uint32, len(paths))}
	for i, s := range strs {
		b.strIdx[s] = uint32(i)
	}
	for i, s := range paths {
		b.pathIdx[s] = uint32(i)
	}
	return b
}

func (b *builder) str(s string) uint32  { return b.intern(s, b.strs, b.strIdx) }
func (b *builder) path(s string) uint32 { return b.intern(s, b.paths, b.pathIdx) }

func (b *builder) intern(s string, arr *[]string, idx map[string]uint32) uint32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if v, ok := idx[s]; ok {
		return v
	}
	v := uint32(len(*arr))
	*arr = append(*arr, s)
	idx[s] = v
	return v
}

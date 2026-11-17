// Package engine runs query conditions against an index.
package engine

import (
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"sheaf/internal/index"
	"sheaf/internal/query"
)

// Engine holds per-index search data. Build one per Index version.
type Engine struct {
	ix        *index.Index
	lower     [][]string // per repo, per commit: lowercased "subject hash repo"
	pathLower []string
}

func New(ix *index.Index) *Engine {
	e := &Engine{ix: ix, lower: make([][]string, len(ix.Repos)), pathLower: make([]string, len(ix.Paths))}
	// In parallel chunks: every index update and scope switch waits for this (a large workspace with 35 repos and 230k commits: 90 ms in one goroutine).
	const chunk = 8192
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	run := func(f func()) {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			f()
		}()
	}
	for k, r := range ix.Repos {
		e.lower[k] = make([]string, r.N())
		tail := " " + strings.ToLower(r.Name)
		for lo := 0; lo < r.N(); lo += chunk {
			run(func() {
				for i := lo; i < min(lo+chunk, r.N()); i++ {
					e.lower[k][i] = strings.ToLower(r.Subject[i]) + " " + r.Oid(int32(i)).String() + tail
				}
			})
		}
	}
	for lo := 0; lo < len(ix.Paths); lo += chunk {
		run(func() {
			for i := lo; i < min(lo+chunk, len(ix.Paths)); i++ {
				e.pathLower[i] = strings.ToLower(ix.Paths[i])
			}
		})
	}
	wg.Wait()
	return e
}

func (e *Engine) Index() *index.Index { return e.ix }

// Bases maps repo name to its base branch (set by the user per repo).
type Bases map[string]string

// Resolve is the base branch to compare repo against: the explicit override, else the repo's configured one.
func (b Bases) Resolve(repo, override string) string {
	if override != "" {
		return override
	}
	return b[repo]
}

// Result is the outcome of one query.
type Result struct {
	Rows []index.CommitRef // newest first, after every filter
	// ByRepo: hits per repo without the whole-repo entries of Paths, so the repo list and dropdown show
	// where else the other conditions hit.
	ByRepo []int
}

// plan is a compiled query ready to test commits.
type plan struct {
	m         *query.Matcher
	branch    []*index.Set // per repo; nil = no branch filter; entry nil = repo has none of the branches
	branchNot bool
	hasPaths  bool
	whole     []bool   // per repo: a Paths entry is the whole repo
	fileHit   [][]bool // per repo, per ix.Paths entry: a changed file there hits a dir or text entry; nil = none can
	hasFile   bool     // some entry is a dir or text
}

// Bits of what match returns.
const (
	hitRow    = 1 << iota // passes every condition
	hitByRepo             // passes with the whole-repo entries of Paths left out
)

func (e *Engine) plan(m *query.Matcher, bases Bases) *plan {
	p := &plan{m: m}
	if ps := m.C.CleanPaths(); len(ps) > 0 {
		e.planPaths(p, ps)
	}
	if bs := m.C.Branches; len(bs) > 0 {
		p.branch, p.branchNot = make([]*index.Set, len(e.ix.Repos)), m.C.BranchNot
		for k, r := range e.ix.Repos {
			if s, ok := branchSet(r, bs, bases[r.Name]); ok {
				p.branch[k] = &s
			}
		}
	}
	return p
}

func (e *Engine) planPaths(p *plan, entries []string) {
	n := len(e.ix.Repos)
	p.hasPaths, p.whole, p.fileHit = true, make([]bool, n), make([][]bool, n)
	isRepo := func(name string) bool { _, ok := e.ix.RepoByName(name); return ok }
	var texts []string
	dirs := map[int][]string{}
	for _, x := range entries {
		repo, dir := query.SplitPath(x, isRepo)
		id, _ := e.ix.RepoByName(repo)
		switch {
		case repo == "":
			texts = append(texts, strings.ToLower(dir))
		case dir == "":
			p.whole[id] = true
		default:
			dirs[int(id)] = append(dirs[int(id)], dir)
		}
	}
	p.hasFile = len(texts) > 0 || len(dirs) > 0
	var textHit []bool
	if len(texts) > 0 {
		textHit = make([]bool, len(e.pathLower))
		for i, s := range e.pathLower {
			textHit[i] = slices.ContainsFunc(texts, func(t string) bool { return strings.Contains(s, t) })
		}
	}
	for k := range p.fileHit {
		ds := dirs[k]
		if len(ds) == 0 {
			p.fileHit[k] = textHit
			continue
		}
		hit := make([]bool, len(e.ix.Paths))
		for i, f := range e.ix.Paths {
			hit[i] = (textHit != nil && textHit[i]) || slices.ContainsFunc(ds, func(d string) bool { return f == d || strings.HasPrefix(f, d+"/") })
		}
		p.fileHit[k] = hit
	}
}

// branchSet is the union of each branch's own commits: those not on the base, or the whole history of the base itself.
// ok is false when r has none of the branches.
func branchSet(r *index.Repo, names []string, baseName string) (index.Set, bool) {
	base := int32(-1)
	if baseName != "" {
		if bi, ok := r.Ref(baseName); ok {
			base = bi
		}
	}
	var own, whole []int32
	for _, b := range names {
		head, ok := r.Ref(b)
		switch {
		case !ok:
		case base >= 0 && !sameBranch(r, baseName, b):
			own = append(own, head)
		default:
			whole = append(whole, head)
		}
	}
	if len(own)+len(whole) == 0 {
		return index.Set{}, false
	}
	var s index.Set
	if len(own) > 0 {
		s = ownCommits(r, own, base)
	}
	if len(whole) > 0 {
		s = s.Union(r.RangeAll(whole, -1))
	}
	return s, true
}

// ownCommits is what the branches added over base. A branch already merged into base has nothing over it any more, so it
// counts the commits it had when it was merged: finding a requirement's changes must not stop working once they land.
func ownCommits(r *index.Repo, heads []int32, base int32) index.Set {
	inBase := r.Range(base, -1)
	var pending, merged []int32
	for _, h := range heads {
		if inBase.Has(h) {
			merged = append(merged, h)
		} else {
			pending = append(pending, h)
		}
	}
	s := r.RangeAll(pending, base)
	for _, h := range merged {
		if m, ok := r.MergedRange(h, base); ok {
			s = s.Union(m)
		}
	}
	return s
}

// sameBranch: selecting the base itself shows all its commits, also when
// the base is the remote-tracking twin of it (base origin/master, branch master).
func sameBranch(r *index.Repo, base, b string) bool {
	if base == b {
		return true
	}
	_, name, ok := strings.Cut(base, "/")
	if !ok || name != b {
		return false
	}
	_, remote := r.Ref("refs/remotes/" + base)
	return remote
}

// match tests everything except time (callers order by time); it returns hitRow / hitByRepo bits.
func (e *Engine) match(p *plan, c index.CommitRef) uint8 {
	r := e.ix.Repos[c.Repo]
	if p.branch != nil {
		s := p.branch[c.Repo]
		if (s != nil && s.Has(c.I)) == p.branchNot {
			return 0
		}
	}
	if p.m.HasAuthor() && !p.m.Author(e.ix.Strs[r.Email[c.I]]) {
		return 0
	}
	if p.m.HasText() && !p.m.Text(e.lower[c.Repo][c.I]) {
		return 0
	}
	if !p.hasPaths {
		return hitRow | hitByRepo
	}
	file := false
	if hit := p.fileHit[c.Repo]; hit != nil {
		files, _ := r.Files(c.I)
		file = slices.ContainsFunc(files, func(f uint32) bool { return hit[f] })
	}
	var out uint8
	if file || p.whole[c.Repo] {
		out |= hitRow
	}
	if file || !p.hasFile {
		out |= hitByRepo
	}
	return out
}

// Run evaluates m over the whole index, newest first.
func (e *Engine) Run(m *query.Matcher, bases Bases) *Result {
	p := e.plan(m, bases)
	order := e.candidates(m)
	hit := e.scan(p, order)
	res := &Result{ByRepo: make([]int, len(e.ix.Repos))}
	for k, h := range hit {
		if h&hitByRepo != 0 {
			res.ByRepo[order[k].Repo]++
		}
		if h&hitRow != 0 {
			res.Rows = append(res.Rows, order[k])
		}
	}
	return res
}

// Count is len(Run(m).Rows) without collecting rows.
func (e *Engine) Count(m *query.Matcher, bases Bases) int {
	p := e.plan(m, bases)
	order := e.candidates(m)
	n := 0
	for _, h := range e.scan(p, order) {
		if h&hitRow != 0 {
			n++
		}
	}
	return n
}

// candidates is the part of Order inside the date filter (Order is newest first).
func (e *Engine) candidates(m *query.Matcher) []index.CommitRef {
	o := e.ix.Order
	at := func(k int) int64 { return e.ix.Repos[o[k].Repo].Time[o[k].I] }
	lo := sort.Search(len(o), func(k int) bool { return m.Before(at(k)) })
	hi := sort.Search(len(o), func(k int) bool { return !m.After(at(k)) })
	return o[lo:max(lo, hi)]
}

// scan matches candidates in parallel chunks; regexes need it (RE2 folding case is slow).
func (e *Engine) scan(p *plan, order []index.CommitRef) []uint8 {
	hit := make([]uint8, len(order))
	const chunk = 8192
	if len(order) <= chunk {
		for k, c := range order {
			hit[k] = e.match(p, c)
		}
		return hit
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for lo := 0; lo < len(order); lo += chunk {
		hi := min(lo+chunk, len(order))
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			for k := lo; k < hi; k++ {
				hit[k] = e.match(p, order[k])
			}
		}()
	}
	wg.Wait()
	return hit
}

// Matches tests single commits, e.g. which Tags a visible row also hits.
type Matches struct {
	e *Engine
	p *plan
}

func (e *Engine) Matcher(m *query.Matcher, bases Bases) *Matches {
	return &Matches{e, e.plan(m, bases)}
}

func (x *Matches) Has(c index.CommitRef) bool {
	r := x.e.ix.Repos[c.Repo]
	return x.p.m.Time(r.Time[c.I]) && x.e.match(x.p, c)&hitRow != 0
}

// UTF16Range converts a byte range of s to UTF-16 offsets, which is what
// JavaScript string indices count.
func UTF16Range(s string, start, end int) (int, int) {
	u := func(b string) int {
		n := 0
		for len(b) > 0 {
			r, size := utf8.DecodeRuneInString(b)
			n += len(utf16.Encode([]rune{r}))
			b = b[size:]
		}
		return n
	}
	a := u(s[:start])
	return a, a + u(s[start:end])
}

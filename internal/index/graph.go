package index

import (
	"slices"
	"sort"
)

import "math/bits"

type bitset []uint64

func newBits(n int) bitset        { return make(bitset, (n+63)/64) }
func (b bitset) has(i int32) bool { return int(i>>6) < len(b) && b[i>>6]&(1<<(uint(i)&63)) != 0 }
func (b bitset) set(i int32)      { b[i>>6] |= 1 << (uint(i) & 63) }
func (b bitset) count() int {
	n := 0
	for _, w := range b {
		n += bits.OnesCount64(w)
	}
	return n
}

type Set struct{ b bitset }

func (s Set) Has(i int32) bool { return s.b.has(i) }

// Union is s ∪ t, both of the same repo.
func (s Set) Union(t Set) Set {
	out := make(bitset, max(len(s.b), len(t.b)))
	copy(out, s.b)
	for k, w := range t.b {
		out[k] |= w
	}
	return Set{out}
}

// Reach marks every commit reachable from starts without entering stop.
func (r *Repo) Reach(starts []int32, stop bitset) bitset {
	seen := newBits(r.N())
	stack := append([]int32(nil), starts...)
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen.has(i) || stop.has(i) {
			continue
		}
		seen.set(i)
		stack = append(stack, r.Parents(i)...)
	}
	return seen
}

// Range is `git rev-list head --not base`: commits reachable from head but
// not from base. base < 0 means everything reachable from head.
func (r *Repo) Range(head, base int32) Set { return r.RangeAll([]int32{head}, base) }

// RangeAll is Range from several heads at once: one walk instead of a union of walks.
func (r *Repo) RangeAll(heads []int32, base int32) Set {
	var stop bitset
	if base >= 0 {
		stop = r.Reach([]int32{base}, nil)
	}
	return Set{r.Reach(heads, stop)}
}

// AheadBehind is `git rev-list --left-right --count base...head` (ahead first).
func (r *Repo) AheadBehind(base, head int32) (ahead, behind int) {
	// Reach sets are closed under parents, so head..base is just h minus b.
	b := r.Reach([]int32{base}, nil)
	h := r.Reach([]int32{head}, nil)
	for k := range h {
		ahead += bits.OnesCount64(h[k] &^ b[k])
		behind += bits.OnesCount64(b[k] &^ h[k])
	}
	return ahead, behind
}

// Holding counts, for each tip, the commits reachable from it but not from base (base < 0: not from anything):
// which branch carries commits that have not reached the base yet. Walks only each tip's own range.
func (r *Repo) Holding(tips []int32, base int32, commits []int32) []int {
	var stop bitset
	if base >= 0 {
		stop = r.Reach([]int32{base}, nil)
	}
	out := make([]int, len(tips))
	for k, tip := range tips {
		in := r.Reach([]int32{tip}, stop)
		for _, c := range commits {
			if in.has(c) {
				out[k]++
			}
		}
	}
	return out
}

// MergedRange is the commits head had of its own when it was merged into base: everything reachable from head but not from
// the first parent of the base commit that brought it in. A fast-forward leaves no such commit, and then ok is false; so
// does a head that is not in base yet.
func (r *Repo) MergedRange(head, base int32) (s Set, ok bool) {
	if head < 0 || base < 0 {
		return Set{}, false
	}
	_, before, ok := r.broughtBy(head, base)
	if !ok {
		return Set{}, false
	}
	return r.Range(head, before), true
}

// MergedBy is the merge commit that brought c in directly, following merges of merges (feature into dev, dev into master
// finds the first); -1 when c came in without a merge.
func (r *Repo) MergedBy(c, base int32) int32 {
	last := int32(-1)
	for tip := base; tip >= 0; {
		m, _, ok := r.broughtBy(c, tip)
		if !ok {
			break
		}
		last = m
		ps := r.Parents(m)
		if len(ps) < 2 {
			break
		}
		tip = ps[1]
	}
	return last
}

// broughtBy finds the commit on tip's first-parent chain that first contains c, and the chain commit before it. Not ok when
// c is on the chain itself (nothing brought it in) or tip does not contain it.
func (r *Repo) broughtBy(c, tip int32) (m, before int32, ok bool) {
	var chain []int32
	for at := tip; ; {
		if at == c {
			return 0, 0, false
		}
		chain = append(chain, at)
		parents := r.Parents(at)
		if len(parents) == 0 {
			break
		}
		at = parents[0]
	}
	slices.Reverse(chain)
	// Containment is monotone along the chain, so only logarithmically many walks are needed.
	i := sort.Search(len(chain), func(i int) bool { return r.Range(chain[i], -1).Has(c) })
	if i == 0 || i == len(chain) {
		return 0, 0, false
	}
	return chain[i], chain[i-1], true
}

// Package index holds the commit history of every repo in a workspace in a
// compact columnar form. An Index is immutable once built: Update returns a
// new Index that shares unchanged repos with the old one, so readers never lock.
package index

import (
	"slices"
	"strings"
	"sync"

	"sheaf/internal/gitcli"
)

// Version of the snapshot format; a mismatch on Load means rebuild.
const Version = 1

type RepoID uint16

// CommitRef names one commit inside an Index.
type CommitRef struct {
	Repo RepoID
	I    int32
}

type Index struct {
	Repos []*Repo
	// Interned author names / emails and file paths, shared by all repos.
	// Append-only: an older Index only reads ids below its own length.
	Strs  []string
	Paths []string
	// Order is every live commit, newest first.
	Order []CommitRef
}

// Repo is the columnar history of one repository.
type Repo struct {
	Name   string
	Dir    string
	GitDir string

	Hash    []byte // 20 bytes per commit
	Time    []int64
	Author  []uint32 // into Index.Strs
	Email   []uint32 // into Index.Strs
	Subject []string
	ParOff  []uint32 // len n+1
	Par     []int32
	FileOff []uint32 // len n+1
	File    []uint32 // into Index.Paths
	Stat    []byte   // A M D R C T per file

	Refs []Ref
	// Tips is what was indexed: every ref and worktree HEAD, by name.
	Tips map[string]gitcli.Oid

	live    bitset // reachable from Tips (force-pushed commits stay indexed but hidden)
	oidOnce sync.Once
	byOid   map[gitcli.Oid]int32
	refOnce sync.Once
	refsAt  map[int32][]int32 // commit -> indices into Refs
}

type Ref struct {
	Name   string // refs/heads/x, refs/remotes/origin/x, refs/tags/v1
	Commit int32
}

func (r *Repo) N() int                  { return len(r.Time) }
func (r *Repo) Oid(i int32) gitcli.Oid  { return gitcli.Oid(r.Hash[i*20 : i*20+20]) }
func (r *Repo) Parents(i int32) []int32 { return r.Par[r.ParOff[i]:r.ParOff[i+1]] }
func (r *Repo) Live(i int32) bool       { return r.live.has(i) }
func (r *Repo) Files(i int32) ([]uint32, []byte) {
	a, b := r.FileOff[i], r.FileOff[i+1]
	return r.File[a:b], r.Stat[a:b]
}

// FindHash returns the commit index of a 40-digit hash, or -1 when the commit is not here; an error when hash is no object id.
func (r *Repo) FindHash(hash string) (int32, error) {
	oid, err := gitcli.ParseOid(hash)
	if err != nil {
		return -1, err
	}
	return r.Find(oid), nil
}

// Find returns the commit index of oid, or -1.
func (r *Repo) Find(oid gitcli.Oid) int32 {
	r.oidOnce.Do(func() {
		r.byOid = make(map[gitcli.Oid]int32, r.N())
		for i := 0; i < r.N(); i++ {
			r.byOid[r.Oid(int32(i))] = int32(i)
		}
	})
	if i, ok := r.byOid[oid]; ok {
		return i
	}
	return -1
}

func (r *Repo) RefsAt(i int32) []Ref {
	r.refOnce.Do(func() {
		r.refsAt = map[int32][]int32{}
		for k, ref := range r.Refs {
			r.refsAt[ref.Commit] = append(r.refsAt[ref.Commit], int32(k))
		}
	})
	ks := r.refsAt[i]
	out := make([]Ref, len(ks))
	for j, k := range ks {
		out[j] = r.Refs[k]
	}
	return out
}

// Ref returns the commit a ref name points at, trying the exact name, then
// refs/heads/<name> or refs/tags/<name>, then refs/remotes/*/<name>.
func (r *Repo) Ref(name string) (int32, bool) {
	var remote int32 = -1
	for _, ref := range r.Refs {
		switch {
		case ref.Name == name || ref.Name == "refs/heads/"+name || ref.Name == "refs/tags/"+name:
			return ref.Commit, true
		case remote < 0 && strings.HasPrefix(ref.Name, "refs/remotes/") && strings.HasSuffix(ref.Name, "/"+name):
			remote = ref.Commit
		}
	}
	return remote, remote >= 0
}

// RepoByName returns the repo id for a workspace-relative name.
func (ix *Index) RepoByName(name string) (RepoID, bool) {
	for i, r := range ix.Repos {
		if r.Name == name {
			return RepoID(i), true
		}
	}
	return 0, false
}

// Commits is the number of live commits.
func (ix *Index) Commits() int { return len(ix.Order) }

func (ix *Index) finish() {
	n := 0
	for _, r := range ix.Repos {
		if r.live == nil {
			r.computeLive()
		}
		n += r.live.count()
	}
	order := make([]CommitRef, 0, n)
	for id, r := range ix.Repos {
		for i := 0; i < r.N(); i++ {
			if r.live.has(int32(i)) {
				order = append(order, CommitRef{RepoID(id), int32(i)})
			}
		}
	}
	slices.SortFunc(order, func(a, b CommitRef) int {
		ta, tb := ix.Repos[a.Repo].Time[a.I], ix.Repos[b.Repo].Time[b.I]
		switch {
		case ta != tb:
			if ta > tb {
				return -1
			}
			return 1
		case a.Repo != b.Repo:
			return int(a.Repo) - int(b.Repo)
		default:
			return int(a.I) - int(b.I)
		}
	})
	ix.Order = order
}

func (r *Repo) computeLive() {
	var starts []int32
	for _, oid := range r.Tips {
		if i := r.Find(oid); i >= 0 {
			starts = append(starts, i)
		}
	}
	r.live = r.Reach(starts, nil)
}

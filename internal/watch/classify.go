package watch

import (
	"path/filepath"
	"slices"
	"strings"
)

// Repo is what Classify needs to know of a repository.
type Repo struct {
	Name      string
	GitDir    string   // common git dir
	Worktrees []string // main working directory first
}

// Changes is what a batch of changed directories asks to refresh.
type Changes struct {
	Repos     []string // repo names whose refs or worktree HEADs may have moved
	Worktrees []string // worktree paths whose uncommitted changes may differ
	Rescan    bool     // a change outside every known repo: repos may have come or gone
	Outside   []string // those directories, for the caller to check before rescanning
}

type prefix struct {
	dir  string
	repo int
	git  bool // the repo's git dir, else worktree wt
	wt   int
}

// Classify maps changed directories to what to refresh, each by the deepest
// known directory it is in:
//   - the git dir itself (HEAD, index, packed-refs): refs, and the main worktree's status (index)
//   - refs/: refs; worktrees/<name>/ (a linked worktree's HEAD and index): refs and the repo's worktrees
//   - objects/, logs/ and the like: nothing (they change with refs)
//   - inside a worktree: that worktree's status
//
// A hint covers everything beneath it (mygo drops hints under another one in
// the same batch), so git dirs and worktrees under a changed directory count too.
func Classify(repos []Repo, dirs []string) Changes {
	var ps []prefix
	for i, r := range repos {
		ps = append(ps, prefix{dir: resolve(r.GitDir), repo: i, git: true})
		for k, w := range r.Worktrees {
			ps = append(ps, prefix{dir: resolve(w), repo: i, wt: k})
		}
	}
	repoSet, wtSet := map[string]bool{}, map[string]bool{}
	var c Changes
	addWt := func(r Repo, k int) {
		if k < len(r.Worktrees) && !wtSet[r.Worktrees[k]] {
			wtSet[r.Worktrees[k]] = true
			c.Worktrees = append(c.Worktrees, r.Worktrees[k])
		}
	}
	addRepo := func(r Repo) {
		if !repoSet[r.Name] {
			repoSet[r.Name] = true
			c.Repos = append(c.Repos, r.Name)
		}
	}
	for _, d := range dirs {
		d = filepath.Clean(d)
		best := -1
		for k, p := range ps {
			if under(d, p.dir) && (best < 0 || len(p.dir) > len(ps[best].dir)) {
				best = k
			}
		}
		if best < 0 {
			c.Rescan = true
			c.Outside = append(c.Outside, d)
		}
		for _, p := range ps {
			if p.dir != d && under(p.dir, d) {
				r := repos[p.repo]
				if p.git {
					addRepo(r)
					for k := range r.Worktrees {
						addWt(r, k)
					}
				} else {
					addWt(r, p.wt)
				}
			}
		}
		if best < 0 {
			continue
		}
		p := ps[best]
		r := repos[p.repo]
		if !p.git {
			addWt(r, p.wt)
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(d, p.dir), string(filepath.Separator))
		first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		switch {
		case rel == "":
			addRepo(r)
			addWt(r, 0)
		case first == "refs":
			addRepo(r)
		case first == "worktrees":
			addRepo(r)
			for k := range r.Worktrees {
				addWt(r, k)
			}
		}
	}
	slices.Sort(c.Repos)
	slices.Sort(c.Worktrees)
	return c
}

// resolve matches the watch, which reports paths with symlinks resolved.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

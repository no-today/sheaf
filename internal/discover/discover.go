// Package discover finds the git repositories of a workspace: plain repos,
// submodules and nested repos are separate histories; linked worktrees are
// grouped under the repo they belong to.
package discover

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"sheaf/internal/gitcli"
)

type Kind string

const (
	KindRepo      Kind = "repo"
	KindSubmodule Kind = "submodule"
)

type Repo struct {
	Name      string // workspace-relative name; the root reserves its basename
	Dir       string // main working directory
	GitDir    string // common git directory
	Kind      Kind
	Worktrees []Worktree // main working directory first
}

type Worktree struct {
	Path   string
	Branch string // refs/heads/x; empty when detached
	Head   gitcli.Oid
	Main   bool
}

// skipDirs: dependency and build output trees never hold repos we want (nor changes worth watching).
var skipDirs = map[string]bool{
	"node_modules": true, "target": true, "dist": true, "build": true,
	".gradle": true, ".idea": true, ".venv": true, "vendor": true,
}

// SkipDir reports whether a directory of this name is never searched or watched.
func SkipDir(name string) bool { return skipDirs[name] }

// Scan walks root and returns its repositories sorted by Name.
func Scan(ctx context.Context, root string) ([]Repo, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var rootIsRepo bool
	var repos []Repo
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable directory: skip it, keep walking
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() && p != root && SkipDir(d.Name()) { // the root is what the user chose, whatever it is called
			return filepath.SkipDir
		}
		if d.Name() != ".git" {
			return nil
		}
		dir := filepath.Dir(p)
		gitDir, kind, ok := resolve(p, d.IsDir())
		if ok && !seen[gitDir] {
			seen[gitDir] = true
			if dir == root {
				rootIsRepo = true
			}
			repos = append(repos, Repo{Dir: dir, GitDir: gitDir, Kind: kind})
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for i := range repos {
		repos[i].Name = rel(root, repos[i].Dir, rootIsRepo)
		wg.Add(1)
		go func() { defer wg.Done(); repos[i].Worktrees = worktrees(ctx, &repos[i]) }()
	}
	wg.Wait()
	sort.Slice(repos, func(a, b int) bool { return repos[a].Name < repos[b].Name })
	return repos, nil
}

// resolve returns the common git dir for a ".git" entry. A .git file pointing
// at a dir with a commondir file is a linked worktree: not a repo of its own.
func resolve(p string, isDir bool) (string, Kind, bool) {
	if isDir {
		return clean(p), KindRepo, true
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", "", false
	}
	g, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return "", "", false
	}
	g = strings.TrimSpace(g)
	if !filepath.IsAbs(g) {
		g = filepath.Join(filepath.Dir(p), g)
	}
	if _, err := os.Stat(filepath.Join(g, "commondir")); err == nil {
		return "", "", false
	}
	if _, err := os.Stat(g); err != nil {
		return "", "", false
	}
	return clean(g), KindSubmodule, true
}

// Reload re-reads the worktrees of one repo (their HEADs and branches move with checkouts).
func Reload(ctx context.Context, r Repo) Repo {
	r.Worktrees = worktrees(ctx, &r)
	return r
}

func worktrees(ctx context.Context, r *Repo) []Worktree {
	main := Worktree{Path: r.Dir, Main: true}
	ws, err := gitcli.Worktrees(ctx, r.Dir)
	if err != nil || len(ws) == 0 {
		return []Worktree{main}
	}
	out := []Worktree{{Path: r.Dir, Branch: ws[0].Branch, Head: ws[0].Head, Main: true}}
	for _, w := range ws[1:] {
		if w.Bare || w.Prunable {
			continue
		}
		out = append(out, Worktree{Path: w.Path, Branch: w.Branch, Head: w.Head})
	}
	return out
}

func clean(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func rel(root, dir string, rootIsRepo bool) string {
	if dir == root {
		return filepath.Base(root)
	}
	r, err := filepath.Rel(root, dir)
	if err != nil {
		return dir
	}
	if rootIsRepo && r == filepath.Base(root) {
		return "./" + r
	}
	return filepath.ToSlash(r)
}

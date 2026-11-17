package gitcli

import (
	"context"
	"strings"
)

// Ref is a ref pointing (after peeling tags) at a commit.
type Ref struct {
	Name string // refs/heads/x, refs/remotes/origin/x, refs/tags/v1
	Oid  Oid
}

// Refs lists branches, remote branches and tags. Annotated tags are peeled to
// their commit. refs/stash and symbolic remote HEADs are skipped.
func Refs(ctx context.Context, dir string) ([]Ref, error) {
	out, err := Output(ctx, dir, "for-each-ref",
		"--format=%(objectname)%1f%(*objectname)%1f%(objecttype)%1f%(*objecttype)%1f%(refname)%1f%(symref)",
		"refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(l, "\x1f")
		if len(f) != 6 || f[5] != "" { // symbolic refs such as origin/HEAD
			continue
		}
		oid, typ := f[0], f[2]
		if f[1] != "" { // annotated tag: use the peeled object
			oid, typ = f[1], f[3]
		}
		if typ != "commit" {
			continue
		}
		o, err := ParseOid(oid)
		if err != nil {
			continue
		}
		refs = append(refs, Ref{Name: f[4], Oid: o})
	}
	return refs, nil
}

// Worktree is one entry of `git worktree list`.
type Worktree struct {
	Path     string
	Head     Oid
	Branch   string // refs/heads/x, empty when detached
	Bare     bool
	Prunable bool // directory is gone
}

func Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := Output(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var ws []Worktree
	var cur *Worktree
	for _, l := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(l, " ")
		switch k {
		case "worktree":
			ws = append(ws, Worktree{Path: v})
			cur = &ws[len(ws)-1]
		case "HEAD":
			if cur != nil {
				cur.Head, _ = ParseOid(v)
			}
		case "branch":
			if cur != nil {
				cur.Branch = v
			}
		case "bare":
			if cur != nil {
				cur.Bare = true
			}
		case "prunable":
			if cur != nil {
				cur.Prunable = true
			}
		}
	}
	return ws, nil
}

package gitcli

import (
	"bytes"
	"context"
	"strings"
)

// Areas of an uncommitted change.
const (
	Staged    = "staged"
	Unstaged  = "unstaged"
	Untracked = "untracked"
	// Head is not an area of git status: staged and unstaged together, the worktree against HEAD.
	Head = "head"
)

// Change is one uncommitted file in one area. A file staged and then edited
// again appears twice: once staged, once unstaged.
type Change struct {
	Path      string
	OldPath   string // renames and copies
	Area      string // Staged | Unstaged | Untracked
	Status    byte   // A M D R C T U (U: unmerged)
	Submodule bool
}

// Status lists the uncommitted changes of a worktree. --no-optional-locks
// keeps git from refreshing (writing) the index while it looks.
func Status(ctx context.Context, worktree string) ([]Change, error) {
	out, err := Output(ctx, worktree, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return nil, err
	}
	return parseStatusV2(out), nil
}

// StatusWithHead keeps HEAD and the changes in one status run so callers need no second git process.
func StatusWithHead(ctx context.Context, worktree string) ([]Change, Oid, error) {
	out, err := Output(ctx, worktree, "status", "--porcelain=v2", "-z", "--branch", "--no-ahead-behind", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return nil, Oid{}, err
	}
	var head Oid
	for _, record := range bytes.Split(out, []byte{0}) {
		if oid, ok := strings.CutPrefix(string(record), "# branch.oid "); ok && oid != "(initial)" {
			if head, err = ParseOid(oid); err != nil {
				return nil, Oid{}, err
			}
			break
		}
	}
	return parseStatusV2(out), head, nil
}

func DirtyCount(cs []Change) int {
	seen := map[string]bool{}
	for _, c := range cs {
		seen[c.Path] = true
	}
	return len(seen)
}

// parseStatusV2 reads `status --porcelain=v2 -z`:
//
//	1 XY sub mH mI mW hH hI path
//	2 XY sub mH mI mW hH hI Xscore path NUL origPath
//	u XY sub m1 m2 m3 mW h1 h2 h3 path
//	? path
func parseStatusV2(b []byte) []Change {
	var out []Change
	recs := bytes.Split(b, []byte{0})
	for i := 0; i < len(recs); i++ {
		r := string(recs[i])
		if len(r) < 2 {
			continue
		}
		switch r[0] {
		case '?':
			out = append(out, Change{Path: r[2:], Area: Untracked, Status: 'A'})
		case '1', '2', 'u':
			n := map[byte]int{'1': 9, '2': 10, 'u': 11}[r[0]]
			f := strings.SplitN(r, " ", n)
			if len(f) != n {
				continue
			}
			xy, sub, path := f[1], f[2], f[n-1]
			old := ""
			if r[0] == '2' && i+1 < len(recs) {
				i++
				old = string(recs[i])
			}
			isSub := strings.HasPrefix(sub, "S")
			if r[0] == 'u' {
				out = append(out, Change{Path: path, Area: Unstaged, Status: 'U', Submodule: isSub})
				continue
			}
			if x := xy[0]; x != '.' {
				c := Change{Path: path, Area: Staged, Status: x, Submodule: isSub}
				if x == 'R' || x == 'C' {
					c.OldPath = old
				}
				out = append(out, c)
			}
			if y := xy[1]; y != '.' {
				out = append(out, Change{Path: path, Area: Unstaged, Status: y, Submodule: isSub})
			}
		}
	}
	return out
}

// DiffNumstat counts lines of the staged (cached) or unstaged changes.
func DiffNumstat(ctx context.Context, worktree string, cached bool) ([]NumStat, error) {
	args := []string{"diff", "--numstat", "-z", "-M", "--no-ext-diff", "--no-textconv"}
	if cached {
		args = append(args, "--cached")
	}
	out, err := Output(ctx, worktree, args...)
	if err != nil {
		return nil, err
	}
	return parseNumstatZ(out), nil
}

func DiffHeadNumstat(ctx context.Context, worktree string) ([]NumStat, error) {
	out, err := Output(ctx, worktree, "diff", "--numstat", "-z", "-M", "--no-ext-diff", "--no-textconv", "HEAD")
	if err != nil {
		return nil, err
	}
	return parseNumstatZ(out), nil
}

// DiffPatch is the staged (cached: index vs HEAD) or unstaged (worktree vs
// index) diff of one file. oldPath is set for staged renames.
func DiffPatch(ctx context.Context, worktree string, cached bool, path, oldPath string) (Patch, error) {
	if cached {
		return diffPatch(ctx, worktree, "--cached", path, oldPath)
	}
	return diffPatch(ctx, worktree, "", path, oldPath)
}

// DiffHeadPatch is the worktree against HEAD: staged and unstaged changes together.
func DiffHeadPatch(ctx context.Context, worktree, path, oldPath string) (Patch, error) {
	return diffPatch(ctx, worktree, "HEAD", path, oldPath)
}

func diffPatch(ctx context.Context, worktree, against, path, oldPath string) (Patch, error) {
	args := patchArgs(ctx, "diff")
	if against != "" {
		args = append(args, against)
	}
	args = append(args, "--", path)
	if oldPath != "" && oldPath != path {
		args = append(args, oldPath)
	}
	out, err := Output(ctx, worktree, args...)
	if err != nil {
		return Patch{}, err
	}
	return ParsePatch(string(out)), nil
}

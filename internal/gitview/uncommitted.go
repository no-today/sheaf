package gitview

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"sheaf/internal/gitcli"
)

// FileStatus is one uncommitted file of a worktree, in one area.
type FileStatus struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Area      string `json:"area"`   // staged | unstaged | untracked
	Status    string `json:"status"` // A M D R C T U
	Add       int    `json:"add"`
	Del       int    `json:"del"`
	Binary    bool   `json:"binary,omitempty"`
	Submodule bool   `json:"submodule,omitempty"`
}

// Untracked files larger than this show only their first maxUntracked bytes.
const maxUntracked = 2 << 20

// Untracked line counts are read for at most this many files (a fresh build dir can hold thousands).
const maxCounted = 300

// Uncommitted lists a worktree's changes grouped staged, unstaged, untracked
// (git status order inside each group), with line counts.
func Uncommitted(ctx context.Context, worktree string) ([]FileStatus, error) {
	st, err := Status(ctx, worktree)
	if err != nil {
		return nil, err
	}
	return UncommittedOf(ctx, worktree, st), nil
}

func WorktreeNumstat(ctx context.Context, worktree string) ([]gitcli.NumStat, error) {
	return gitcli.DiffHeadNumstat(ctx, worktree)
}

// WorktreeStatus is one git status run of a worktree, kept so the file list can be built from it without running it again.
type WorktreeStatus struct {
	changes []gitcli.Change
	head    gitcli.Oid
}

// Head is kept with status so a refresh cannot mix it with a cached worktree HEAD.
func (s WorktreeStatus) Head() gitcli.Oid { return s.head }

// Count is the number of files with uncommitted changes.
func (s WorktreeStatus) Count() int { return gitcli.DirtyCount(s.changes) }

// UncommittedFile is one status entry, with paths relative to its worktree.
type UncommittedFile struct {
	Worktree string `json:"worktree"`
	Path     string `json:"path"`
	OldPath  string `json:"oldPath,omitempty"`
	Area     string `json:"area"`
	Status   string `json:"status"`
}

// Files converts the cached status without reading Git or the worktree.
func (s WorktreeStatus) Files() []UncommittedFile {
	files := make([]UncommittedFile, 0, len(s.changes))
	for _, c := range s.changes {
		files = append(files, UncommittedFile{Path: c.Path, OldPath: c.OldPath, Area: c.Area, Status: string(c.Status)})
	}
	slices.SortFunc(files, func(a, b UncommittedFile) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.Area, b.Area))
	})
	return files
}

func Status(ctx context.Context, worktree string) (WorktreeStatus, error) {
	cs, head, err := gitcli.StatusWithHead(ctx, worktree)
	return WorktreeStatus{changes: cs, head: head}, err
}

// UncommittedOf is Uncommitted from a status already run.
func UncommittedOf(ctx context.Context, worktree string, st WorktreeStatus) []FileStatus {
	cs := st.changes
	var staged, unstaged []gitcli.NumStat
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); staged, _ = gitcli.DiffNumstat(ctx, worktree, true) }()
	go func() { defer wg.Done(); unstaged, _ = gitcli.DiffNumstat(ctx, worktree, false) }()
	wg.Wait()
	count := map[string]map[string]gitcli.NumStat{gitcli.Staged: byPath(staged), gitcli.Unstaged: byPath(unstaged)}

	out := []FileStatus{}
	counted := 0
	for _, area := range []string{gitcli.Staged, gitcli.Unstaged, gitcli.Untracked} {
		for _, c := range cs {
			if c.Area != area {
				continue
			}
			f := FileStatus{Path: c.Path, OldPath: c.OldPath, Area: c.Area, Status: string(c.Status), Submodule: c.Submodule}
			if n, ok := count[area][c.Path]; ok {
				f.Add, f.Del, f.Binary = n.Add, n.Del, n.Binary
			}
			if area == gitcli.Untracked && counted < maxCounted {
				counted++
				if b, err := readUntracked(worktree, c.Path); err == nil {
					f.Binary = isBinary(b)
					if !f.Binary {
						f.Add = len(splitLines(b))
					}
				}
			}
			out = append(out, f)
		}
	}
	return out
}

func byPath(ns []gitcli.NumStat) map[string]gitcli.NumStat {
	m := make(map[string]gitcli.NumStat, len(ns))
	for _, n := range ns {
		m[n.Path] = n
	}
	return m
}

// UncommittedPatch is the diff of one uncommitted file. An untracked file is
// read from disk and shown as wholly added: nothing is staged (no git add -N).
func UncommittedPatch(ctx context.Context, worktree, path, oldPath, area string) (gitcli.Patch, error) {
	switch area {
	case gitcli.Head:
		return gitcli.DiffHeadPatch(ctx, worktree, path, oldPath)
	case gitcli.Staged, gitcli.Unstaged:
		return gitcli.DiffPatch(ctx, worktree, area == gitcli.Staged, path, oldPath)
	case gitcli.Untracked:
		b, err := readUntracked(worktree, path)
		if err != nil {
			return gitcli.Patch{}, err
		}
		return addedPatch(b), nil
	}
	return gitcli.Patch{}, fmt.Errorf("unknown area %q", area)
}

// readUntracked reads a file inside the worktree; a symlink yields its target, as git would store it.
func readUntracked(worktree, path string) ([]byte, error) {
	if !filepath.IsLocal(filepath.FromSlash(path)) {
		return nil, fmt.Errorf("path %q is outside the worktree", path)
	}
	full := filepath.Join(worktree, filepath.FromSlash(path))
	st, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(full)
		return []byte(target), err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxUntracked+1)) // one over, so that textOf can tell the file was cut
}

// isBinary uses git's rule: a NUL byte in the first 8000 bytes.
func isBinary(b []byte) bool { return bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 }

func splitLines(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func addedPatch(b []byte) gitcli.Patch {
	if isBinary(b) {
		return gitcli.Patch{Binary: true}
	}
	lines := splitLines(b)
	if len(lines) == 0 {
		return gitcli.Patch{}
	}
	h := gitcli.Hunk{Header: fmt.Sprintf("@@ -0,0 +1,%d @@", len(lines)), Lines: make([]gitcli.Line, len(lines))}
	for i, l := range lines {
		h.Lines[i] = gitcli.Line{Kind: '+', New: i + 1, Text: strings.TrimSuffix(l, "\r")}
	}
	return gitcli.Patch{Hunks: []gitcli.Hunk{h}}
}

package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// NumStat is added/deleted line counts of one file in a commit.
type NumStat struct {
	Path    string
	OldPath string
	Add     int
	Del     int
	Binary  bool
}

// ShowNumstat returns line counts of a commit against its first parent.
func ShowNumstat(ctx context.Context, dir string, oid Oid) ([]NumStat, error) {
	out, err := Output(ctx, dir, "show", "--numstat", "-z", "-M", "--format=", "--diff-merges=first-parent",
		"--no-ext-diff", "--no-textconv", oid.String())
	if err != nil {
		return nil, err
	}
	return parseNumstatZ(out), nil
}

// -z format: "add\tdel\tpath\0" or, for renames, "add\tdel\t\0old\0new\0".
func parseNumstatZ(b []byte) []NumStat {
	var res []NumStat
	fields := bytes.Split(b, []byte{0})
	for i := 0; i < len(fields); i++ {
		f := string(bytes.TrimLeft(fields[i], "\n"))
		if f == "" {
			continue
		}
		parts := strings.SplitN(f, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		ns := NumStat{}
		if parts[0] == "-" {
			ns.Binary = true
		} else {
			ns.Add, _ = strconv.Atoi(parts[0])
			ns.Del, _ = strconv.Atoi(parts[1])
		}
		if parts[2] == "" && i+2 < len(fields) {
			ns.OldPath, ns.Path = string(fields[i+1]), string(fields[i+2])
			i += 2
		} else {
			ns.Path = parts[2]
		}
		res = append(res, ns)
	}
	return res
}

// ShowMessage returns the full commit message: subject and body.
func ShowMessage(ctx context.Context, dir string, oid Oid) (string, error) {
	out, err := Output(ctx, dir, "show", "-s", "--format=%B", "--no-show-signature", oid.String())
	return strings.TrimRight(string(out), "\n"), err
}

// Patch is a parsed unified diff of one file.
type Patch struct {
	Binary bool
	Hunks  []Hunk
}

type Hunk struct {
	Header string // "@@ -84,14 +84,18 @@ class X"
	Lines  []Line
}

type Line struct {
	Kind byte // ' ' '+' '-'
	Old  int  // 0 when not on the old side
	New  int  // 0 when not on the new side
	Text string
}

// ShowPatch returns the diff of one file in a commit against its first parent.
// oldPath is set for renames so both sides are found.
func ShowPatch(ctx context.Context, dir string, oid Oid, path, oldPath string) (Patch, error) {
	args := patchArgs(ctx, "show", "--format=", "--diff-merges=first-parent", oid.String(), "--", path)
	if oldPath != "" && oldPath != path {
		args = append(args, oldPath)
	}
	out, err := Output(ctx, dir, args...)
	if err != nil {
		return Patch{}, err
	}
	return ParsePatch(string(out)), nil
}

// ParsePatch parses the hunks of a single-file unified diff.
func ParsePatch(s string) Patch {
	var p Patch
	var h *Hunk
	var o, n int
	for _, l := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(l, "Binary files ") || strings.HasPrefix(l, "GIT binary patch"):
			p.Binary = true
		case strings.HasPrefix(l, "@@"):
			p.Hunks = append(p.Hunks, Hunk{Header: l})
			h = &p.Hunks[len(p.Hunks)-1]
			o, n = hunkStarts(l)
		case h == nil:
			// file header lines (diff --git, index, ---, +++)
		case strings.HasPrefix(l, "+"):
			h.Lines = append(h.Lines, Line{Kind: '+', New: n, Text: l[1:]})
			n++
		case strings.HasPrefix(l, "-"):
			h.Lines = append(h.Lines, Line{Kind: '-', Old: o, Text: l[1:]})
			o++
		case strings.HasPrefix(l, " "):
			h.Lines = append(h.Lines, Line{Kind: ' ', Old: o, New: n, Text: l[1:]})
			o++
			n++
		}
	}
	return p
}

// "@@ -84,14 +84,18 @@" -> 84, 84
func hunkStarts(h string) (int, int) {
	f := strings.Fields(h)
	if len(f) < 3 {
		return 0, 0
	}
	start := func(s string) int {
		s = strings.TrimLeft(s, "-+")
		s, _, _ = strings.Cut(s, ",")
		v, _ := strconv.Atoi(s)
		return v
	}
	return start(f[1]), start(f[2])
}

// remergeMark starts each commit in RemergeFiles' output: with -z a hash and a path are otherwise both plain fields.
const remergeMark = "\x01"

type MergeChanges struct {
	Files []string
	Own   bool
}

// RemergeFiles keeps resolved files for review and distinguishes new work from parent choices and conflict markers.
// It needs git ≥ 2.36 and re-merges in a temporary object directory, so the repository is not written.
func RemergeFiles(ctx context.Context, dir string, merges []Oid) (map[Oid]MergeChanges, error) {
	res := make(map[Oid]MergeChanges, len(merges))
	if len(merges) == 0 {
		return res, nil
	}
	args := []string{"show", "--remerge-diff", "--format=" + remergeMark + "%H", "--name-only", "-z", "--no-show-signature",
		"--no-ext-diff", "--no-textconv"}
	for _, m := range merges {
		args = append(args, m.String())
	}
	out, err := Output(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	var cur Oid
	for _, f := range bytes.Split(out, []byte{0}) {
		s := string(bytes.TrimLeft(f, "\n"))
		if s == "" {
			continue
		}
		if hash, ok := strings.CutPrefix(s, remergeMark); ok {
			if cur, err = ParseOid(hash); err != nil {
				return nil, fmt.Errorf("remerge output: %w", err)
			}
			res[cur] = MergeChanges{}
			continue
		}
		change := res[cur]
		change.Files = append(change.Files, s)
		res[cur] = change
	}
	var edited []string
	for _, merge := range merges {
		if len(res[merge].Files) > 0 {
			edited = append(edited, merge.String())
		}
	}
	if len(edited) == 0 {
		return res, nil
	}
	args = append([]string{"show", "--cc", "--format=" + remergeMark + "%H", "--no-renames", "--no-show-signature",
		"--no-ext-diff", "--no-textconv"}, edited...)
	out, err = Output(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	parents := 0
	var removed map[string][]bool
	for line := range strings.SplitSeq(string(out), "\n") {
		if hash, ok := strings.CutPrefix(line, remergeMark); ok {
			cur, err = ParseOid(hash)
			if err != nil {
				return nil, fmt.Errorf("combined diff output: %w", err)
			}
			parents = 0
			continue
		}
		change := res[cur]
		switch {
		case strings.HasPrefix(line, "diff --"):
			parents = 0
		case strings.HasPrefix(line, "@@@"):
			parents = len(line) - len(strings.TrimLeft(line, "@")) - 1
			removed = map[string][]bool{}
		case strings.HasPrefix(line, "Binary files "), strings.HasPrefix(line, "new file mode "), strings.HasPrefix(line, "deleted file mode "):
			change.Own = true
		case strings.HasPrefix(line, "mode "):
			before, after, ok := strings.Cut(strings.TrimPrefix(line, "mode "), "..")
			change.Own = change.Own || ok && !slices.Contains(strings.Split(before, ","), after)
		case parents > 0 && strings.HasPrefix(line, strings.Repeat("+", parents)):
			content := strings.TrimSpace(line[parents:])
			change.Own = change.Own || !conflictMarker(content)
		case parents > 0 && len(line) >= parents && strings.Contains(line[:parents], "-"):
			content := line[parents:]
			if conflictMarker(strings.TrimSpace(content)) {
				continue
			}
			if removed[content] == nil {
				removed[content] = make([]bool, parents)
			}
			for parent := range parents {
				removed[content][parent] = removed[content][parent] || line[parent] == '-'
			}
			change.Own = change.Own || !slices.Contains(removed[content], false)
		}
		res[cur] = change
	}
	return res, nil
}

func conflictMarker(line string) bool {
	if line == "=======" {
		return true
	}
	for _, marker := range []string{"<<<<<<<", "|||||||", ">>>>>>>"} {
		if line == marker || strings.HasPrefix(line, marker+" ") {
			return true
		}
	}
	return false
}

// RemergePatch is what a merge commit changed in path beyond merging its parents again (see RemergeFiles).
func RemergePatch(ctx context.Context, dir string, merge Oid, path string) (Patch, error) {
	out, err := Output(ctx, dir, patchArgs(ctx, "show", "--format=", "--remerge-diff", merge.String(), "--", path)...)
	if err != nil {
		return Patch{}, err
	}
	return ParsePatch(string(out)), nil
}

// DiffMergeBaseNumstat counts the lines of every file changed from the fork point of base and head to head (base...head).
func DiffMergeBaseNumstat(ctx context.Context, dir string, base, head Oid) ([]NumStat, error) {
	out, err := Output(ctx, dir, "diff", "--numstat", "-z", "-M", "--no-ext-diff", "--no-textconv", base.String()+"..."+head.String())
	if err != nil {
		return nil, err
	}
	return parseNumstatZ(out), nil
}

// DiffMergeBaseWorktreeNumstat counts the lines of every file changed from the merge base of base and HEAD to the working
// tree of worktree, uncommitted changes included. Untracked files are not in it.
func DiffMergeBaseWorktreeNumstat(ctx context.Context, worktree string, base Oid) ([]NumStat, error) {
	out, err := Output(ctx, worktree, "diff", "--numstat", "-z", "-M", "--no-ext-diff", "--no-textconv", "--merge-base", base.String())
	if err != nil {
		return nil, err
	}
	return parseNumstatZ(out), nil
}

// ShowNumstats counts the lines each commit changed against its first parent, all the commits in one call; a file
// several of them changed comes once per commit.
func ShowNumstats(ctx context.Context, dir string, oids []Oid) ([]NumStat, error) {
	if len(oids) == 0 {
		return nil, nil
	}
	args := []string{"show", "--numstat", "-z", "-M", "--format=", "--diff-merges=first-parent", "--no-ext-diff", "--no-textconv"}
	for _, o := range oids {
		args = append(args, o.String())
	}
	out, err := Output(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return parseNumstatZ(out), nil
}

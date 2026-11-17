// Package gitview reads what the index does not keep: line counts, diffs and
// full messages, asked of git when the user looks at a commit.
package gitview

import (
	"context"
	"slices"
	"strings"
	"sync"

	"sheaf/internal/gitcli"
	"sheaf/internal/index"
)

// FileChange is one changed file of a commit, or of several selected commits.
type FileChange struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"` // A M D R C T
	Add     int    `json:"add"`
	Del     int    `json:"del"`
	Binary  bool   `json:"binary,omitempty"`
	Commits int    `json:"commits"` // how many selected commits changed it (×N)
	// Latest is the newest selected commit that changed it (union only): where a click in the union shows its diff.
	Latest string `json:"latest,omitempty"`
}

// CommitFiles lists the files of commit i with line counts.
func CommitFiles(ctx context.Context, ix *index.Index, r *index.Repo, i int32) ([]FileChange, error) {
	files, stat := r.Files(i)
	out := make([]FileChange, len(files))
	pos := map[string]int{}
	for k, f := range files {
		p := ix.Paths[f]
		out[k] = FileChange{Repo: r.Name, Path: p, Status: string(stat[k]), Commits: 1}
		pos[p] = k
	}
	ns, err := gitcli.ShowNumstat(ctx, r.Dir, r.Oid(i))
	if err != nil {
		return out, err // still useful without counts
	}
	for _, n := range ns {
		if k, ok := pos[n.Path]; ok {
			out[k].Add, out[k].Del, out[k].Binary, out[k].OldPath = n.Add, n.Del, n.Binary, n.OldPath
		}
	}
	return out, nil
}

// UnionFiles merges the files of several commits, grouped by repo then path;
// a file changed by several commits sums its line counts..
func UnionFiles(ctx context.Context, ix *index.Index, refs []index.CommitRef) ([]FileChange, error) {
	per := make([][]FileChange, len(refs))
	errs := make([]error, len(refs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for k, c := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			per[k], errs[k] = CommitFiles(ctx, ix, ix.Repos[c.Repo], c.I)
		}()
	}
	wg.Wait()
	byKey := map[string]int{}
	var out []FileChange
	var newest []int64
	for k, fs := range per {
		c := refs[k]
		r := ix.Repos[c.Repo]
		at := r.Time[c.I]
		for _, f := range fs {
			key := f.Repo + "\x00" + f.Path
			if j, ok := byKey[key]; ok {
				out[j].Add += f.Add
				out[j].Del += f.Del
				out[j].Commits++
				if at > newest[j] {
					out[j].Latest, newest[j] = r.Oid(c.I).String(), at
				}
				continue
			}
			byKey[key] = len(out)
			f.Latest = r.Oid(c.I).String()
			out = append(out, f)
			newest = append(newest, at)
		}
	}
	slices.SortStableFunc(out, func(a, b FileChange) int {
		if c := strings.Compare(a.Repo, b.Repo); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	for _, e := range errs {
		if e != nil {
			return out, e
		}
	}
	return out, nil
}

// Patch is the diff of one file of commit i against its first parent.
func Patch(ctx context.Context, r *index.Repo, i int32, path, oldPath string) (gitcli.Patch, error) {
	return gitcli.ShowPatch(ctx, r.Dir, r.Oid(i), path, oldPath)
}

// Message is the full commit message; the body is everything after the subject.
func Message(ctx context.Context, r *index.Repo, i int32) (subject, body string, err error) {
	msg, err := gitcli.ShowMessage(ctx, r.Dir, r.Oid(i))
	if err != nil {
		return r.Subject[i], "", err
	}
	subject, body, _ = strings.Cut(msg, "\n")
	return subject, strings.TrimSpace(body), nil
}

// ComparePatch is the diff of one file from the fork point of base and head to head (base...head, three dots).
func ComparePatch(ctx context.Context, r *index.Repo, base, head int32, path, oldPath string) (gitcli.Patch, error) {
	return gitcli.DiffMergeBasePatch(ctx, r.Dir, r.Oid(base), r.Oid(head), path, oldPath)
}

// MergedPatch is one file's change on a branch checked out in worktree, from its merge base with base, including what is
// not committed yet. An untracked file is all added.
func MergedPatch(ctx context.Context, r *index.Repo, base int32, worktree, path, oldPath string, untracked bool) (gitcli.Patch, error) {
	if untracked {
		return UncommittedPatch(ctx, worktree, path, "", gitcli.Untracked)
	}
	return gitcli.DiffMergeBaseWorktree(ctx, worktree, r.Oid(base), path, oldPath)
}

// IgnoreSpace makes the patches asked for with the returned context ignore whitespace changes (git diff -w).
func IgnoreSpace(ctx context.Context, on bool) context.Context { return gitcli.IgnoreSpace(ctx, on) }

// Record makes the git commands run with the returned context be recorded in r (the command log).
func Record(ctx context.Context, r *gitcli.Recorder) context.Context {
	return gitcli.WithRecorder(ctx, r)
}

// NewLimit is a ceiling on the git processes running at once, shared by the contexts given it with Limit.
func NewLimit(n int) *gitcli.Limit { return gitcli.NewLimit(n) }

// Limit makes the git commands run with the returned context wait for a slot of l.
func Limit(ctx context.Context, l *gitcli.Limit) context.Context { return gitcli.WithLimit(ctx, l) }

// Source names, in one phrase, what the git commands run with the returned context are for; an outer source wins.
func Source(ctx context.Context, source string) context.Context {
	return gitcli.WithSource(ctx, source)
}

// GlabConfig makes the glab commands run with the returned context use the workspace's own glab configuration directory.
func GlabConfig(ctx context.Context, dir string) context.Context {
	return gitcli.WithGlabConfig(ctx, dir)
}

// CheckGlabConfig is nil when dir ("" = the system's) can be used as a glab configuration directory.
func CheckGlabConfig(dir string) error { _, err := gitcli.ResolveGlabConfig(dir); return err }

// CompareNumstat counts the lines of every file changed from the fork point of base and head to head.
func CompareNumstat(ctx context.Context, r *index.Repo, base, head int32) ([]gitcli.NumStat, error) {
	return gitcli.DiffMergeBaseNumstat(ctx, r.Dir, r.Oid(base), r.Oid(head))
}

// MergedNumstat counts the lines of every file changed on the branch checked out in worktree since its merge base with
// base, uncommitted changes included and untracked files not.
func MergedNumstat(ctx context.Context, r *index.Repo, base int32, worktree string) ([]gitcli.NumStat, error) {
	return gitcli.DiffMergeBaseWorktreeNumstat(ctx, worktree, r.Oid(base))
}

// CommitsNumstat counts the lines each of commits changed against its first parent, in one git call.
func CommitsNumstat(ctx context.Context, r *index.Repo, commits []int32) ([]gitcli.NumStat, error) {
	oids := make([]gitcli.Oid, len(commits))
	for i, c := range commits {
		oids[i] = r.Oid(c)
	}
	return gitcli.ShowNumstats(ctx, r.Dir, oids)
}

// PatchIDs is the patch-id of each non-merge commit of commits that changes something (see gitcli.PatchIDs).
func PatchIDs(ctx context.Context, r *index.Repo, oids []gitcli.Oid) (map[gitcli.Oid]string, error) {
	return gitcli.PatchIDs(ctx, r.Dir, oids)
}

// PatchCandidates lists the non-merge commits of line that changed one of paths and are not older than since (unix
// seconds): where a patch-equivalent copy of a commit that changed those paths at that time can be.
func PatchCandidates(ctx context.Context, r *index.Repo, line int32, since int64, paths []string) ([]gitcli.Oid, error) {
	return gitcli.LineCandidates(ctx, r.Dir, r.Oid(line), since, paths)
}

package diffwin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"sheaf/internal/native/diffview"
	"sheaf/internal/session"
)

// plan is what a DiffSource shows: the files the window steps through, in the order of their tree, how to read them and
// which one is first.
type plan struct {
	files   []diffview.File
	group   diffview.Grouper // the headings of the tree, nil for none
	load    diffview.Loader
	current int
	commit  *session.CommitDetail // kind commit: the commit the files belong to
	rows    []session.AggRepo
	stage   string
}

var errUnsupported = errors.New("unsupported diff source")

// planOf lists the files of src (src.Path is the one to show first) and gives their loader.
func planOf(ctx context.Context, s *session.Session, src session.DiffSource) (plan, error) {
	switch src.Kind {
	case "commit":
		k := session.CommitKey{Repo: src.Repo, Hash: src.Hash}
		changes, err := s.Files(ctx, []session.CommitKey{k})
		if err != nil {
			return plan{}, err
		}
		if len(changes) == 0 { // a merge commit lists none: show the one file asked for
			changes = []session.FileChange{{Repo: src.Repo, Path: src.Path, OldPath: src.OldPath, Status: "M"}}
		}
		p := plan{files: diffview.TreeOrder(diffview.FilesOf(changes), nil), load: diffview.CommitLoader(s, k)}
		p.current = indexOf(p.files, func(f diffview.File) bool { return f.Path == src.Path })
		if d, err := s.Commit(ctx, k); err == nil {
			p.commit = &d
		}
		return p, nil
	case "wc":
		fs, err := s.Uncommitted(ctx, src.Worktree)
		if err != nil {
			return plan{}, err
		}
		p := plan{load: diffview.UncommittedLoader(s, src.Worktree)}
		for _, f := range fs {
			p.files = append(p.files, diffview.File{Path: f.Path, OldPath: f.OldPath, Status: f.Status, Source: f})
		}
		p.files = diffview.TreeOrder(p.files, nil)
		if len(p.files) == 0 { // the changes went away since the window was opened
			return p, nil
		}
		p.current = indexOf(p.files, func(f diffview.File) bool {
			return f.Path == src.Path && (src.Area == "" || f.Source.(session.FileStatus).Area == src.Area)
		})
		return p, nil
	case "agg":
		return aggPlan(ctx, s, src)
	}
	return plan{}, fmt.Errorf("%w %q", errUnsupported, src.Kind)
}

// aggPlan steps through the files of the filter that src names (its row's, or every row's with All, under Dir, only the
// uncommitted ones with Only), the way the web does. The session gives the merged diff as hunks, shown as they are; a
// repo without a branch diff has one part per commit, all shown, oldest first, each under its commit.
func aggPlan(ctx context.Context, s *session.Session, src session.DiffSource) (plan, error) {
	if src.Agg == nil {
		return plan{}, fmt.Errorf("%w %q: no filter", errUnsupported, src.Kind)
	}
	in := *src.Agg
	if src.Only {
		in.DiffMode = session.DiffWorktree
	}
	all, err := s.TagAggFiles(ctx, in)
	if err != nil {
		return plan{}, err
	}
	row := src.Row
	if row == "" {
		row = src.Repo
	}
	dir := ""
	if src.Dir != "" {
		dir = strings.TrimRight(src.Dir, "/") + "/"
	}
	agg, err := s.TagAgg(ctx, in)
	if err != nil {
		return plan{}, fmt.Errorf("diff source: %w", err)
	}
	p := plan{rows: agg.Repos}
	if in.DiffMode == session.DiffStage {
		if flow := s.MergeFlow(); in.DiffStage >= 0 && in.DiffStage < len(flow) {
			p.stage = flow[in.DiffStage].Name
		}
	}
	if src.All {
		p.group = byRow
	}
	for _, f := range all {
		if !f.Worktree && (src.All || f.Key == row) && (dir == "" || strings.HasPrefix(f.Path, dir)) && (!src.Only || f.Uncommitted) {
			p.files = append(p.files, diffview.File{Path: f.Path, OldPath: f.OldPath, Status: f.Status, Source: f})
		}
	}
	p.files = diffview.TreeOrder(p.files, p.group)
	p.current = indexOf(p.files, func(f diffview.File) bool {
		return f.Path == src.Path && f.Source.(session.AggFile).Key == row
	})
	p.load = diffview.AggLoader(s, in)
	return p, nil
}

// byRow puts the files of a filter under the row whose commits or worktree changed them, named as the filter page
// names its rows (repo · worktree).
func byRow(f diffview.File) (string, string) {
	af, _ := f.Source.(session.AggFile)
	if af.Key == af.Repo || len(af.Key) <= len(af.Repo) {
		return af.Key, af.Repo
	}
	return af.Key, af.Repo + " · " + af.Key[len(af.Repo)+1:]
}

// indexOf is the first file that is wanted, 0 when none is.
func indexOf(files []diffview.File, want func(diffview.File) bool) int {
	for i, f := range files {
		if want(f) {
			return i
		}
	}
	return 0
}

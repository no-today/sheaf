package session

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sheaf/internal/gitops"
	"sheaf/internal/gitview"
)

// Dirty lists all working directories with uncommitted changes, grouped by repo.
// It runs git status in every directory that is not known to be clean, so the page is right even before Info caught up.
func (s *Session) Dirty(ctx context.Context) (DirtyView, error) {
	ctx = s.git(ctx, "dirtyView")
	ctx, done := s.latest.start(ctx, "dirty")
	defer done()
	var dirs []DirtyDir
	for _, r := range s.Info().Repos {
		for _, w := range r.Worktrees {
			if w.Dirty != 0 {
				dirs = append(dirs, DirtyDir{Repo: r.Name, Path: w.Path, Branch: w.Branch, Requirement: w.Requirement, Main: w.Main, OnMain: onBase(r.Base, w.Branch), Ahead: w.Ahead, Behind: w.Behind})
			}
		}
	}
	files := make([][]AggFile, len(dirs))
	errs := make([]error, len(dirs))
	err := parallel(ctx, len(dirs), aggFilesParallel, func(i int) {
		l, err := s.dirtyList(ctx, dirs[i].Repo, dirs[i].Path)
		if err != nil {
			errs[i] = fmt.Errorf("uncommitted files of %s: %w", dirs[i].Path, err)
			return
		}
		files[i] = l.files
		dirs[i].Dirty = len(l.files)
		dirs[i].LastChange = l.lastChange
		if dirs[i].Merging = gitops.Merging(dirs[i].Path); dirs[i].Merging {
			for _, f := range l.files {
				if f.Status == "U" {
					dirs[i].Conflicts++
				}
			}
		}
	})
	if err != nil {
		return DirtyView{Groups: []DirtyGroup{}, Files: []AggFile{}}, err
	}
	out := DirtyView{Files: []AggFile{}}
	var live []DirtyDir
	for i, d := range dirs {
		if errs[i] != nil {
			return DirtyView{Groups: []DirtyGroup{}, Files: []AggFile{}}, errs[i]
		}
		if d.Dirty > 0 {
			live = append(live, d)
			out.Files = append(out.Files, files[i]...)
		}
	}
	out.Groups = groupDirty(live)
	return out, nil
}

// dirtyList is the files of one working directory, from the cache unless the watch reported a change there since.
// A directory whose list was never read, or was invalidated, costs one git status, unless the count run that saw the change
// left its status behind.
func (s *Session) dirtyList(ctx context.Context, repo, path string) (dirtyList, error) {
	l, ok, epoch := s.dirty.listOf(path)
	if ok {
		return l, nil
	}
	var fs []FileStatus
	if st, ok := s.dirty.takeStatus(path, epoch); ok {
		fs = gitview.UncommittedOf(ctx, path, st)
	} else {
		var err error
		if fs, err = s.Uncommitted(ctx, path); err != nil {
			return dirtyList{}, err
		}
	}
	l = dirtyList{files: withUncommitted(repo, path, nil, fs)}
	for _, f := range l.files {
		if st, err := os.Stat(filepath.Join(path, f.Path)); err == nil {
			l.lastChange = max(l.lastChange, st.ModTime().Unix())
		}
	}
	s.dirty.storeList(path, epoch, l)
	return l, nil
}

// onBase tells whether branch is the base branch (origin/master → master).
func onBase(base, branch string) bool {
	if base == "" || branch == "" {
		return false
	}
	return branch == base || strings.HasSuffix(base, "/"+branch)
}

// groupDirty groups the directories by repo and sorts: directories by last change, groups by their newest directory.
func groupDirty(dirs []DirtyDir) []DirtyGroup {
	groups := []DirtyGroup{}
	at := map[string]int{}
	for _, d := range dirs {
		i, ok := at[d.Repo]
		if !ok {
			i = len(groups)
			at[d.Repo] = i
			groups = append(groups, DirtyGroup{Name: d.Repo})
		}
		g := &groups[i]
		g.Dirs = append(g.Dirs, d)
		g.Files += d.Dirty
		g.LastChange = max(g.LastChange, d.LastChange)
	}
	for i := range groups {
		slices.SortStableFunc(groups[i].Dirs, func(a, b DirtyDir) int {
			return cmp.Or(cmp.Compare(b.LastChange, a.LastChange), strings.Compare(a.Path, b.Path))
		})
	}
	slices.SortStableFunc(groups, func(a, b DirtyGroup) int {
		return cmp.Or(cmp.Compare(b.LastChange, a.LastChange), strings.Compare(a.Name, b.Name))
	})
	return groups
}

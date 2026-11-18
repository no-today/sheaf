package session

import (
	"path/filepath"
	"slices"
	"strings"

	"sheaf/internal/discover"
	"sheaf/internal/engine"
	"sheaf/internal/index"
)

// withoutIgnored splits the scan into the repos tracked and those the user does not track, keeping the scan's order.
// with says what each ignored repo took along: ignored name -> repos inside its directory (submodules, nested clones) that
// were not on the list themselves; a repo that took none has no key.
func withoutIgnored(repos []discover.Repo, ignored []string) (kept, dropped []discover.Repo, with map[string][]string) {
	with = map[string][]string{}
	kept = make([]discover.Repo, 0, len(repos))
	for _, r := range repos {
		if slices.Contains(ignored, r.Name) {
			dropped = append(dropped, r)
			continue
		}
		parent := slices.IndexFunc(repos, func(p discover.Repo) bool {
			return p.Dir != "" && slices.Contains(ignored, p.Name) && strings.HasPrefix(r.Dir, p.Dir+string(filepath.Separator))
		})
		if parent < 0 {
			kept = append(kept, r)
			continue
		}
		dropped = append(dropped, r)
		with[repos[parent].Name] = append(with[repos[parent].Name], r.Name)
	}
	return kept, dropped, with
}

// repoDirs are the git dirs and worktrees of repos, symlinks resolved like the watch reports them.
func repoDirs(repos []discover.Repo) []string {
	var out []string
	for _, r := range repos {
		out = append(out, resolvePath(r.GitDir))
		for _, w := range r.Worktrees {
			out = append(out, resolvePath(w.Path))
		}
	}
	return out
}

func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// dropUnder prevents activity in ignored repos from triggering a rescan. A tracked repo can have
// a linked worktree in an ignored repo's directory, so changes under keep still count.
func dropUnder(dirs, skip, keep []string) []string {
	if len(skip) == 0 {
		return dirs
	}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		d = filepath.Clean(d)
		if !under(d, skip) || under(d, keep) {
			out = append(out, d)
		}
	}
	return out
}

func under(d string, roots []string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return d == r || strings.HasPrefix(d, r+string(filepath.Separator)) })
}

// cleanNames trims, drops blanks and repeats, and keeps the order given.
func cleanNames(names []string) []string {
	out := []string{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// SetIgnored replaces the repos the user does not track and returns at once; the index follows. No scan: the repos of the
// last scan are filtered by the new list, and only a repo that comes back is read by git (its history is not in the index).
func (s *Session) SetIgnored(names []string) error {
	s.mu.Lock()
	prev := s.cfg.Ignored
	s.cfg.Ignored = cleanNames(names)
	err := s.saveConfig()
	if err != nil {
		s.cfg.Ignored = prev
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.retrack()
	return nil
}

// retrack applies the ignore list to the last scan. While an index update runs, it queues behind it
// so the running update cannot overwrite the change. Without a scan yet it refreshes the workspace.
func (s *Session) retrack() {
	if s.applyTracked(false) {
		return
	}
	s.Go(func() {
		if s.ctx.Err() == nil && !s.applyTracked(true) {
			s.Refresh(s.ctx)
		}
	})
}

func (s *Session) applyTracked(wait bool) bool {
	if wait {
		s.refresh.Lock()
	} else if !s.refresh.TryLock() {
		return false
	}
	defer s.refresh.Unlock()
	s.mu.Lock()
	if s.eng == nil || s.scanned == nil {
		s.mu.Unlock()
		return false
	}
	old := s.eng.Index()
	kept, dropped, with := withoutIgnored(s.scanned, s.cfg.Ignored)
	var paths []string
	for _, r := range kept {
		if !slices.ContainsFunc(old.Repos, func(p *index.Repo) bool { return p.GitDir == r.GitDir }) {
			for _, w := range r.Worktrees {
				paths = append(paths, w.Path)
			}
		}
	}
	s.repos = kept
	s.ignoredDirs = repoDirs(dropped)
	s.ignoredWith = with
	s.mu.Unlock()
	s.syncWatch(kept)
	ix, d, err := index.UpdateSome(s.git(s.ctx, "refresh"), old, kept, nil)
	if err != nil {
		return false
	}
	if len(d.Changed) > 0 {
		eng := engine.New(ix)
		s.mu.Lock()
		s.eng = eng
		s.indexGen++
		s.mu.Unlock()
		s.Go(func() { s.saveIndex() })
	}
	s.setState(func(st *State) {
		st.Commits = ix.Commits()
		st.Failed = mergeFailures(st.Failed, kept, nil, d)
	})
	if len(paths) > 0 {
		s.Go(func() { s.RefreshDirty(s.ctx, paths, false) })
	}
	s.indexUpdated(d.Changed)
	return true
}

// tookAlong tells whether an ignored repo's directory holds name.
func tookAlong(with map[string][]string, name string) bool {
	for _, names := range with {
		if slices.Contains(names, name) {
			return true
		}
	}
	return false
}

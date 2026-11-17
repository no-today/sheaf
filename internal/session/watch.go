package session

import (
	"context"
	"slices"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/engine"
	"sheaf/internal/index"
	"sheaf/internal/watch"
)

// Changes settle this long before anything is refreshed; a stream of changes
// is still handled at least every maxWait.
const (
	debounce = 300 * time.Millisecond
	maxWait  = 2 * time.Second
)

type watcher struct {
	roots []string
	stop  context.CancelFunc
}

// syncWatch (re)starts the file watch when the directories to cover changed.
// It never waits for the old loop: that loop may itself be waiting for the
// refresh lock our caller holds.
func (s *Session) syncWatch(repos []discover.Repo) {
	if s.opts.Watch == nil {
		return
	}
	dirs := []string{s.Root}
	for _, r := range repos {
		dirs = append(dirs, r.GitDir)
		for _, w := range r.Worktrees {
			dirs = append(dirs, w.Path)
		}
	}
	roots := watch.Roots(dirs)
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Equal(roots, s.watcher.roots) || s.ctx.Err() != nil {
		return
	}
	if s.watcher.stop != nil {
		s.watcher.stop()
	}
	ctx, stop := context.WithCancel(s.ctx)
	s.watcher = watcher{roots: roots, stop: stop}
	src, err := s.opts.Watch(ctx, roots)
	if err != nil {
		s.watcher.roots = nil // try again on the next refresh
		stop()
		return
	}
	s.Go(func() { s.watchLoop(ctx, src) })
}

// watchLoop gathers changed directories and applies them once they settle.
func (s *Session) watchLoop(ctx context.Context, src watch.Source) {
	defer src.Close()
	var pending []string
	var first time.Time
	var fire <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case dirs, ok := <-src.Events():
			if !ok {
				return
			}
			pending = append(pending, dirs...)
			if first.IsZero() {
				first = time.Now()
			}
			fire = time.After(max(0, min(debounce, maxWait-time.Since(first))))
		case <-fire:
			s.applyChanges(ctx, pending)
			pending, first, fire = nil, time.Time{}, nil
		}
	}
}

func (s *Session) applyChanges(ctx context.Context, dirs []string) {
	s.mu.Lock()
	dirs = dropUnder(dirs, s.ignoredDirs, repoDirs(s.repos))
	if len(dirs) == 0 {
		s.mu.Unlock()
		return
	}
	repos := make([]watch.Repo, len(s.repos))
	for i, r := range s.repos {
		repos[i] = watch.Repo{Name: r.Name, GitDir: r.GitDir}
		for _, w := range r.Worktrees {
			repos[i].Worktrees = append(repos[i].Worktrees, w.Path)
		}
	}
	s.mu.Unlock()
	c := watch.Classify(repos, dirs)
	if ctx.Err() != nil {
		return
	}
	// The work runs on the session's context, not the watch's: a refresh that changes the watched roots
	// restarts the watch, and must not cancel itself by doing so.
	work := s.git(s.ctx, "watch")
	if c.Rescan && s.outside.Changed(c.Outside) {
		s.Refresh(work)
		return
	}
	if len(c.Repos) > 0 {
		s.UpdateRepos(work, c.Repos)
	}
	if len(c.Worktrees) > 0 {
		s.RefreshDirty(work, c.Worktrees, false)
	}
}

// UpdateRepos re-reads the refs and worktrees of the named repos only, and
// pushes IndexUpdated when their history or worktree branches moved.
func (s *Session) UpdateRepos(ctx context.Context, names []string) error {
	ctx = s.git(ctx, "refresh")
	s.refresh.Lock()
	defer s.refresh.Unlock()
	s.mu.Lock()
	repos := slices.Clone(s.repos)
	eng := s.eng
	s.mu.Unlock()
	if eng == nil {
		return errNotReady
	}
	moved := false
	var paths []string
	for i, r := range repos {
		if slices.Contains(names, r.Name) {
			repos[i] = discover.Reload(ctx, r)
			moved = moved || !slices.Equal(r.Worktrees, repos[i].Worktrees)
			for _, w := range repos[i].Worktrees {
				if !slices.ContainsFunc(r.Worktrees, func(old discover.Worktree) bool { return old.Path == w.Path }) {
					paths = append(paths, w.Path)
				}
			}
		}
	}
	ix, d, err := index.UpdateSome(ctx, eng.Index(), repos, names)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.repos = repos
	// Update the last scan so ignore-list changes see the new worktrees. A refresh whose scan
	// began before this read also keeps them (freshenLocked).
	for _, r := range repos {
		if slices.Contains(names, r.Name) {
			if j := slices.IndexFunc(s.scanned, func(x discover.Repo) bool { return x.GitDir == r.GitDir }); j >= 0 {
				s.scanned[j].Worktrees = r.Worktrees
			}
			s.reloaded[r.GitDir] = s.scanSeq
		}
	}
	s.mu.Unlock()
	if moved { // e.g. `git worktree add ../x`: a worktree outside the watched roots
		s.syncWatch(repos)
	}
	if len(d.Changed) > 0 {
		next := engine.New(ix)
		s.mu.Lock()
		s.eng = next
		s.indexGen++
		s.mu.Unlock()
		err = s.saveIndex()
		s.setState(func(st *State) { st.Commits, st.Updated = ix.Commits(), s.opts.Now().Unix() })
	}
	if len(paths) > 0 {
		s.Go(func() { s.RefreshDirty(s.ctx, paths, false) })
	}
	changed := d.Changed
	if moved { // a checkout: branches in Info changed even when no commit is new
		changed = append(changed, names...)
	}
	if len(changed) > 0 {
		s.indexUpdated(changed)
	}
	return err
}

func (s *Session) indexUpdated(changed []string) {
	if s.opts.OnIndex == nil {
		return
	}
	changed = slices.Clone(changed)
	slices.Sort(changed)
	s.opts.OnIndex(IndexUpdate{ChangedRepos: slices.Compact(changed)})
}

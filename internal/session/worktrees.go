package session

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/engine"
	"sheaf/internal/gitcli"
	"sheaf/internal/gitview"
)

// abKey caches ahead/behind per (repo, base, head) for one index version.
type abKey struct {
	repo       string
	base, head int32
}

type abCache struct {
	eng *engine.Engine
	m   map[abKey][2]int
}

// aheadBehind of a worktree HEAD against the repo's base branch, from the index (no git).
// Caller holds s.mu.
func (s *Session) aheadBehind(repo string, head gitcli.Oid) (int, int) {
	base := s.cfg.Bases[repo]
	if base == "" {
		base = s.autoBaseLocked(repo)
	}
	if s.eng == nil || base == "" || head.IsZero() {
		return 0, 0
	}
	ix := s.eng.Index()
	id, ok := ix.RepoByName(repo)
	if !ok {
		return 0, 0
	}
	r := ix.Repos[id]
	h := r.Find(head)
	b, ok := r.Ref(base)
	if h < 0 || !ok {
		return 0, 0
	}
	if s.ab.eng != s.eng {
		s.ab = abCache{eng: s.eng, m: map[abKey][2]int{}}
	}
	k := abKey{repo, b, h}
	v, ok := s.ab.m[k]
	if !ok {
		a, bh := r.AheadBehind(b, h)
		v = [2]int{a, bh}
		s.ab.m[k] = v
	}
	return v[0], v[1]
}

// dirtyParallel bounds the git status processes of RefreshDirty.
const dirtyParallel = 4

// meParallel bounds the git config processes of deriveMe.
const meParallel = 8

// dirtyPushEvery spaces the pushes of one RefreshDirty: a large workspace has dozens of worktrees, and each push makes
// every view read its state again.
const dirtyPushEvery = 200 * time.Millisecond

// RefreshDirty runs git status in the given worktrees (all when nil), at most
// dirtyParallel at a time, and caches the counts for Info. OnWorktrees is told about
// worktrees whose diff fingerprint changed, or all of them when a write forces a report,
// as they are read: the first at once, the rest at most every dirtyPushEvery.
func (s *Session) RefreshDirty(ctx context.Context, paths []string, always bool) {
	ctx = s.git(ctx, "dirtyCount")
	s.dirty.invalidate(paths)
	if paths == nil {
		s.mu.Lock()
		for _, r := range s.repos {
			for _, w := range r.Worktrees {
				paths = append(paths, w.Path)
			}
		}
		s.mu.Unlock()
	}
	push := &dirtyPush{ctx: ctx, every: dirtyPushEvery, now: s.opts.Now, push: s.opts.OnWorktrees}
	_ = parallel(ctx, len(paths), dirtyParallel, func(i int) {
		p := paths[i]
		// A full refresh and a watch-triggered one may overlap: only the newest status may be stored.
		g := s.dirty.ticket()
		_, _, epoch := s.dirty.listOf(p)
		// Unknown would show the directory's stage as being read for good: a failed read counts it clean, as before
		// counts existed.
		failed := func() {
			if ctx.Err() == nil && s.dirty.settleUnread(p, g) {
				push.add(p, false)
			}
		}
		st, err := gitview.Status(ctx, p)
		if err != nil {
			failed()
			return
		}
		fingerprint, err := dirtyFingerprint(ctx, p, st)
		if err != nil {
			failed()
			return
		}
		s.dirty.putStatus(p, epoch, st)
		differs, first, stale := s.dirty.store(p, g, st.Count(), fingerprint)
		switch {
		case stale:
		case always || differs:
			push.add(p, true)
		case first:
			push.add(p, false)
		}
	})
	push.flush()
}

// dirtyPush gathers the worktrees one RefreshDirty read and hands them to OnWorktrees in batches.
type dirtyPush struct {
	ctx   context.Context
	every time.Duration
	now   func() time.Time
	push  func(WorktreeStatus) // nil: nobody listens

	mu      sync.Mutex
	last    time.Time // of the last push; zero until the first, which goes out at once
	pending WorktreeStatus
}

// add records a worktree whose diff changed (changed) or whose status was read for the first time,
// and pushes what is pending when the last push is long enough ago.
func (b *dirtyPush) add(path string, changed bool) {
	b.mu.Lock()
	if changed {
		b.pending.Worktrees = append(b.pending.Worktrees, path)
	} else {
		b.pending.Read = append(b.pending.Read, path)
	}
	due := b.last.IsZero() || b.now().Sub(b.last) >= b.every
	b.mu.Unlock()
	if due {
		b.flush()
	}
}

// flush pushes what is pending, unless the refresh was cancelled.
func (b *dirtyPush) flush() {
	b.mu.Lock()
	ws := b.pending
	b.pending = WorktreeStatus{}
	if len(ws.Worktrees)+len(ws.Read) > 0 {
		b.last = b.now()
	}
	b.mu.Unlock()
	if len(ws.Worktrees)+len(ws.Read) == 0 || b.ctx.Err() != nil || b.push == nil {
		return
	}
	b.push(ws)
}

// worktreeKnown: only worktrees of this workspace may be read.
func (s *Session) worktreeKnown(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		for _, w := range r.Worktrees {
			if w.Path == path {
				return true
			}
		}
	}
	return false
}

// Branches lists a repo's local branches, remote branches and tags (in that
// order, by name), marking those checked out in a worktree.
func (s *Session) Branches(repo string) ([]BranchView, error) {
	eng, _, _ := s.current()
	if eng == nil {
		return nil, errNotReady
	}
	id, ok := eng.Index().RepoByName(repo)
	if !ok {
		return nil, fmt.Errorf("unknown repo %q", repo)
	}
	checked := map[string]string{}
	s.mu.Lock()
	for _, r := range s.repos {
		if r.Name == repo {
			for _, w := range r.Worktrees {
				if w.Branch != "" {
					checked[w.Branch] = w.Path
				}
			}
		}
	}
	s.mu.Unlock()
	out := []BranchView{}
	for _, kind := range []struct{ prefix, name string }{{"refs/heads/", "local"}, {"refs/remotes/", "remote"}, {"refs/tags/", "tag"}} {
		for _, ref := range eng.Index().Repos[id].Refs {
			if n, ok := strings.CutPrefix(ref.Name, kind.prefix); ok {
				out = append(out, BranchView{Name: n, Kind: kind.name, Worktree: checked[ref.Name]})
			}
		}
	}
	return out, nil
}

func (s *Session) Uncommitted(ctx context.Context, worktree string) ([]FileStatus, error) {
	ctx = s.git(ctx, "uncommitted")
	if !s.worktreeKnown(worktree) {
		return nil, fmt.Errorf("unknown worktree %q", worktree)
	}
	fs, err := gitview.Uncommitted(ctx, worktree)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range fs {
		seen[f.Path] = true
	}
	s.dirty.set(worktree, len(seen))
	return fs, nil
}

// UncommittedPatch is the diff of one uncommitted file; area is staged, unstaged or untracked.
func (s *Session) UncommittedPatch(ctx context.Context, worktree, path, oldPath, area string, ignoreSpace bool) (Patch, error) {
	ctx = s.git(ctx, "uncommittedPatch")
	ctx = gitview.IgnoreSpace(ctx, ignoreSpace)
	if !s.worktreeKnown(worktree) {
		return Patch{}, fmt.Errorf("unknown worktree %q", worktree)
	}
	p, err := gitview.UncommittedPatch(ctx, worktree, path, oldPath, area)
	if err != nil {
		return Patch{}, err
	}
	return patchOf(p, path), nil
}

// UncommittedTexts is both sides of one uncommitted file in full, for a diff view that lays them out itself: staged is
// HEAD against the index, unstaged the index (HEAD for a conflict, which has no single index entry) against the file on
// disk, untracked nothing against it, head HEAD against it. status (FileStatus.Status) leaves out the side an added or
// deleted file does not have.
func (s *Session) UncommittedTexts(ctx context.Context, worktree, path, oldPath, area, status string) (DiffTexts, error) {
	ctx = s.git(ctx, "uncommittedTexts")
	if !s.worktreeKnown(worktree) {
		return DiffTexts{}, fmt.Errorf("unknown worktree %q", worktree)
	}
	var oldSide, newSide string
	switch area {
	case "staged":
		oldSide, newSide = "head", "index"
	case "unstaged":
		oldSide, newSide = "index", "worktree"
		if status == "U" {
			oldSide = "head"
		}
	case "untracked":
		newSide = "worktree"
	case "head":
		oldSide, newSide = "head", "worktree"
	default:
		return DiffTexts{}, fmt.Errorf("unknown area %q", area)
	}
	if status == "A" {
		oldSide = ""
	}
	if status == "D" {
		newSide = ""
	}
	read := func(side, p string) (FileText, error) {
		if side == "" {
			return FileText{Lines: []string{}}, nil
		}
		lines, bin, cut, err := gitview.UncommittedText(ctx, worktree, side, p)
		if lines == nil {
			lines = []string{}
		}
		return FileText{Lines: lines, Binary: bin, Cut: cut}, err
	}
	from := path
	if oldPath != "" {
		from = oldPath
	}
	var out DiffTexts
	var err error
	if out.Old, err = read(oldSide, from); err != nil {
		return DiffTexts{}, err
	}
	if out.New, err = read(newSide, path); err != nil {
		return DiffTexts{}, err
	}
	return out, nil
}

// meLocked is what "me" matches: the emails derived from git config (the global user.email plus the one each repo
// commits with: a work address often lives only in includeIf or the repo config) and the ones added by hand.
func (s *Session) meLocked() []string {
	out := []string{}
	for _, f := range s.meAuto {
		out = append(out, f.Email)
	}
	for _, e := range s.cfg.MeManual {
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// defaultMe derives the "me" emails again; a change re-runs queries.
func (s *Session) defaultMe(ctx context.Context, repos []discover.Repo) {
	from := deriveMe(ctx, repos)
	s.mu.Lock()
	before := s.meLocked()
	s.meAuto = from
	changed := !slices.Equal(before, s.meLocked())
	s.mu.Unlock()
	if changed {
		s.indexUpdated(nil) // "me" queries give other results now
	}
}

// deriveMe lists the derived "me" emails (lowercased, global first) with their origin.
func deriveMe(ctx context.Context, repos []discover.Repo) []MeFrom {
	global := strings.ToLower(discover.UserEmail(ctx))
	var out []MeFrom
	if global != "" {
		out = append(out, MeFrom{Email: global, Global: true, Repos: []string{}})
	}
	emails := make([]string, len(repos))
	_ = parallel(ctx, len(repos), meParallel, func(i int) {
		emails[i] = strings.ToLower(discover.RepoUserEmail(ctx, repos[i].Dir))
	})
	for i, e := range emails {
		if e == "" || e == global {
			continue
		}
		if j := slices.IndexFunc(out, func(f MeFrom) bool { return f.Email == e }); j >= 0 {
			out[j].Repos = append(out[j].Repos, repos[i].Name)
		} else {
			out = append(out, MeFrom{Email: e, Repos: []string{repos[i].Name}})
		}
	}
	if out == nil {
		out = []MeFrom{}
	}
	return out
}

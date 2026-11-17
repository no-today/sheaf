package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"sheaf/internal/discover"
	"sheaf/internal/engine"
	"sheaf/internal/gitcli"
	"sheaf/internal/gitview"
	"sheaf/internal/index"
	"sheaf/internal/query"
)

// The Tag view: what one Tag hit, per repo, against a base branch.
// TagAgg reads only the index; TagAggFiles and TagAggPatch ask git.

// A file diffed per commit shows at most this many of the Tag's commits.
const maxAggParts = 50

// aggRepo is one repo of a Tag: the view plus what the git calls need.
type aggRepo struct {
	mode    string
	deleted bool
	view    AggRepo
	r       *index.Repo
	commits []int32 // the Tag's commits here, newest first
	scope   []int32 // the commits the row counts and lists: on the branch and not in the base when the row compares, else commits
	files   []int32 // the commits the file list takes files from: scope, without merge commits
	base    int32   // -1 when there is no usable base
	branch  int32   // -1 when there is no feature branch
	// The merge commits of scope that files leaves out: those known to be edited (file list: their files) and those not
	// classified yet (TagAggRemerge).
	hands   map[int32][]string
	unknown []int32
	// The requirement's own commits in this row (planStage): its non-merge commits and the merges known to be edited.
	own []int32
	// noOwn: the row has none, so it takes no part in the stage of a requirement.
	noOwn bool
	// named tells whether a branch name carries the requirement the rows are read for (nil: not read for one).
	named func(branch string) bool
	// jobs are the patch comparisons planStage lacked the answer to; RequirementsSettle runs them.
	jobs []patchJob
	// paths names the files of the index the commits' file numbers point into.
	paths []string
}

// aggTag is what a view reads: a name for the page head and the conditions that pick the commits.
type aggTag struct {
	Name string
	Cond query.Conditions
}

type aggPlan struct {
	eng   *engine.Engine
	tag   aggTag
	m     *query.Matcher
	rows  []index.CommitRef
	repos []aggRepo // most commits first
}

func (s *Session) aggPlan(in TagAggInput) (*aggPlan, error) {
	eng, env, bases := s.current()
	if in.Requirement != "" {
		s.mu.Lock()
		pattern := s.cfg.Requirement
		s.mu.Unlock()
		re, err := compileRequirement(pattern)
		if err != nil {
			return nil, err
		}
		in.keep = requirementKeep(re, in.Requirement)
		in.named = requirementNamed(re, in.Requirement)
	}
	return s.planAgg(in, eng, env, bases)
}

func (s *Session) planAgg(in TagAggInput, eng *engine.Engine, env query.Env, bases engine.Bases) (*aggPlan, error) {
	if eng == nil {
		return nil, errNotReady
	}
	if in.Cond == nil {
		return nil, errors.New("no conditions")
	}
	tag := aggTag{Name: strings.Join(in.Cond.Branches, ", "), Cond: *in.Cond}
	ix := eng.Index()
	m, err := query.Compile(tag.Cond, env)
	if err != nil {
		return nil, fmt.Errorf("tag %s: %w", tag.Name, err)
	}
	rows := eng.Run(m, bases).Rows
	if in.keep != nil {
		rows = slices.DeleteFunc(rows, func(c index.CommitRef) bool { return !in.keep(ix.Repos[c.Repo], c.I) })
	}
	release := in.Release
	if release == "" {
		s.mu.Lock()
		release = s.cfg.Release
		s.mu.Unlock()
	}
	release = cmp.Or(release, DefaultRelease)
	per := map[index.RepoID][]int32{}
	var ids []index.RepoID
	for _, c := range rows {
		if per[c.Repo] == nil {
			ids = append(ids, c.Repo)
		}
		per[c.Repo] = append(per[c.Repo], c.I)
	}
	wts := s.worktrees()
	dirtyBranches := map[index.RepoID]string{}
	if in.named != nil {
		for repoID, r := range ix.Repos {
			id := index.RepoID(repoID)
			if len(per[id]) > 0 {
				continue
			}
			for _, wt := range wts[r.Name] {
				if in.named(wt.Branch) && s.dirty.get(wt.Path) > 0 {
					ids = append(ids, id)
					dirtyBranches[id] = strings.TrimPrefix(wt.Branch, "refs/heads/")
					break
				}
			}
		}
	}
	p := &aggPlan{eng: eng, tag: tag, m: m, rows: rows}
	for _, id := range ids {
		r := ix.Repos[id]
		baseName := bases.Resolve(r.Name, in.Base)
		var mine []string
		for _, b := range tagBranches(tag.Cond) {
			if hasRef(r, b) {
				mine = append(mine, b)
			}
		}
		if len(mine) < 2 {
			tagBranch := dirtyBranches[id]
			if len(mine) == 1 {
				tagBranch = mine[0]
			}
			a := planRepo(r, per[id], per[id], baseName, bases[r.Name], in.Branches[r.Name], tagBranch, release, wts[r.Name], in.named)
			a.view.Key = r.Name
			p.repos = append(p.repos, a)
			continue
		}
		// Several of the Tag's branches in one repo: a row each, with the Tag's commits on that branch.
		base, _ := r.Ref(baseName)
		if baseName == "" {
			base = -1
		}
		for _, b := range mine {
			key := RowKey(r.Name, b)
			head, _ := r.Ref(b)
			on := r.Range(head, base)
			commits := slices.DeleteFunc(slices.Clone(per[id]), func(c int32) bool { return !on.Has(c) })
			a := planRepo(r, commits, per[id], baseName, bases[r.Name], in.Branches[key], b, release, wts[r.Name], in.named)
			a.view.Key = key
			p.repos = append(p.repos, a)
		}
	}
	for k := range p.repos {
		a := &p.repos[k]
		a.named, a.paths = in.named, ix.Paths
		s.planRemerge(a)
		if !a.deleted {
			a.view.Files = countPaths(ix, a.r, a.files, a.handPaths())
		}
		if !a.deleted && a.named != nil {
			for _, wt := range wts[a.r.Name] {
				if a.named(wt.Branch) {
					a.view.Worktree = wt.Path
					break
				}
			}
		}
		if w := p.repos[k].view.Worktree; w != "" {
			p.repos[k].view.Uncommitted = s.dirty.get(w)
		}
		s.planStage(a)
	}
	slices.SortStableFunc(p.repos, func(a, b aggRepo) int {
		return cmp.Or(b.view.Commits-a.view.Commits, cmp.Compare(a.view.Repo, b.view.Repo), cmp.Compare(a.view.Key, b.view.Key))
	})
	return p, nil
}

// RowKey names the row of one of a Tag's branches in a repo that has several of them; a repo with one row is keyed by its name.
func RowKey(repo, branch string) string { return repo + "\x00" + branch }

// scoped is the Tag's commits in the scope of row ("" = of every row), in the order of p.rows.
func (p *aggPlan) scoped(row string) []index.CommitRef {
	ix := p.eng.Index()
	in := map[index.CommitRef]bool{}
	for _, a := range p.repos {
		if row != "" && a.view.Key != row {
			continue
		}
		id := index.RepoID(slices.Index(ix.Repos, a.r))
		for _, c := range a.scope {
			in[index.CommitRef{Repo: id, I: c}] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(p.rows), func(c index.CommitRef) bool { return !in[c] })
}

func (p *aggPlan) row(key string) (aggRepo, bool) {
	i := slices.IndexFunc(p.repos, func(a aggRepo) bool { return a.view.Key == key })
	if i < 0 {
		return aggRepo{}, false
	}
	return p.repos[i], true
}

func (s *Session) worktrees() map[string][]discover.Worktree {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]discover.Worktree, len(s.repos))
	for _, r := range s.repos {
		out[r.Name] = slices.Clone(r.Worktrees)
	}
	return out
}

// planRepo settles one row of the Tag view. The feature branch is, in order: the one chosen in the view
// (override), the Tag's branch (tagBranch), else inferred: the local branch (then remote) holding most of the Tag's
// commits that are not in the base; ties go to a branch checked out in a worktree, then to the newest tip.
// mainName is the repo's own base branch, whatever the compare target is.
func planRepo(r *index.Repo, commits, candidates []int32, baseName, mainName, override, tagBranch, release string, wts []discover.Worktree, named func(string) bool) aggRepo {
	a := aggRepo{r: r, commits: commits, scope: commits, files: commits, base: -1, branch: -1}
	a.view = AggRepo{Repo: r.Name, Commits: len(commits), Base: baseName, BranchFrom: FromNone, Main: mainName}
	switch b, ok := r.Ref(baseName); {
	case baseName == "":
		a.view.Skip = SkipBaseUnset
	case !ok:
		a.view.Skip = SkipNoBase
	default:
		a.base = b
	}
	checkedOut := map[string]bool{}
	for _, w := range wts {
		if w.Branch != "" {
			checkedOut[w.Branch] = true
		}
	}
	switch {
	case tagBranch != "" && (named == nil || named(tagBranch)):
		a.view.Branch, a.view.BranchFrom = tagBranch, FromTag
	case named != nil || a.base >= 0:
		a.view.Branch, a.view.BranchFrom = inferBranch(r, a.base, baseName, release, commits, checkedOut, named)
	}
	a.view.Auto = a.view.Branch
	if override != "" && (named == nil || named(override) && hasRef(r, override)) {
		a.view.Branch, a.view.BranchFrom = override, FromOverride
	}
	if named != nil && a.view.Branch == "" {
		a.deleted = !slices.ContainsFunc(r.Refs, func(ref index.Ref) bool {
			return (strings.HasPrefix(ref.Name, "refs/heads/") || strings.HasPrefix(ref.Name, "refs/remotes/")) &&
				!strings.HasSuffix(ref.Name, "/HEAD") && named(ref.Name)
		})
		if a.deleted {
			a.view.BranchFrom = FromMerged
			return a
		}
	}
	if a.view.BranchFrom == FromMerged {
		a.view.MergedBranch = mergedBranchName(r, commits, a.base, baseName, release, named)
	}
	if a.view.Branch == "" {
		return a
	}
	h, ok := r.Ref(a.view.Branch)
	if !ok {
		a.view.Skip = cmp.Or(a.view.Skip, SkipNoHead)
		return a
	}
	a.branch = h
	if a.base < 0 {
		return a
	}
	a.view.Ahead, a.view.Behind = r.AheadBehind(a.base, h)
	// The merged diff is merge-base → branch: a commit adds a file to it only when it is on the branch and not in the base
	// (the Tag's commits in this repo also cover other branches and the base's history). The commit count and the commits
	// tab take the same commits, so the title bar, the table and the tab agree.
	on := r.Range(h, a.base)
	a.scope = slices.DeleteFunc(slices.Clone(commits), func(c int32) bool { return !on.Has(c) })
	if a.view.BranchFrom != FromOverride && a.view.Ahead == 0 {
		if scope := mergedScope(r, candidates, h, a.base); len(scope) > 0 {
			a.commits, a.scope = scope, scope
			a.files = slices.DeleteFunc(slices.Clone(scope), func(c int32) bool { return len(r.Parents(c)) > 1 })
			a.branch = -1
			a.view.MergedBranch = a.view.Branch
			a.view.Branch, a.view.Auto, a.view.BranchFrom = "", "", FromMerged
			a.view.Ahead, a.view.Behind, a.view.Commits = 0, 0, len(scope)
			return a
		}
	}
	// A merge commit's files are what it brought in from its other parent (the base, for 合并主分支): not in the merged diff.
	a.files = slices.DeleteFunc(slices.Clone(a.scope), func(c int32) bool { return len(r.Parents(c)) > 1 })
	a.view.Commits = len(a.scope)
	for _, w := range wts {
		if w.Branch == "refs/heads/"+a.view.Branch {
			a.view.Worktree, a.view.Uncommitted = w.Path, -1
			break
		}
	}
	return a
}

func mergedScope(r *index.Repo, commits []int32, h, base int32) []int32 {
	on, ok := r.MergedRange(h, base)
	if !ok {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(commits), func(c int32) bool { return !on.Has(c) })
}

var mergeSubject = regexp.MustCompile(`^Merge (?:remote-tracking )?branch '([^']+)'`)

// mergedBranchName names the feature branch of commits (newest first), all of them in base: a branch still pointing at one
// of them, else the merge that brought the newest in, whose subject outlives a branch deleted on merging. "" when neither says.
func mergedBranchName(r *index.Repo, commits []int32, base int32, baseName, release string, named func(string) bool) string {
	if len(commits) == 0 {
		return ""
	}
	isLine := func(name string) bool {
		return named != nil && !named(name) || name == branchName(r, baseName) || name == branchName(r, release)
	}
	own := make(map[int32]bool, len(commits))
	for _, c := range commits {
		own[c] = true
	}
	for _, prefix := range []string{"refs/heads/", "refs/remotes/"} {
		best, at := "", int64(-1)
		for _, ref := range r.Refs {
			if !strings.HasPrefix(ref.Name, prefix) || strings.HasSuffix(ref.Name, "/HEAD") || !own[ref.Commit] || isLine(branchName(r, ref.Name)) {
				continue
			}
			if t := r.Time[ref.Commit]; t > at {
				best, at = strings.TrimPrefix(ref.Name, prefix), t
			}
		}
		if best != "" {
			return best
		}
	}
	if m := r.MergedBy(commits[0], base); m >= 0 {
		if sub := mergeSubject.FindStringSubmatch(r.Subject[m]); sub != nil && !isLine(sub[1]) {
			return sub[1]
		}
	}
	return ""
}

// tagBranches are the branches a Tag picks; excluded branches are not feature branches.
func tagBranches(c query.Conditions) []string {
	if c.BranchNot {
		return nil
	}
	return c.Branches
}

func hasRef(r *index.Repo, name string) bool {
	_, ok := r.Ref(name)
	return ok
}

// inferBranch returns the branch that holds most of commits beyond base, and FromInferred; FromMerged when the base
// has them all; FromNone when no branch does (only a detached HEAD or a tag reaches them).
func inferBranch(r *index.Repo, base int32, baseName, release string, commits []int32, checkedOut map[string]bool, named func(string) bool) (string, string) {
	releaseBranch := branchName(r, release)
	var inBase index.Set
	if base >= 0 {
		inBase = r.RangeAll(baseHeads(r, base, baseName), -1)
	}
	var pending []int32
	for _, c := range commits {
		if !inBase.Has(c) {
			pending = append(pending, c)
		}
	}
	if len(pending) == 0 {
		return "", FromMerged
	}
	if named != nil {
		pending, base = commits, -1
	}
	for _, prefix := range []string{"refs/heads/", "refs/remotes/"} {
		var names []string
		var tips []int32
		for _, ref := range r.Refs {
			if !strings.HasPrefix(ref.Name, prefix) || strings.HasSuffix(ref.Name, "/HEAD") || branchName(r, ref.Name) == releaseBranch {
				continue
			}
			if named != nil && !named(ref.Name) || named == nil && inBase.Has(ref.Commit) {
				continue
			}
			names = append(names, ref.Name)
			tips = append(tips, ref.Commit)
		}
		best := -1
		counts := r.Holding(tips, base, pending)
		for k := range tips {
			if counts[k] == 0 && named == nil {
				continue
			}
			if best < 0 || cmp.Or(
				cmp.Compare(counts[k], counts[best]),
				boolCmp(checkedOut[names[k]], checkedOut[names[best]]),
				cmp.Compare(r.Time[tips[k]], r.Time[tips[best]]),
				cmp.Compare(names[best], names[k]),
			) > 0 {
				best = k
			}
		}
		if best >= 0 {
			return strings.TrimPrefix(names[best], prefix), FromInferred
		}
	}
	if named != nil {
		return "", FromMerged
	}
	return "", FromNone
}

func remoteBranch(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, "refs/remotes/")
	if !ok {
		return "", false
	}
	_, branch, ok := strings.Cut(rest, "/")
	return branch, ok
}

func branchName(r *index.Repo, name string) string {
	if b, ok := strings.CutPrefix(name, "refs/heads/"); ok {
		return b
	}
	if b, ok := remoteBranch(name); ok {
		return b
	}
	for _, ref := range r.Refs {
		if ref.Name == "refs/remotes/"+name {
			b, _ := remoteBranch(ref.Name)
			return b
		}
	}
	return name
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// countPaths counts the distinct files of commits and of extra (paths).
func countPaths(ix *index.Index, r *index.Repo, commits []int32, extra []string) int {
	seen := map[uint32]bool{}
	for _, c := range commits {
		files, _ := r.Files(c)
		for _, f := range files {
			seen[f] = true
		}
	}
	n := len(seen)
	if len(extra) == 0 {
		return n
	}
	known := make(map[string]bool, len(seen))
	for f := range seen {
		known[ix.Paths[f]] = true
	}
	for _, path := range extra {
		if !known[path] {
			n++
		}
	}
	return n
}

// TagAgg is the overview of a Tag: per repo its branch, worktree and counts, from the index alone.
func (s *Session) TagAgg(ctx context.Context, in TagAggInput) (TagAggResult, error) {
	p, err := s.aggPlan(in)
	if err != nil {
		return TagAggResult{}, err
	}
	t := p.tag
	out := TagAggResult{Tag: TagView{Name: t.Name, Cond: t.Cond}, Repos: []AggRepo{}, Commits: len(p.scoped(""))}
	out.Tag.Count = len(p.rows)
	out.ResultID = s.results.put(&result{eng: p.eng, m: p.m, rows: p.rows})
	for _, a := range p.repos {
		out.Repos = append(out.Repos, a.view)
		out.Files += a.view.Files
		out.Uncommitted += max(a.view.Uncommitted, 0)
	}
	return out, nil
}

// aggFilesParallel bounds the rows whose git status and numstat TagAggFiles reads at once.
const aggFilesParallel = 8

// TagAggFiles lists each repo's committed files in path order. Base and stage modes append a separate HEAD-to-worktree
// list; worktree mode returns only that list. A branch diff keeps only paths in its net numstat; if numstat cannot be
// read, its full committed file list stays available.
func (s *Session) TagAggFiles(ctx context.Context, in TagAggInput) ([]AggFile, error) {
	ctx = s.git(ctx, "aggFiles")
	ctx, done := s.latest.start(ctx, "aggFiles")
	defer done()
	return s.aggFiles(ctx, in, aggFilesParallel)
}

// prefetchParallel keeps a prefetch to a corner of the git limit, so a requirement the user opens meanwhile is not
// queued behind it.
const prefetchParallel = 2

// PrefetchTagAgg reads in the background what TagAggFiles and TagAggStats of each input need from git, so that opening
// one of them later is answered from the caches. It is never superseded by those calls, nor supersedes them.
func (s *Session) PrefetchTagAgg(ctx context.Context, ins []TagAggInput) {
	ctx = s.git(ctx, "aggPrefetch")
	for _, in := range ins {
		if ctx.Err() != nil {
			return
		}
		_, _ = s.aggFiles(ctx, in, prefetchParallel)
	}
}

// DiffAvail is how many files each way of viewing the changes shows (TagAggFiles of that DiffMode), so the empty ones
// can be told before they are picked; -1 where it could not be read.
type DiffAvail struct {
	Base     int   `json:"base"`
	Worktree int   `json:"worktree"`
	Stages   []int `json:"stages"` // per MergeFlow step
}

// TagAggDiffAvail counts the files of in under every DiffMode and merge flow step, whatever in.DiffMode is.
func (s *Session) TagAggDiffAvail(ctx context.Context, in TagAggInput) (DiffAvail, error) {
	ctx = s.git(ctx, "aggAvail")
	ctx, done := s.latest.start(ctx, "aggAvail")
	defer done()
	count := func(mode string, stage int) int {
		in := in
		in.DiffMode, in.DiffStage = mode, stage
		files, err := s.aggFiles(ctx, in, aggFilesParallel)
		if err != nil {
			return -1
		}
		return len(files)
	}
	out := DiffAvail{Base: count(DiffBase, 0), Worktree: count(DiffWorktree, 0)}
	for i := range s.MergeFlow() {
		out.Stages = append(out.Stages, count(DiffStage, i))
	}
	return out, ctx.Err()
}

func (s *Session) aggFiles(ctx context.Context, in TagAggInput, par int) ([]AggFile, error) {
	p, err := s.diffPlan(ctx, in)
	if err != nil {
		return []AggFile{}, err
	}
	ix := p.eng.Index()
	per := make([][]AggFile, len(p.repos))
	errs := make([]error, len(p.repos))
	for k, a := range p.repos {
		per[k] = withResolved(a.r.Name, a.view.Key, committedFiles(ix, a.r, a.view.Key, a.files, a.mode != DiffStage), a.hands)
	}
	err = parallel(ctx, len(p.repos), par, func(k int) {
		a := &p.repos[k]
		if a.mode == DiffWorktree {
			per[k], errs[k] = s.worktreeDiffFiles(ctx, a)
			return
		}
		if a.mode == DiffBase || a.mode == DiffStage {
			defer func() {
				files, err := s.worktreeDiffFiles(ctx, a)
				if err != nil {
					errs[k] = fmt.Errorf("uncommitted files of %s: %w", a.view.Repo, err)
					return
				}
				for i := range files {
					files[i].Worktree = true
				}
				per[k] = append(per[k], files...)
			}()
		} else if a.view.Worktree != "" {
			fs, err := s.Uncommitted(ctx, a.view.Worktree)
			if err != nil {
				errs[k] = fmt.Errorf("uncommitted files of %s: %w", a.view.Repo, err)
				return
			}
			per[k] = withUncommitted(a.view.Repo, a.view.Key, per[k], fs)
		}
		if a.base < 0 || a.branch < 0 {
			return
		}
		stats, err := s.rowStats(ctx, a)
		if err != nil {
			return
		}
		paths := make(map[string]bool, len(stats))
		for _, st := range stats {
			paths[st.Path] = true
		}
		per[k] = slices.DeleteFunc(per[k], func(f AggFile) bool { return !f.Untracked && !paths[f.Path] })
	})
	if err != nil {
		return []AggFile{}, err
	}
	out := []AggFile{}
	for _, fs := range per {
		out = append(out, fs...)
	}
	for _, e := range errs {
		if e != nil {
			return out, e
		}
	}
	return out, nil
}

// committedFiles is the union of the files of commits (newest first), by path, excluding paths first added and last
// deleted. A file one commit added stays A, one the newest deleted is D, else the newest commit's status.
func committedFiles(ix *index.Index, r *index.Repo, key string, commits []int32, net bool) []AggFile {
	at := map[string]int{}
	added := map[string]bool{}
	var out []AggFile
	for k := len(commits) - 1; k >= 0; k-- {
		files, stat := r.Files(commits[k])
		for j, f := range files {
			path, st := ix.Paths[f], string(stat[j])
			i, ok := at[path]
			if !ok {
				at[path] = len(out)
				added[path] = st == "A"
				out = append(out, AggFile{Repo: r.Name, Key: key, Path: path, Status: st, Commits: 1})
				continue
			}
			out[i].Commits++
			if st == "D" || out[i].Status != "A" {
				out[i].Status = st
			}
		}
	}
	if net {
		out = slices.DeleteFunc(out, func(f AggFile) bool { return added[f.Path] && f.Status == "D" })
	}
	slices.SortFunc(out, func(a, b AggFile) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// withUncommitted marks the committed files that also have uncommitted changes and adds the ones that have only those.
func withUncommitted(repo, key string, files []AggFile, fs []FileStatus) []AggFile {
	at := map[string]int{}
	for i, f := range files {
		at[f.Path] = i
	}
	out := slices.Clone(files)
	for _, f := range fs {
		if i, ok := at[f.Path]; ok {
			out[i].Uncommitted = true
			out[i].Untracked = out[i].Untracked || f.Area == gitcli.Untracked
			if f.OldPath != "" {
				out[i].OldPath = f.OldPath
			}
			continue
		}
		at[f.Path] = len(out)
		out = append(out, AggFile{Repo: repo, Key: key, Path: f.Path, OldPath: f.OldPath, Status: f.Status, Uncommitted: true, Untracked: f.Area == gitcli.Untracked})
	}
	slices.SortFunc(out, func(a, b AggFile) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// TagAggPatch reads HEAD to the working directory for worktree entries or worktree mode. Otherwise base mode reads
// merge base to the branch, and stage mode reads each remaining commit's patch.
func (s *Session) TagAggPatch(ctx context.Context, in TagAggInput, row, path, oldPath string, untracked, worktree, ignoreSpace bool) (AggPatch, error) {
	ctx = s.git(ctx, "aggPatch")
	ctx = gitview.IgnoreSpace(ctx, ignoreSpace)
	ctx, done := s.latest.start(ctx, "aggPatch")
	defer done()
	p, err := s.diffPlan(ctx, in)
	if err != nil {
		return AggPatch{}, err
	}
	a, ok := p.row(row)
	if !ok {
		return AggPatch{}, fmt.Errorf("tag %s has no row %q", p.tag.Name, row)
	}
	if worktree || a.mode == DiffWorktree {
		return s.worktreeDiffPatch(ctx, &a, path, oldPath, untracked)
	}
	if a.base >= 0 && a.branch >= 0 {
		var gp gitcli.Patch
		if a.view.Worktree != "" && a.mode != DiffBase && a.mode != DiffStage {
			gp, err = gitview.MergedPatch(ctx, a.r, a.base, a.view.Worktree, path, oldPath, untracked)
		} else {
			gp, err = gitview.ComparePatch(ctx, a.r, a.base, a.branch, path, oldPath)
		}
		if err != nil {
			return AggPatch{}, fmt.Errorf("merged diff of %s: %w", path, err)
		}
		out := AggPatch{Mode: "range", Parts: []AggPart{{Patch: patchOf(gp, path)}}}
		// What the merge commits changed on their own is part of the range diff, but only these parts say who made it.
		for _, c := range a.handsOf(path, a.scope) {
			if len(out.Parts) > maxAggParts {
				out.More++
				continue
			}
			part, err := a.resolvedPart(ctx, c, path)
			if err != nil {
				return AggPatch{}, err
			}
			out.Parts = append(out.Parts, part)
		}
		return out, nil
	}
	ix := p.eng.Index()
	out := AggPatch{Mode: "commits", Parts: []AggPart{}}
	for k := len(a.commits) - 1; k >= 0; k-- {
		c := a.commits[k]
		_, resolved := a.hands[c]
		if !resolved && len(a.r.Parents(c)) > 1 {
			continue
		}
		if resolved {
			if !slices.Contains(a.hands[c], path) {
				continue
			}
		} else {
			files, _ := a.r.Files(c)
			if !slices.ContainsFunc(files, func(f uint32) bool { return ix.Paths[f] == path }) {
				continue
			}
		}
		if len(out.Parts) == maxAggParts {
			out.More++
			continue
		}
		if resolved {
			part, err := a.resolvedPart(ctx, c, path)
			if err != nil {
				return AggPatch{}, err
			}
			out.Parts = append(out.Parts, part)
			continue
		}
		gp, err := gitview.Patch(ctx, a.r, c, path, oldPath)
		if err != nil {
			return AggPatch{}, fmt.Errorf("diff of %s in %s: %w", path, a.r.Oid(c).String(), err)
		}
		out.Parts = append(out.Parts, AggPart{Hash: a.r.Oid(c).String(), Subject: a.r.Subject[c], Patch: patchOf(gp, path)})
	}
	return out, nil
}

// handsOf is the edited merges among commits (newest first) that changed path, oldest first.
func (a *aggRepo) handsOf(path string, commits []int32) []int32 {
	var out []int32
	for k := len(commits) - 1; k >= 0; k-- {
		if slices.Contains(a.hands[commits[k]], path) {
			out = append(out, commits[k])
		}
	}
	return out
}

// resolvedPart is the diff of path in merge commit c against merging its parents again, not against its first parent.
func (a *aggRepo) resolvedPart(ctx context.Context, c int32, path string) (AggPart, error) {
	gp, err := gitview.RemergePatch(ctx, a.r, c, path)
	if err != nil {
		return AggPart{}, fmt.Errorf("resolution of %s in %s: %w", path, a.r.Oid(c).String(), err)
	}
	return AggPart{Hash: a.r.Oid(c).String(), Subject: a.r.Subject[c], Patch: patchOf(gp, path)}, nil
}

// TagAggBranches lists the branches row could switch to: local and remote branches holding some of its Tag commits, most commits first, then checked out, then the newest tip.
func (s *Session) TagAggBranches(in TagAggInput, row string) ([]AggBranch, error) {
	p, err := s.aggPlan(in)
	if err != nil {
		return nil, err
	}
	a, ok := p.row(row)
	if !ok {
		return nil, fmt.Errorf("no row %q", row)
	}
	r := a.r
	var names []string
	var tips []int32
	for _, ref := range r.Refs {
		if a.named != nil && !a.named(ref.Name) {
			continue
		}
		name, ok := strings.CutPrefix(ref.Name, "refs/heads/")
		if !ok {
			name, ok = strings.CutPrefix(ref.Name, "refs/remotes/")
		}
		if ok && name != "HEAD" && !strings.HasSuffix(name, "/HEAD") {
			names = append(names, name)
			tips = append(tips, ref.Commit)
		}
	}
	counts := r.Holding(tips, -1, a.commits) // all commits, not beyond the main branch: a merged row still says which branches hold them
	wt := map[string]string{}
	for _, w := range s.worktrees()[r.Name] {
		if b, ok := strings.CutPrefix(w.Branch, "refs/heads/"); ok {
			wt[b] = w.Path
		}
	}
	out := []AggBranch{}
	time := map[string]int64{}
	for k, n := range names {
		if counts[k] > 0 {
			out = append(out, AggBranch{Name: n, Commits: counts[k], Worktree: wt[n]})
			time[n] = r.Time[tips[k]]
		}
	}
	slices.SortFunc(out, func(x, y AggBranch) int {
		return cmp.Or(y.Commits-x.Commits, boolCmp(y.Worktree != "", x.Worktree != ""), cmp.Compare(time[y.Name], time[x.Name]), strings.Compare(x.Name, y.Name))
	})
	return out, nil
}

// TagAggCommits pages the filter page's commits tab: the Tag's commits of one row ("" = every row). Merge commits are left
// out unless merges is set, except those someone edited while merging (Row.Resolved, once TagAggRemerge has found them).
// Merges counts the merge commits left out.
func (s *Session) TagAggCommits(in TagAggInput, row string, merges bool) (AggCommits, error) {
	p, err := s.aggPlan(in)
	if err != nil {
		return AggCommits{}, err
	}
	if _, ok := p.row(row); row != "" && !ok {
		return AggCommits{}, fmt.Errorf("no row %q", row)
	}
	rows := p.scoped(row)
	resolved := p.resolved(row)
	out := AggCommits{}
	if !merges {
		ix := p.eng.Index()
		n := len(rows)
		rows = slices.DeleteFunc(slices.Clone(rows), func(c index.CommitRef) bool {
			return len(ix.Repos[c.Repo].Parents(c.I)) > 1 && !resolved[c]
		})
		out.Merges = n - len(rows)
	}
	out.Total = len(rows)
	out.ResultID = s.results.put(&result{eng: p.eng, m: p.m, rows: rows, resolved: resolved})
	return out, nil
}

// resolved is the merge commits of row ("" = of every row) known to be edited while merging.
func (p *aggPlan) resolved(row string) map[index.CommitRef]bool {
	out := map[index.CommitRef]bool{}
	ix := p.eng.Index()
	for _, a := range p.repos {
		if row != "" && a.view.Key != row {
			continue
		}
		id := index.RepoID(slices.Index(ix.Repos, a.r))
		for c := range a.hands {
			out[index.CommitRef{Repo: id, I: c}] = true
		}
	}
	return out
}

// TagAggDown lists the commits the compare target has and the row's branch lacks (the ↓ of the row), newest first: what
// merging the target in would bring. Empty when the row has no comparison.
func (s *Session) TagAggDown(in TagAggInput, row string) (AggCommits, error) {
	p, err := s.aggPlan(in)
	if err != nil {
		return AggCommits{}, err
	}
	a, ok := p.row(row)
	if !ok {
		return AggCommits{}, fmt.Errorf("no row %q", row)
	}
	var rows []index.CommitRef
	if a.base >= 0 && a.branch >= 0 {
		set := a.r.Range(a.base, a.branch)
		id, _ := p.eng.Index().RepoByName(a.r.Name)
		for i := range a.r.N() {
			if set.Has(int32(i)) {
				rows = append(rows, index.CommitRef{Repo: id, I: int32(i)})
			}
		}
		slices.SortStableFunc(rows, func(x, y index.CommitRef) int { return cmp.Compare(a.r.Time[y.I], a.r.Time[x.I]) })
	}
	return AggCommits{Total: len(rows), ResultID: s.results.put(&result{eng: p.eng, m: p.m, rows: rows})}, nil
}

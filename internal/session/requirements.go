package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"sheaf/internal/engine"
	"sheaf/internal/index"
	"sheaf/internal/query"
)

// DefaultRequirement is the pattern of a requirement number in a workspace that has not set one: ABC-123.
const DefaultRequirement = `[A-Z][A-Z0-9]+-\d+`

// recentMine is how far back my commits are read for requirement numbers.
const recentMine = 90 * 24 * time.Hour

// Requirement is one tracked requirement number.
type Requirement struct {
	ID       string `json:"id"`
	Title    string `json:"title"` // subject of its newest commit, without the number
	Stage    string `json:"stage"` // Progress, or StageArchived when archived
	Archived bool   `json:"archived"`
	Progress string `json:"progress"` // the least advanced stage of its rows, regardless of archive state
	Repos    int    `json:"repos"`    // repos that hold its commits or a working directory branch named after it
	Latest   int64  `json:"latest"`   // unix seconds of its newest commit or branch tip
	Named    bool   `json:"named"`    // Title is the user's own (SetRequirementName), not the commit's
}

// CheckRequirement is "" when pattern is a usable requirement pattern, else why not, in the English of the error.
func (s *Session) CheckRequirement(pattern string) string {
	if _, err := compileRequirement(pattern); err != nil {
		return err.Error()
	}
	return ""
}

func compileRequirement(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(cmp.Or(strings.TrimSpace(pattern), DefaultRequirement))
	if err != nil {
		return nil, fmt.Errorf("requirement pattern: %w", err)
	}
	return re, nil
}

// SetRequirement saves the requirement number pattern; one that is not a regular expression is refused and nothing is saved.
func (s *Session) SetRequirement(pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if _, err := compileRequirement(pattern); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if pattern == DefaultRequirement {
		pattern = ""
	}
	prev := s.cfg.Requirement
	s.cfg.Requirement = pattern
	if err := s.saveConfig(); err != nil {
		s.cfg.Requirement = prev
		return err
	}
	return nil
}

type reqKey struct {
	eng                    *engine.Engine
	pattern, release, base string
	state                  string // working directory branches and uncommitted counts
	patches, covers        uint64 // patch comparisons and merge classifications answered so far
}

// reqCache keeps the last list: it is the same until the index, the settings, a working directory's branch or
// uncommitted state, or a patch comparison changes.
type reqCache struct {
	mu   sync.Mutex
	key  reqKey
	list []Requirement
	jobs []patchJob
	ok   bool
}

// Requirements lists the tracked requirement numbers (Track), stage by stage (dev, pending, uat, landed), the newest
// first within a stage. Index only: the stage of a requirement is the least advanced of its rows, each judged by the
// requirement's own commits (see planStage). Commits that are not ancestors of the main or release branch may still be
// there as patch-equivalent commits; the list counts them as not until RequirementsSettle has asked git, and then reads
// again.
//
// It takes about 100 ms on a large workspace when the index or a working directory changed, and is cached otherwise:
// call it from a background goroutine, never on the UI thread.
func (s *Session) Requirements(ctx context.Context) ([]Requirement, error) {
	list, _, err := s.requirements(ctx)
	if err != nil {
		return list, err
	}
	s.mu.Lock()
	names := maps.Clone(s.cfg.RequirementNames)
	archived := slices.Clone(s.cfg.Archived)
	s.mu.Unlock()
	for i := range list {
		if n := names[list[i].ID]; n != "" {
			list[i].Title, list[i].Named = n, true
		}
		list[i].Progress = list[i].Stage
		list[i].Archived = slices.Contains(archived, list[i].ID)
		if list[i].Archived {
			list[i].Stage = StageArchived
		}
	}
	slices.SortStableFunc(list, func(a, b Requirement) int {
		return cmp.Or(cmp.Compare(StageRank(a.Stage), StageRank(b.Stage)), cmp.Compare(b.Latest, a.Latest))
	})
	return list, nil
}

// SetRequirementName gives requirement id a title of its own, which Requirements then reports instead of the commits'.
// A blank name removes it: the title is found from the commits again.
func (s *Session) SetRequirementName(id, name string) error {
	id, name = strings.TrimSpace(id), strings.TrimSpace(name)
	if id == "" {
		return errors.New("requirement name: no requirement number")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	names := maps.Clone(s.cfg.RequirementNames)
	if name == "" {
		delete(names, id)
	} else {
		if names == nil {
			names = map[string]string{}
		}
		names[id] = name
	}
	prev := s.cfg.RequirementNames
	s.cfg.RequirementNames = names
	if err := s.saveConfig(); err != nil {
		s.cfg.RequirementNames = prev
		return err
	}
	return nil
}

// RequirementsSettle asks git for the patch comparisons Requirements lacked, in the background, and reports whether any
// answer came in: then the stages may have changed and Requirements gives the new ones. Run it after Requirements so
// the first screen does not wait for git.
func (s *Session) RequirementsSettle(ctx context.Context) (bool, error) {
	ctx = s.git(ctx, "releaseCheck")
	changed := false
	for {
		_, jobs, err := s.requirements(ctx)
		if err != nil {
			return changed, err
		}
		jobs = slices.DeleteFunc(jobs, func(j patchJob) bool { return len(s.pending(j)) == 0 })
		if len(jobs) == 0 {
			break
		}
		var mu sync.Mutex
		done := false
		err = parallel(ctx, len(jobs), releaseParallel, func(i int) {
			if s.runPatchJob(ctx, jobs[i]) {
				mu.Lock()
				done = true
				mu.Unlock()
			}
		})
		if err = cmp.Or(err, ctx.Err()); err != nil {
			return changed || done, err
		}
		if !done {
			break
		}
		changed = true
	}
	return changed, nil
}

// Candidate is a requirement number found in working directory branch names or my recent commit titles that is not
// tracked yet.
type Candidate struct {
	ID     string `json:"id"`
	Title  string `json:"title"`  // subject of its newest commit of mine, without the number; "" when only a branch names it
	Latest int64  `json:"latest"` // unix seconds of its newest commit or branch tip
}

// NormalizeRequirement is input as a requirement number of this workspace's pattern (trimmed, upper-cased when that is
// what makes it match), and whether it is one.
func (s *Session) NormalizeRequirement(input string) (string, bool) {
	s.mu.Lock()
	pattern := s.cfg.Requirement
	s.mu.Unlock()
	re, err := compileRequirement(pattern)
	if err != nil {
		return "", false
	}
	whole := func(x string) bool {
		m := re.FindStringIndex(x)
		return m != nil && m[0] == 0 && m[1] == len(x)
	}
	input = strings.TrimSpace(input)
	if whole(input) {
		return input, true
	}
	if up := strings.ToUpper(input); whole(up) {
		return up, true
	}
	return "", false
}

// Tracked lists the requirement numbers the user chose to follow, in the order they were added.
func (s *Session) Tracked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.cfg.Tracked)
}

// Track adds requirement id to the list Requirements reports; one already there stays where it is.
func (s *Session) Track(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("track requirement: no requirement number")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.cfg.Tracked, id) {
		return nil
	}
	prev := s.cfg.Tracked
	s.cfg.Tracked = append(slices.Clone(prev), id)
	if err := s.saveConfig(); err != nil {
		s.cfg.Tracked = prev
		return err
	}
	return nil
}

// Untrack removes requirement id from the list. Its name (SetRequirementName) stays, for when it is tracked again.
func (s *Session) Untrack(id string) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(s.cfg.Tracked, id) {
		return nil
	}
	prev := s.cfg.Tracked
	s.cfg.Tracked = slices.DeleteFunc(slices.Clone(prev), func(x string) bool { return x == id })
	if err := s.saveConfig(); err != nil {
		s.cfg.Tracked = prev
		return err
	}
	return nil
}

// RequirementCandidates lists the numbers worth tracking: those found in working directory branch names and my commit
// titles of the last 90 days that are not tracked, the newest first. It decides no stage, so it is as fast as the
// discovery alone, and cached with it.
func (s *Session) RequirementCandidates(ctx context.Context) ([]Candidate, error) {
	d, _, _, err := s.discover()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	tracked := slices.Clone(s.cfg.Tracked)
	s.mu.Unlock()
	out := make([]Candidate, 0, len(d.ids))
	for id, f := range d.ids {
		if !slices.Contains(tracked, id) {
			out = append(out, Candidate{ID: id, Title: f.title, Latest: f.latest})
		}
	}
	slices.SortFunc(out, func(a, b Candidate) int { return cmp.Or(cmp.Compare(b.Latest, a.Latest), cmp.Compare(a.ID, b.ID)) })
	return out, nil
}

// found is what discovery knows of one number.
type found struct {
	latest  int64
	title   string // subject of my newest commit that names it, without the number
	titleAt int64
}

// dirtyBranch is a working directory branch with uncommitted files.
type dirtyBranch struct {
	repo, branch string
	path         string
	files        int
	at           int64
}

type discovered struct {
	ids   map[string]*found
	dirty []dirtyBranch
}

type discKey struct {
	eng            *engine.Engine
	pattern, state string
}

type discCache struct {
	mu  sync.Mutex
	key discKey
	d   *discovered
}

// discover finds the numbers in working directory branch names and my recent commit titles. The result is shared:
// callers must not change it.
func (s *Session) discover() (*discovered, *regexp.Regexp, string, error) {
	eng, env, bases := s.current()
	if eng == nil {
		return nil, nil, "", errNotReady
	}
	s.mu.Lock()
	pattern := s.cfg.Requirement
	s.mu.Unlock()
	re, err := compileRequirement(pattern)
	if err != nil {
		return nil, nil, "", err
	}
	wts := s.worktrees()
	ix := eng.Index()
	var state strings.Builder
	for _, r := range ix.Repos {
		for _, w := range wts[r.Name] {
			fmt.Fprintf(&state, "%s\x00%s\x00%d\x01", w.Path, w.Branch, s.dirty.get(w.Path))
		}
	}
	key := discKey{eng, pattern, state.String()}
	s.disc.mu.Lock()
	if s.disc.d != nil && s.disc.key == key {
		d := s.disc.d
		s.disc.mu.Unlock()
		return d, re, key.state, nil
	}
	s.disc.mu.Unlock()

	d := &discovered{ids: map[string]*found{}}
	note := func(id string, at int64) *found {
		f := d.ids[id]
		if f == nil {
			f = &found{}
			d.ids[id] = f
		}
		f.latest = max(f.latest, at)
		return f
	}
	// A working directory branch named after a number names a requirement I take part in, however old its commits or
	// whoever wrote them. Only its uncommitted files count as work by themselves: a clean one has nothing of the
	// requirement that its commits do not show.
	for _, r := range ix.Repos {
		for _, w := range wts[r.Name] {
			branch := strings.TrimPrefix(w.Branch, "refs/heads/")
			if branch == "" {
				continue
			}
			var at int64
			if h, ok := r.Ref(w.Branch); ok {
				at = r.Time[h]
			}
			if n := s.dirty.get(w.Path); n > 0 {
				d.dirty = append(d.dirty, dirtyBranch{r.Name, branch, w.Path, n, at})
			}
			for _, id := range re.FindAllString(branch, -1) {
				note(id, at)
			}
		}
	}
	mine := query.Conditions{Me: true, Authors: []string{}, Since: env.Now.Add(-recentMine).Unix()}
	if m, err := query.Compile(mine, env); err == nil {
		for _, c := range eng.Run(m, bases).Rows {
			r := ix.Repos[c.Repo]
			if len(r.Parents(c.I)) > 1 { // a merge subject carries the merged branch: it would count synced merges
				continue
			}
			for _, id := range re.FindAllString(r.Subject[c.I], -1) {
				f := note(id, r.Time[c.I])
				if title, ok := requirementTitle(re, r.Subject[c.I], id); ok && r.Time[c.I] >= f.titleAt {
					f.title, f.titleAt = title, r.Time[c.I]
				}
			}
		}
	}
	s.disc.mu.Lock()
	s.disc.key, s.disc.d = key, d
	s.disc.mu.Unlock()
	return d, re, key.state, nil
}

func (s *Session) requirements(ctx context.Context) ([]Requirement, []patchJob, error) {
	d, re, state, err := s.discover()
	if err != nil {
		return nil, nil, err
	}
	eng, env, bases := s.current()
	s.mu.Lock()
	pattern, release, master := s.cfg.Requirement, s.cfg.Release, s.cfg.Master
	tracked := slices.Clone(s.cfg.Tracked)
	flow := fmt.Sprint(s.mergeFlowLocked(), s.cfg.BaselineOnly)
	s.mu.Unlock()
	ix := eng.Index()
	key := reqKey{eng, pattern, release, master, state + "\x02" + strings.Join(tracked, "\x01") + "\x02" + flow, s.remerge.generation(), s.cover.generation()}
	s.reqs.mu.Lock()
	hit, list, jobs := s.reqs.ok && s.reqs.key == key, slices.Clone(s.reqs.list), slices.Clone(s.reqs.jobs)
	s.reqs.mu.Unlock()
	if hit {
		return list, jobs, nil
	}

	out := make([]Requirement, len(tracked))
	errs := make([]error, len(tracked))
	var jobMu sync.Mutex
	jobSet := map[jobKey]patchJob{}
	err = parallel(ctx, len(tracked), runtime.NumCPU(), func(k int) {
		id := tracked[k]
		var latest int64
		if f := d.ids[id]; f != nil {
			latest = f.latest
		}
		req := Requirement{ID: id, Stage: StageLanded, Latest: latest}
		// The text search finds ABC-12 in ABC-123 too: keep only the commits that name the whole number.
		keep := requirementKeep(re, id)
		named := requirementNamed(re, id)
		cond := &query.Conditions{Q: id, Authors: []string{}}
		p, err := s.planAgg(TagAggInput{Cond: cond, Base: master, Release: release, keep: keep, named: named}, eng, env, bases)
		if err != nil {
			errs[k] = fmt.Errorf("requirement %s: %w", id, err)
			return
		}
		repos := map[string]bool{}
		for _, a := range p.repos {
			if a.noOwn { // only merges: nothing of the requirement is waiting there
				continue
			}
			if StageRank(a.view.Stage) < StageRank(req.Stage) {
				req.Stage = a.view.Stage
			}
			repos[a.view.Repo] = true
			if len(a.jobs) > 0 {
				jobMu.Lock()
				for _, j := range a.jobs {
					k := j.key()
					if old, ok := jobSet[k]; ok {
						j.commits = slices.Compact(slices.Sorted(slices.Values(slices.Concat(old.commits, j.commits))))
					}
					jobSet[k] = j
				}
				jobMu.Unlock()
			}
		}
		// Uncommitted files on a branch named after the number count even before its first commit.
		for _, b := range d.dirty {
			if named(b.branch) {
				req.Stage = StageDev
				repos[b.repo] = true
				req.Latest = max(req.Latest, b.at)
			}
		}
		req.Repos = len(repos)
		if req.Repos == 0 { // tracked, but nothing of it exists yet
			req.Stage = StageDev
		}
		for _, c := range p.rows { // newest first
			r := ix.Repos[c.Repo]
			if len(r.Parents(c.I)) > 1 {
				continue
			}
			if title, ok := requirementTitle(re, r.Subject[c.I], id); ok {
				req.Title, req.Latest = title, max(req.Latest, r.Time[c.I])
				break
			}
		}
		out[k] = req
	})
	if err = cmp.Or(err, errors.Join(errs...)); err != nil {
		return nil, nil, err
	}
	slices.SortStableFunc(out, func(a, b Requirement) int {
		return cmp.Or(cmp.Compare(StageRank(a.Stage), StageRank(b.Stage)), cmp.Compare(b.Latest, a.Latest))
	})
	jobs = slices.Collect(maps.Values(jobSet))
	s.reqs.mu.Lock()
	s.reqs.key, s.reqs.list, s.reqs.jobs, s.reqs.ok = key, slices.Clone(out), slices.Clone(jobs), true
	s.reqs.mu.Unlock()
	return out, jobs, nil
}

// requirementNamed tells whether a branch name carries any whole number in ids, in any letter case: branches are written
// in lower case more often than commit titles.
func requirementNamed(re *regexp.Regexp, ids ...string) func(string) bool {
	match := requirementMatch(re, ids...)
	return func(branch string) bool { return match(branch) != "" }
}

func requirementMatch(re *regexp.Regexp, ids ...string) func(string) string {
	loose, err := regexp.Compile("(?i)" + re.String())
	if err != nil {
		loose = re
	}
	return func(branch string) string {
		matches := loose.FindAllString(branch, -1)
		for _, id := range ids {
			if slices.ContainsFunc(matches, func(m string) bool { return strings.EqualFold(m, id) }) {
				return id
			}
		}
		return ""
	}
}

func requirementKeep(re *regexp.Regexp, id string) func(*index.Repo, int32) bool {
	return func(r *index.Repo, c int32) bool {
		return slices.Contains(re.FindAllString(r.Subject[c], -1), id)
	}
}

// requirementTitle is subject without the number id and the separators after it (brackets around the number go with
// it); false when the subject does not name id as a whole number, only the start of a longer one (ABC-12 in ABC-123).
func requirementTitle(re *regexp.Regexp, subject, id string) (string, bool) {
	for _, at := range re.FindAllStringIndex(subject, -1) {
		if subject[at[0]:at[1]] != id {
			continue
		}
		before, after := subject[:at[0]], subject[at[1]:]
		for _, pair := range [][2]string{{"[", "]"}, {"【", "】"}, {"(", ")"}, {"（", "）"}} {
			if strings.HasSuffix(before, pair[0]) && strings.HasPrefix(after, pair[1]) {
				before, after = strings.TrimSuffix(before, pair[0]), strings.TrimPrefix(after, pair[1])
				break
			}
		}
		after = strings.TrimLeft(after, " \t:：-–—_.,，、;；|/")
		return strings.TrimSpace(strings.TrimSpace(before) + " " + after), true
	}
	return "", false
}

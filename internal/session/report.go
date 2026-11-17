package session

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"sheaf/internal/discover"
	"sheaf/internal/gitcli"
	"sheaf/internal/gitview"
	"sheaf/internal/query"
)

// Settle observes local state synchronously. Nil ids settles all tracked requirements; otherwise only ids are settled.
// Callers retain its problems alongside subsequent query results.
func (s *Session) Settle(ctx context.Context, ids []string) []Problem {
	defer s.saveDerived()
	start := s.rec.LastID()
	problems := []Problem{}
	if err := s.Refresh(ctx); err != nil {
		problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
	}
	// GUI sessions may already have a dirty refresh in flight.
	s.bg.Wait()
	dirtyIDs := ids
	if dirtyIDs == nil {
		dirtyIDs = s.Tracked()
	}
	paths, err := s.requirementWorktreePaths(dirtyIDs)
	if err != nil {
		problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
	}
	s.RefreshDirty(ctx, paths, false)
	if ids == nil {
		for {
			merges, patches := s.remerge.generation(), s.cover.generation()
			if _, err := s.Requirements(ctx); err != nil {
				problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
				break
			}
			for _, id := range s.Tracked() {
				if _, err := s.TagAggRemerge(ctx, reportInput(id)); err != nil {
					problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
				}
			}
			if _, err := s.RequirementsSettle(ctx); err != nil {
				problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
			}
			if ctx.Err() != nil || merges == s.remerge.generation() && patches == s.cover.generation() {
				break
			}
		}
		ids = s.Tracked()
	}
	for _, id := range ids {
		r, err := s.Report(ctx, id)
		if err != nil {
			problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
		} else {
			for _, problem := range r.Problems {
				if problem.Code == ProblemGitRead || problem.Code == ProblemPatchPending || problem.Code == ProblemUnclassifiedMerge {
					problems = append(problems, problem)
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
	}
	return uniqueProblems(append(problems, s.reportReadErrors(start)...))
}

func (s *Session) requirementWorktreePaths(ids []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths := []string{}
	re, err := compileRequirement(s.cfg.Requirement)
	if err != nil {
		return paths, err
	}
	matches := requirementNamed(re, ids...)
	for _, r := range s.repos {
		for _, wt := range r.Worktrees {
			if matches(wt.Branch) {
				paths = append(paths, wt.Path)
			}
		}
	}
	return paths, nil
}

// SettleIndex refreshes repositories and the index without reading uncommitted state or comparing patches.
func (s *Session) SettleIndex(ctx context.Context) []Problem {
	defer s.saveDerived()
	problems := []Problem{}
	if err := s.Refresh(ctx); err != nil {
		problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
	}
	if err := ctx.Err(); err != nil {
		problems = append(problems, Problem{Code: ProblemGitRead, Detail: err.Error()})
	}
	return uniqueProblems(problems)
}

func reportInput(id string) TagAggInput {
	return TagAggInput{Requirement: id, Cond: &query.Conditions{Q: id, Authors: []string{}}}
}

// ScopeIDs lists tracked requirement numbers in their tracked order for the given scope.
func (s *Session) ScopeIDs(scope Scope) ([]string, error) {
	if scope == "" {
		scope = ScopeActive
	}
	if scope != ScopeActive && scope != ScopeArchived && scope != ScopeAll {
		return nil, fmt.Errorf("scope IDs: invalid scope %q", scope)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := []string{}
	for _, id := range s.cfg.Tracked {
		archived := slices.Contains(s.cfg.Archived, id)
		if scope == ScopeActive && archived || scope == ScopeArchived && !archived {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Overview lists only tracked requirements; a number search matches the whole number.
func (s *Session) Overview(ctx context.Context, scope Scope, search string) (Overview, error) {
	ids, err := s.ScopeIDs(scope)
	if err != nil {
		return Overview{}, err
	}
	s.mu.Lock()
	out := Overview{ObservedAt: s.state.Updated, Excluded: append([]string{}, s.cfg.Ignored...), Items: []ReqSummary{}, Problems: []Problem{}}
	s.mu.Unlock()
	search = strings.ToLower(strings.TrimSpace(search))
	for _, id := range ids {
		r, err := s.Report(ctx, id)
		if err != nil {
			return out, err
		}
		if search != "" && strings.ToLower(id) != search && !strings.Contains(strings.ToLower(r.Title), search) {
			continue
		}
		out.Items = append(out.Items, r.ReqSummary)
		out.Problems = append(out.Problems, r.Problems...)
	}
	slices.SortFunc(out.Items, func(a, b ReqSummary) int {
		return cmp.Or(cmp.Compare(StageRank(a.Progress), StageRank(b.Progress)), cmp.Compare(b.Latest, a.Latest), strings.Compare(a.ID, b.ID))
	})
	out.Problems = uniqueProblems(out.Problems)
	return out, nil
}

func (s *Session) Report(ctx context.Context, id string) (ReqReport, error) {
	return s.requirementReport(ctx, id, "", false)
}

// ReportWithFiles includes file lists from the statuses cached by Settle.
func (s *Session) ReportWithFiles(ctx context.Context, id string) (ReqReport, error) {
	return s.requirementReport(ctx, id, "", true)
}

// StageCheck keeps the default progress while replacing the gaps and checks with the requested target's evidence.
func (s *Session) StageCheck(ctx context.Context, id, stage string) (ReqReport, error) {
	if !slices.ContainsFunc(s.MergeFlow(), func(step MergeStep) bool { return step.Name == stage }) {
		return ReqReport{}, fmt.Errorf("stage check: unknown stage %q", stage)
	}
	return s.requirementReport(ctx, id, stage, false)
}

func (s *Session) requirementReport(ctx context.Context, input, stage string, files bool) (ReqReport, error) {
	id, ok := s.NormalizeRequirement(input)
	if !ok {
		return ReqReport{}, fmt.Errorf("report: invalid requirement %q", input)
	}
	start := s.rec.LastID()
	p, err := s.settleReport(ctx, id)
	if err != nil {
		return ReqReport{}, err
	}
	s.mu.Lock()
	out := ReqReport{
		ReqSummary: ReqSummary{ID: id, Title: s.cfg.RequirementNames[id], Tracked: slices.Contains(s.cfg.Tracked, id),
			Archived: slices.Contains(s.cfg.Archived, id), Progress: StageLanded, Gaps: []RepoGap{}},
		Flow: s.mergeFlowLocked(), ObservedAt: s.state.Updated, Rows: []RowReport{}, Problems: []Problem{},
	}
	stateError := s.state.Error
	pattern := s.cfg.Requirement
	s.mu.Unlock()
	if stateError != "" {
		out.Problems = append(out.Problems, Problem{Code: ProblemGitRead, Detail: stateError})
	}
	re, _ := compileRequirement(pattern)
	ix := p.eng.Index()
	for _, c := range p.rows {
		r := ix.Repos[c.Repo]
		out.Found = true
		out.Latest = max(out.Latest, r.Time[c.I])
		if out.Title == "" && len(r.Parents(c.I)) < 2 {
			out.Title, _ = requirementTitle(re, r.Subject[c.I], id)
		}
	}
	wts := s.worktrees()
	named := requirementNamed(re, id)
	seenDirty := map[string]bool{}
	repos := map[string]bool{}
	for _, a := range p.repos {
		row := RowReport{Repo: a.r.Name, Path: a.r.Dir, Branch: a.view.Branch, Worktree: a.view.Worktree,
			Deleted: a.deleted, Commits: len(a.commits), Stage: a.view.Stage, Stages: []StageCheck{}}
		if row.Branch == "" && !a.deleted {
			row.Branch = a.view.MergedBranch
		}
		if files {
			list := []UncommittedFile{}
			row.UncommittedFiles = &list
		}
		for _, wt := range wts[a.r.Name] {
			if !named(wt.Branch) {
				continue
			}
			out.Found = true
			if tip, ok := a.r.Ref(wt.Branch); ok {
				out.Latest = max(out.Latest, a.r.Time[tip])
			}
			if row.Worktree == "" {
				row.Worktree = wt.Path
			}
			if files {
				if status, ok := s.dirty.statusOf(wt.Path); ok {
					for _, file := range status.Files() {
						file.Worktree = wt.Path
						*row.UncommittedFiles = append(*row.UncommittedFiles, file)
					}
				} else {
					out.Problems = append(out.Problems, Problem{Code: ProblemGitRead, Repo: row.Repo,
						Detail: "cached worktree status unavailable: " + wt.Path})
				}
			}
			if n := s.dirty.get(wt.Path); n > 0 {
				row.Uncommitted += n
				row.Stage = StageDev
				if !seenDirty[wt.Path] {
					seenDirty[wt.Path] = true
					out.Uncommitted += n
				}
			}
		}
		if !a.noOwn || row.Uncommitted > 0 {
			repos[a.r.Name] = true
			if StageRank(row.Stage) < StageRank(out.Progress) {
				out.Progress = row.Stage
			}
		}
		for _, commit := range a.unknown {
			out.Problems = append(out.Problems, Problem{Code: ProblemUnclassifiedMerge, Repo: a.r.Name, Detail: a.r.Oid(commit).String()})
		}
		for i, step := range out.Flow {
			if stage != "" && step.Name != stage {
				continue
			}
			check, problems := s.reportStage(&a, step, i, len(out.Flow))
			row.Stages = append(row.Stages, check)
			out.Problems = append(out.Problems, problems...)
		}
		gap := RepoGap{Repo: row.Repo, Path: row.Path, Uncommitted: row.Uncommitted}
		if len(a.unknown) > 0 {
			gap.Unknown = ProblemUnclassifiedMerge
		} else if row.Stage == StageUnknown {
			gap.Unknown = ProblemNoEvidence
			out.Problems = append(out.Problems, Problem{Code: ProblemNoEvidence, Repo: row.Repo, Detail: "no own commits"})
		}
		for _, check := range row.Stages {
			if !check.Applies {
				continue
			}
			next, isMerge := MergeStageIndex(row.Stage)
			if stage != "" || isMerge && out.Flow[next].Name == check.Name || row.Stage == StageDev && gap.Next == "" && len(check.Missing) > 0 {
				gap.Next, gap.Missing = check.Name, len(check.Missing)
			}
			if check.SHA == "" && check.Name == out.Flow[len(out.Flow)-1].Name {
				gap.Unknown = ProblemFinalMissing
			}
		}
		if gap.Unknown != "" {
			gap.Next = ""
		}
		if gap.Unknown != "" || gap.Uncommitted > 0 || stage == "" && row.Stage != StageLanded || stage != "" && gap.Missing > 0 {
			out.Gaps = append(out.Gaps, gap)
		}
		if files {
			slices.SortFunc(*row.UncommittedFiles, func(a, b UncommittedFile) int {
				return cmp.Or(strings.Compare(a.Worktree, b.Worktree), strings.Compare(a.Path, b.Path), strings.Compare(a.Area, b.Area))
			})
		}
		out.Rows = append(out.Rows, row)
	}
	// A clean, named working directory is still locating evidence, even without any matching commits.
	for _, r := range ix.Repos {
		for _, wt := range wts[r.Name] {
			if named(wt.Branch) {
				out.Found = true
				if tip, ok := r.Ref(wt.Branch); ok {
					out.Latest = max(out.Latest, r.Time[tip])
				}
			}
		}
	}
	out.Repos = len(repos)
	if out.Repos == 0 {
		out.Progress = StageDev
	}
	if out.Tracked && (!out.Found || len(p.rows) == 0 && out.Uncommitted == 0) {
		out.Problems = append(out.Problems, Problem{Code: ProblemNoEvidence, Detail: id})
	}
	out.Problems = uniqueProblems(append(out.Problems, s.reportReadErrors(start)...))
	slices.SortFunc(out.Rows, func(a, b RowReport) int {
		return cmp.Or(strings.Compare(a.Repo, b.Repo), strings.Compare(a.Branch, b.Branch))
	})
	return out, nil
}

func (s *Session) settleReport(ctx context.Context, id string) (*aggPlan, error) {
	for {
		merges, patches := s.remerge.generation(), s.cover.generation()
		if _, err := s.TagAggRemerge(ctx, reportInput(id)); err != nil {
			return nil, err
		}
		p, err := s.aggPlan(reportInput(id))
		if err != nil {
			return nil, err
		}
		flow := s.MergeFlow()
		for _, a := range p.repos {
			for i, step := range flow {
				if s.repoBaselineOnly(a.r.Name) && i < len(flow)-1 {
					continue
				}
				line := releaseTip(a.r, cmp.Or(step.Branch, a.view.Main))
				if line >= 0 {
					s.patchCover(&a, line, s.reachOf(a.r, []int32{line}))
				}
			}
			for _, job := range a.jobs {
				s.runPatchJob(s.git(ctx, "releaseCheck"), job)
			}
		}
		if ctx.Err() != nil || merges == s.remerge.generation() && patches == s.cover.generation() {
			return p, nil
		}
	}
}

func (s *Session) reportStage(a *aggRepo, step MergeStep, position, count int) (StageCheck, []Problem) {
	check := StageCheck{Name: step.Name, Applies: true, Covered: []Cover{}, Missing: []CommitRef{}}
	problems := []Problem{}
	if s.repoBaselineOnly(a.r.Name) && position < count-1 {
		check.Applies, check.Skip = false, SkipBaselineOnly
		return check, problems
	}
	branch := cmp.Or(step.Branch, a.view.Main)
	line, ref := releaseTarget(a.r, branch)
	if line < 0 && position < count-1 {
		check.Applies, check.Skip = false, SkipNoTarget
		return check, problems
	}
	if line < 0 {
		problems = append(problems, Problem{Code: ProblemFinalMissing, Repo: a.r.Name, Stage: step.Name, Detail: branch})
	} else {
		check.Ref, check.SHA, check.Remote = ref, a.r.Oid(line).String(), strings.HasPrefix(ref, "refs/remotes/")
	}
	anc := s.reachOf(a.r, nil)
	if line >= 0 {
		anc = s.reachOf(a.r, []int32{line})
	}
	for _, c := range a.own {
		commit := CommitRef{SHA: a.r.Oid(c).String(), Subject: a.r.Subject[c]}
		if anc.Has(c) {
			check.Covered = append(check.Covered, Cover{CommitRef: commit, By: CoverAncestor})
			continue
		}
		if line >= 0 && len(a.r.Parents(c)) < 2 {
			covered, known := s.cover.get(s.coverKey(a.r, line, c))
			if covered {
				check.Covered = append(check.Covered, Cover{CommitRef: commit, By: CoverPatch})
				continue
			}
			if !known {
				problems = append(problems, Problem{Code: ProblemPatchPending, Repo: a.r.Name, Stage: step.Name, Detail: commit.SHA})
			}
		}
		check.Missing = append(check.Missing, commit)
	}
	return check, problems
}

func (s *Session) reportReadErrors(after uint64) []Problem {
	out := []Problem{}
	wts := s.worktrees()
	for _, event := range s.rec.Events() {
		if event.ID <= after || event.Result == gitcli.Succeeded || event.Result == gitcli.Running {
			continue
		}
		if event.Source != "dirtyCount" && event.Source != "releaseCheck" && event.Source != "remerge" {
			continue
		}
		repo := ""
		for name, dirs := range wts {
			for _, wt := range dirs {
				if event.Dir == wt.Path {
					repo = name
					break
				}
			}
		}
		out = append(out, Problem{Code: ProblemGitRead, Repo: repo, Detail: strings.Join(event.Args, " ") + ": " + event.Stderr})
	}
	return out
}

func uniqueProblems(problems []Problem) []Problem {
	slices.SortFunc(problems, func(a, b Problem) int {
		return cmp.Or(strings.Compare(a.Repo, b.Repo), strings.Compare(a.Stage, b.Stage), strings.Compare(a.Code, b.Code), strings.Compare(a.Detail, b.Detail))
	})
	return slices.Compact(problems)
}

// ReqCommits pages the complete matching local history, independently of merge and archive state.
func (s *Session) ReqCommits(ctx context.Context, input, repo, cursor string, limit int) (CommitPage, error) {
	id, ok := s.NormalizeRequirement(input)
	if !ok {
		return CommitPage{}, fmt.Errorf("commits: invalid requirement %q", input)
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 0 {
		return CommitPage{}, errors.New("commits: limit must be positive")
	}
	var after commitCursor
	if cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(b, &after) != nil || len(after.SHA) != 40 || after.Repo == "" {
			return CommitPage{}, errors.New("commits: invalid cursor")
		}
	}
	p, err := s.aggPlan(reportInput(id))
	if err != nil {
		return CommitPage{}, err
	}
	ix := p.eng.Index()
	items := make([]CommitItem, 0, len(p.rows))
	for _, c := range p.rows {
		r := ix.Repos[c.Repo]
		if repo != "" && r.Name != repo {
			continue
		}
		item := CommitItem{Repo: r.Name, Path: r.Dir, SHA: r.Oid(c.I).String(), Time: r.Time[c.I], Subject: r.Subject[c.I],
			Author: ix.Strs[r.Author[c.I]], Email: ix.Strs[r.Email[c.I]], Parents: []string{}}
		for _, parent := range r.Parents(c.I) {
			item.Parents = append(item.Parents, r.Oid(parent).String())
		}
		if cursor == "" || compareCursor(cursorOf(item), after) > 0 {
			items = append(items, item)
		}
	}
	slices.SortFunc(items, func(a, b CommitItem) int { return compareCursor(cursorOf(a), cursorOf(b)) })
	out := CommitPage{Items: items}
	if len(items) > limit {
		out.Items = items[:limit]
		b, _ := json.Marshal(cursorOf(out.Items[limit-1]))
		out.Next = base64.RawURLEncoding.EncodeToString(b)
	}
	for _, r := range ix.Repos {
		var oids []gitcli.Oid
		for _, item := range out.Items {
			if item.Repo == r.Name {
				c, _ := r.FindHash(item.SHA)
				oids = append(oids, r.Oid(c))
			}
		}
		if len(oids) == 0 {
			continue
		}
		bodies, err := gitview.CommitBodies(s.git(ctx, "commit"), r, oids)
		for i := range out.Items {
			item := &out.Items[i]
			if item.Repo != r.Name {
				continue
			}
			c, _ := r.FindHash(item.SHA)
			if body, ok := bodies[r.Oid(c)]; ok {
				item.Body = &body
			} else if err != nil {
				item.BodyErr = err.Error()
			} else {
				item.BodyErr = "commit object is missing or unreadable"
			}
		}
	}
	return out, nil
}

type commitCursor struct {
	Time int64  `json:"time"`
	Repo string `json:"repo"`
	SHA  string `json:"sha"`
}

func cursorOf(item CommitItem) commitCursor { return commitCursor{item.Time, item.Repo, item.SHA} }

func compareCursor(a, b commitCursor) int {
	return cmp.Or(cmp.Compare(b.Time, a.Time), strings.Compare(a.Repo, b.Repo), strings.Compare(a.SHA, b.SHA))
}

func (s *Session) ConfigJSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.cfg
	cfg.MergeFlow = s.mergeFlowLocked()
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return b
}

func (s *Session) ApplyConfig(b []byte) error {
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("apply config: %w", err)
	}
	re, err := compileRequirement(cfg.Requirement)
	if err != nil {
		return err
	}
	if len(cfg.MergeFlow) == 0 {
		return errors.New("apply config: merge flow must not be empty")
	}
	names, branches := map[string]bool{}, map[string]bool{}
	for _, step := range cfg.MergeFlow {
		if !validReportBranch(step.Branch) {
			return fmt.Errorf("apply config: invalid target branch %q", step.Branch)
		}
		if strings.TrimSpace(step.Name) == "" || strings.TrimSpace(step.Name) != step.Name || strings.ContainsAny(step.Name, "\x00\r\n") || names[step.Name] || branches[step.Branch] {
			return errors.New("apply config: merge flow needs distinct names and targets")
		}
		names[step.Name], branches[step.Branch] = true, true
	}
	tracked := map[string]bool{}
	validID := func(id string) bool { at := re.FindStringIndex(id); return at != nil && at[0] == 0 && at[1] == len(id) }
	for _, id := range cfg.Tracked {
		if !validID(id) || tracked[id] {
			return fmt.Errorf("apply config: invalid or duplicate tracked requirement %q", id)
		}
		tracked[id] = true
	}
	archived := map[string]bool{}
	for _, id := range cfg.Archived {
		if !validID(id) || archived[id] {
			return fmt.Errorf("apply config: invalid or duplicate archived requirement %q", id)
		}
		archived[id] = true
	}
	cfg.Root = s.Root
	if cfg.Bases == nil {
		cfg.Bases = map[string]string{}
	}
	if cfg.OldMeSet && cfg.MeManual == nil {
		cfg.MeManual = cfg.OldMe
	}
	cfg.OldMe, cfg.OldMeSet = nil, false
	s.mu.Lock()
	previous := s.cfg
	s.cfg = cfg
	err = s.saveConfig()
	if err != nil {
		s.cfg = previous
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("apply config: %w", err)
	}
	if !slices.Equal(previous.Ignored, cfg.Ignored) {
		s.retrack()
	}
	return nil
}

func validReportBranch(branch string) bool {
	if branch == "" {
		return true
	}
	if branch == "@" || strings.HasPrefix(branch, "-") || strings.HasSuffix(branch, ".") ||
		strings.ContainsAny(branch, "~^:?*[\\") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") {
		return false
	}
	for _, r := range branch {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			return false
		}
	}
	for _, part := range strings.Split(branch, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// CompletionData reads saved candidates without opening a session or updating app data.
func CompletionData(dataDir, cacheDir, root string) (CompletionSnapshot, error) {
	var out CompletionSnapshot
	id := WorkspaceID(root)
	var cfg Config
	read := func(path string, value any) error {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read completion data %s: %w", path, err)
		}
		if err := json.Unmarshal(b, value); err != nil {
			return fmt.Errorf("decode completion data %s: %w", path, err)
		}
		return nil
	}
	if err := read(filepath.Join(dataDir, "workspaces", id, "workspace.json"), &cfg); err != nil {
		return out, err
	}
	for _, archived := range []bool{false, true} {
		for _, requirement := range cfg.Tracked {
			if slices.Contains(cfg.Archived, requirement) == archived {
				out.Requirements = append(out.Requirements, CompletionRequirement{
					ID: requirement, Title: cfg.RequirementNames[requirement], Archived: archived,
				})
			}
		}
	}
	for _, step := range cfg.mergeFlow() {
		out.Stages = append(out.Stages, step.Name)
	}
	var repos []discover.Repo
	if err := read(filepath.Join(cacheDir, "workspaces", id, "scan.json"), &repos); err != nil {
		return CompletionSnapshot{}, err
	}
	repos, _, _ = withoutIgnored(repos, cfg.Ignored)
	for _, repo := range repos {
		out.Repositories = append(out.Repositories, repo.Name)
	}
	slices.Sort(out.Repositories)
	out.Repositories = slices.Compact(out.Repositories)
	return out, nil
}

// CanonicalRoot preserves a recorded workspace's identity when opened through a symbolic link.
func CanonicalRoot(dataDir, root string) string {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return absolute
	}
	roots, err := recordedWorkspaceRoots(dataDir)
	if err != nil {
		return absolute
	}
	matches := []string{}
	for _, recorded := range roots {
		if recorded == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(recorded); err == nil && resolved == real {
			matches = append(matches, recorded)
		}
	}
	if len(matches) == 0 {
		return absolute
	}
	slices.Sort(matches)
	return matches[0]
}

// FindWorkspace uses persisted roots, falling back to recent.json for older configurations.
func FindWorkspace(dataDir, cacheDir, dir string) ([]string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("find workspace: %w", err)
	}
	roots, err := recordedWorkspaceRoots(dataDir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for id, root := range roots {
		if root == "" {
			continue
		}
		match := pathWithin(root, dir)
		if repos, ok := loadScan(filepath.Join(cacheDir, "workspaces", id, "scan.json")); ok {
			for _, repo := range repos {
				match = match || pathWithin(repo.Dir, dir)
				for _, wt := range repo.Worktrees {
					match = match || pathWithin(wt.Path, dir)
				}
			}
		}
		if match {
			out = append(out, root)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// KnownWorkspaces reads saved workspace metadata without opening sessions or updating caches.
func KnownWorkspaces(dataDir, cacheDir, cwd string) ([]KnownWorkspace, error) {
	roots, err := recordedWorkspaceRoots(dataDir)
	if err != nil {
		return nil, err
	}
	current, err := FindWorkspace(dataDir, cacheDir, cwd)
	if err != nil {
		return nil, err
	}
	items := []KnownWorkspace{}
	seen := map[string]bool{}
	for id, root := range roots {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		item := KnownWorkspace{Root: root, Name: filepath.Base(root), ID: WorkspaceID(root), Current: slices.Contains(current, root)}
		if st, err := os.Stat(root); err == nil {
			item.Exists = st.IsDir()
		}
		file := filepath.Join(dataDir, "workspaces", id, "workspace.json")
		b, err := os.ReadFile(file)
		if !os.IsNotExist(err) {
			item.ConfigFile = file
		}
		var cfg Config
		if err == nil && json.Unmarshal(b, &cfg) == nil {
			for _, tracked := range cfg.Tracked {
				if slices.Contains(cfg.Archived, tracked) {
					item.Archived++
				} else {
					item.Tracked++
				}
			}
		}
		items = append(items, item)
	}
	slices.SortFunc(items, func(a, b KnownWorkspace) int { return strings.Compare(a.Root, b.Root) })
	return items, nil
}

func recordedWorkspaceRoots(dataDir string) (map[string]string, error) {
	roots := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(dataDir, "workspaces"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read workspaces: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dataDir, "workspaces", entry.Name(), "workspace.json"))
		if err != nil {
			continue
		}
		var cfg Config
		if err := json.Unmarshal(b, &cfg); err != nil {
			continue
		}
		roots[entry.Name()] = cfg.Root
	}
	var recent struct {
		Recent []struct {
			Root string `json:"root"`
		} `json:"recent"`
		Open []string `json:"open"`
	}
	if b, err := os.ReadFile(filepath.Join(dataDir, "recent.json")); err == nil {
		if err := json.Unmarshal(b, &recent); err != nil {
			return nil, fmt.Errorf("read recent workspaces: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read recent workspaces: %w", err)
	}
	for _, r := range recent.Recent {
		recent.Open = append(recent.Open, r.Root)
	}
	for _, root := range recent.Open {
		if root != "" && roots[WorkspaceID(root)] == "" {
			roots[WorkspaceID(root)] = root
		}
	}
	return roots, nil
}

func pathWithin(root, dir string) bool {
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

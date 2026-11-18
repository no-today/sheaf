package session

import (
	"cmp"
	"context"
	"regexp"
	"slices"
	"strings"

	"sheaf/internal/gitops"
	"sheaf/internal/gitview"
	"sheaf/internal/index"
)

// 创建 MR: plan, then run; the page keeps the result.

// mrRows settles the git side of the ticked rows from the index: the source branch's name on origin, whether it is local and
// how far ahead of origin, and the target (the branch the row's 对主分支 column compares with) by its name on origin.
func (s *Session) mrRows(in MRInput) ([]gitops.MRRow, []AggRepo, error) {
	p, err := s.aggPlan(in.Agg)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	dirs := map[string]string{}
	for _, r := range s.repos {
		dirs[r.Name] = r.Dir
	}
	s.mu.Unlock()
	var rows []gitops.MRRow
	var views []AggRepo
	for _, key := range in.Rows {
		a, ok := p.row(key)
		if !ok {
			continue
		}
		row := gitops.MRRow{Key: key, Dir: dirs[a.view.Repo], Ahead: -1, Target: onOrigin(a.r, a.view.Base)}
		if a.view.BranchFrom != FromMerged && a.view.BranchFrom != FromNone {
			row.Source = onOrigin(a.r, a.view.Branch)
			row.Local = fullRef(a.r, a.view.Branch) == "refs/heads/"+row.Source
		}
		if row.Local {
			head, _ := a.r.Ref("refs/heads/" + row.Source)
			if remote, ok := a.r.Ref("refs/remotes/origin/" + row.Source); ok {
				row.Ahead, row.Behind = a.r.AheadBehind(remote, head)
			}
		}
		rows = append(rows, row)
		views = append(views, a.view)
	}
	return rows, views, nil
}

// fullRef is the ref a short branch name (origin/master, master) is in the repo: a local branch first, then a remote one.
func fullRef(r *index.Repo, name string) string {
	if name == "" {
		return ""
	}
	for _, want := range []string{name, "refs/heads/" + name, "refs/remotes/" + name} {
		if strings.HasPrefix(want, "refs/") && slices.ContainsFunc(r.Refs, func(ref index.Ref) bool { return ref.Name == want }) {
			return want
		}
	}
	return ""
}

// onOrigin is a branch's name on origin: feat for feat or origin/feat; "" for a branch of another remote or none.
func onOrigin(r *index.Repo, name string) string {
	ref := fullRef(r, name)
	if b, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		return b
	}
	b, _ := strings.CutPrefix(ref, "refs/remotes/origin/")
	return b
}

// MRPlan is what each ticked row would do: push first or not, open a merge request, or why not.
func (s *Session) MRPlan(ctx context.Context, in MRInput) (MRPlan, error) {
	ctx = s.git(ctx, "mrPlan")
	out := MRPlan{Rows: []MRPlanRow{}}
	rows, views, err := s.mrRows(in)
	if err != nil {
		return out, err
	}
	for i, r := range rows {
		r, err = gitops.PrepareMR(ctx, r)
		if err != nil {
			return out, err
		}
		skip, err := gitops.DecideMR(ctx, r)
		if err != nil && (skip == "" || ctx.Err() != nil) {
			return out, err
		}
		pr := MRPlanRow{Tool: r.Tool, Missing: r.Missing, Key: r.Key, Repo: views[i].Repo, Branch: r.Source, Target: r.Target, Skip: skip, Commands: []string{}}
		if skip == "" {
			pr.Push, pr.Commands = r.Push(), gitops.MRCommands(r)
			if !r.Missing {
				out.Open++
			}
		}
		out.Rows = append(out.Rows, pr)
	}
	return out, nil
}

// CreateMR opens the ticked rows' merge requests, as MRPlan says at the moment it runs.
func (s *Session) CreateMR(ctx context.Context, in MRInput) (MRRun, error) {
	ctx, cancel := writeContext(s.git(ctx, "mrCreate"))
	defer cancel()
	s.writes.Lock()
	defer s.writes.Unlock()
	run := MRRun{At: s.opts.Now().Unix(), Rows: []MRRow{}}
	rows, views, err := s.mrRows(in)
	if err != nil {
		return run, err
	}
	labels := make([]string, len(rows))
	for i, r := range rows {
		labels[i] = views[i].Repo + " · " + r.Source
	}
	task := s.startTask(TaskMR, in.Subject, labels, func() {}) // begin sees the stop before each row
	fail := func(err error) (MRRun, error) {
		task.end(err, false)
		return run, err
	}
	needsGlab := false
	for i, r := range rows {
		rows[i], err = gitops.PrepareMR(ctx, r)
		if err != nil {
			return fail(err)
		}
		skip, err := gitops.DecideMR(ctx, rows[i])
		if err != nil {
			return fail(err)
		}
		needsGlab = needsGlab || (skip == "" && rows[i].Tool == "glab" && !rows[i].Missing)
	}
	s.mu.Lock()
	glabCfg := s.cfg.GlabConfig
	s.mu.Unlock()
	// Before any push: a wrong directory must not leave branches pushed and no merge requests.
	if needsGlab {
		if err := gitview.CheckGlabConfig(glabCfg); err != nil {
			return fail(err)
		}
		ctx = gitview.GlabConfig(ctx, glabCfg)
	}
	pushed := false
	for r := range gitops.RunMR(ctx, rows, gitops.Progress{Start: task.begin}) {
		i := len(run.Rows)
		task.rowURL(i, batchState(r.Outcome), cmp.Or(r.Error, r.Skip, r.Outcome), r.URL)
		run.Rows = append(run.Rows, MRRow{Tool: r.Tool, Missing: r.Missing, Key: r.Key, Repo: views[i].Repo, Branch: rows[i].Source, Target: rows[i].Target,
			Outcome: r.Outcome, Skip: r.Skip, Pushed: r.Pushed, URL: r.URL, Auth: r.Auth, Error: r.Error})
		pushed = pushed || r.Pushed
		switch r.Outcome {
		case gitops.MRCreated:
			run.Created++
		case gitops.MRExists:
			run.Existing++
		case gitops.Skipped:
			run.Skipped++
		default:
			run.Failed++
		}
	}
	task.end(nil, false)
	if pushed {
		s.afterWrite("mrCreate", nil) // origin/<branch> moved
	}
	return run, nil
}

var (
	mrNumber = regexp.MustCompile(`/(?:-/)?merge_requests/(\d+)`)
	prNumber = regexp.MustCompile(`https://github\.com/[^/]+/[^/]+/pull/(\d+)`)
)

// MRRef is how a merge or pull request's URL is referred to: "MR !12", "PR #12"; "" when the URL is neither.
func MRRef(url string) string {
	if m := mrNumber.FindStringSubmatch(url); m != nil {
		return "MR !" + m[1]
	}
	if m := prNumber.FindStringSubmatch(url); m != nil {
		return "PR #" + m[1]
	}
	return ""
}

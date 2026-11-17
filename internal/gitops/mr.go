// Package gitops plans and runs the batch writes. Every git, glab and gh command goes
// through gitcli's fixed functions; nothing here touches a directory with uncommitted changes, and only 创建 MR pushes.
package gitops

import (
	"context"
	"errors"
	"strings"

	"sheaf/internal/gitcli"
)

// 创建 MR: push the branch to origin when origin lacks some of its commits, then open a merge request
// into the row's target with gh for github.com and glab for other hosts.

// Why a row gets no merge request.
const (
	SkipNoBranch  = "no_branch"  // the row has no branch of its own
	SkipNoTarget  = "no_target"  // no branch to merge into (no compare target, or the repo lacks it)
	SkipIsTarget  = "is_target"  // the branch is the target itself
	SkipNoProject = "no_project" // origin's URL names no GitLab project, and glab must not guess one (--repo)
	SkipDiverged  = "diverged"   // the local branch and origin's each have commits the other lacks: pushing would need a force
)

// Outcomes of a row, besides Skipped and Failed.
const (
	MRCreated = "created"
	MRExists  = "exists" // an open merge request from the branch was there already
)

// MRRow is one branch to open a merge request for.
type MRRow struct {
	Key     string
	Dir     string // the repo's main working directory
	Source  string // the branch's name on origin (and locally, when Local)
	Local   bool   // refs/heads/<Source> exists: push it when origin lacks some of it
	Ahead   int    // commits of the local branch origin/<Source> lacks; -1 = origin has no such branch
	Behind  int    // commits of origin/<Source> the local branch lacks; 0 when origin has no such branch
	Target  string // short name of the branch on origin to merge into
	Tool    string
	Project string
	Missing bool
}

// PrepareMR derives the tool from origin, never from another remote or CLI defaults.
func PrepareMR(ctx context.Context, r MRRow) (MRRow, error) {
	r.Tool, r.Project, r.Missing = gitcli.ToolGlab, "", false
	project, err := gitcli.GlabProject(ctx, r.Dir)
	if errors.Is(err, gitcli.ErrNoProject) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	r.Project = project
	host, projectPath, _ := strings.Cut(project, "/")
	host, _, _ = strings.Cut(host, ":")
	if strings.EqualFold(host, "github.com") {
		r.Tool = gitcli.ToolGh
		r.Project = "github.com/" + projectPath
		r.Missing = !gitcli.HasGh()
	} else {
		r.Missing = !gitcli.HasGlab()
	}
	return r, nil
}

// Push reports whether the row pushes first.
func (r MRRow) Push() bool { return r.Local && r.Ahead != 0 }

// DecideMR says why a row gets no merge request; "" = it gets one. A repo without origin has nowhere to push or open it.
func DecideMR(ctx context.Context, r MRRow) (skip string, err error) {
	switch {
	case r.Source == "":
		return SkipNoBranch, nil
	case r.Target == "":
		return SkipNoTarget, nil
	case r.Source == r.Target:
		return SkipIsTarget, nil
	}
	ok, err := gitcli.HasRemote(ctx, r.Dir, "origin")
	if err != nil || !ok {
		return SkipNoRemote, err
	}
	if r.Local && r.Ahead > 0 && r.Behind > 0 {
		return SkipDiverged, nil
	}
	if _, err := gitcli.GlabProject(ctx, r.Dir); err != nil {
		if errors.Is(err, gitcli.ErrNoProject) {
			return SkipNoProject, nil
		}
		return "", err
	}
	return "", nil
}

// MRCommands is what a row runs, as the sheet shows it (the command log has the full lines).
func MRCommands(r MRRow) []string {
	out := []string{}
	if r.Push() {
		out = append(out, "git push --set-upstream origin "+r.Source)
	}
	if r.Tool == gitcli.ToolGh {
		return append(out, "gh pr create --repo="+r.Project+" --base="+r.Target+" --head="+r.Source+" --fill")
	}
	return append(out, "glab mr create --repo="+r.Project+" --source-branch="+r.Source+" --target-branch="+r.Target+" --fill --yes --push=false")
}

// MRResult is what happened to one row.
type MRResult struct {
	Tool    string
	Missing bool
	Key     string
	Outcome string
	Skip    string
	Pushed  bool
	URL     string // the merge request, created or found
	Auth    bool   // failed because the selected tool is not logged in
	Error   string
}

// RunMR opens the rows' merge or pull requests one after another.
func RunMR(ctx context.Context, rows []MRRow, p Progress) []MRResult {
	out := make([]MRResult, 0, len(rows))
	stopped := false
	for i, r := range rows {
		stopped = stopped || !p.start(i)
		if stopped {
			out = append(out, MRResult{Key: r.Key, Tool: r.Tool, Missing: r.Missing, Outcome: Skipped, Skip: SkipStopped})
		} else {
			out = append(out, runMR(ctx, r))
		}
		p.done(i)
	}
	return out
}

func runMR(ctx context.Context, r MRRow) MRResult {
	prepared, prepErr := PrepareMR(ctx, r)
	r = prepared
	res := MRResult{Key: r.Key, Tool: r.Tool, Missing: r.Missing}
	if prepErr != nil {
		return mrFailed(res, prepErr)
	}
	skip, err := DecideMR(ctx, r)
	if err != nil {
		return mrFailed(res, err)
	}
	if skip != "" {
		res.Outcome, res.Skip = Skipped, skip
		return res
	}
	if r.Missing {
		if r.Tool == gitcli.ToolGh {
			return mrFailed(res, gitcli.ErrNoGh)
		}
		return mrFailed(res, gitcli.ErrNoGlab)
	}
	find, create := gitcli.GlabMRFind, gitcli.GlabMRCreate
	if r.Tool == gitcli.ToolGh {
		find, create = gitcli.GhPRFind, gitcli.GhPRCreate
	}
	if r.Push() {
		if err := gitcli.Push(ctx, r.Dir, "origin", r.Source); err != nil {
			if pushRejected(err) { // origin moved since the plan: the same case as SkipDiverged, found late
				res.Outcome, res.Skip = Skipped, SkipDiverged
				return res
			}
			return mrFailed(res, err)
		}
		res.Pushed = true
	}
	project := r.Project
	url, err := find(ctx, r.Dir, project, r.Source, r.Target)
	if err != nil {
		return mrFailed(res, err)
	}
	if url != "" {
		res.Outcome, res.URL = MRExists, url
		return res
	}
	url, err = create(ctx, r.Dir, project, r.Source, r.Target)
	if errors.Is(err, gitcli.ErrNoMRAddress) || errors.Is(err, gitcli.ErrNoPRAddress) {
		// A request may exist even when the CLI prints no address; ask before trying another create.
		if found, ferr := find(ctx, r.Dir, project, r.Source, r.Target); ferr == nil && found != "" {
			url, err = found, nil
		}
	}
	if err != nil {
		return mrFailed(res, err)
	}
	res.Outcome, res.URL = MRCreated, url
	return res
}

// pushRejected: git refused the push because origin has commits the branch lacks (never forced).
func pushRejected(err error) bool {
	m := err.Error()
	return strings.Contains(m, "non-fast-forward") || strings.Contains(m, "fetch first")
}

func mrFailed(res MRResult, err error) MRResult {
	res.Outcome, res.Error, res.Auth = Failed, err.Error(), errors.Is(err, gitcli.ErrGlabAuth) || errors.Is(err, gitcli.ErrGhAuth)
	return res
}

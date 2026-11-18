package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"sheaf/internal/session"
)

func render(w io.Writer, e envelope, exit int) error {
	var b strings.Builder
	if e.Error != nil {
		fmt.Fprintf(&b, "%s: %s\n", e.Error.Code, e.Error.Message)
		for _, root := range e.Error.Workspaces {
			fmt.Fprintf(&b, "  sheaf --workspace %q %s\n", root, e.Scope.Command)
		}
		if e.Error.Code == "not_found" {
			fmt.Fprintf(&b, "Next: verify the requirement number and workspace; inspect the configured number rule with sheaf --workspace %q config show.\n", e.Workspace)
			_, err := io.WriteString(w, b.String())
			return err
		}
	}
	if help, ok := e.Result.(string); ok {
		b.WriteString(help)
	} else {
		summary := e.Scope.Command == "status" || e.Scope.Command == "check"
		if !summary && e.Workspace != "" {
			fmt.Fprintf(&b, "Workspace: %s | complete: %t\n", e.Workspace, e.Complete)
		}
		switch result := e.Result.(type) {
		case workspacesResult:
			if len(result.Items) == 0 {
				fmt.Fprintln(&b, "No known workspaces; use sheaf --workspace <root> status to register one.")
			}
			for _, item := range result.Items {
				fmt.Fprintf(&b, "%s  %s  tracked %d, archived %d", item.Name, item.Root, item.Tracked, item.Archived)
				if item.Current {
					b.WriteString(" (current)")
				}
				if !item.Exists {
					b.WriteString(" (missing)")
				}
				b.WriteByte('\n')
			}
		case statusResult:
			fmt.Fprintf(&b, "%d tracked requirements (%s) | complete: %t\n", len(result.Items), e.Scope.Selection, e.Complete)
			if len(result.Items) == 0 {
				fmt.Fprintln(&b, "No requirements in this scope; use track <id>, status --all, or change --search.")
			}
			for _, item := range result.Items {
				renderSummary(&b, item.ReqSummary, item.Flow)
			}
		case session.ReqReport:
			renderReport(&b, result, e.Workspace)
		case checkResult:
			conclusion := "Passed: all checked requirements meet the coverage rules."
			if len(result.Items) == 0 {
				conclusion = "No check objects."
			} else if exit == 2 || !e.Complete {
				conclusion = "Incomplete: coverage could not be fully determined."
			} else if !result.Passed {
				conclusion = "Not finished: commits or uncommitted work remain."
			}
			fmt.Fprintln(&b, conclusion)
			if result.Stage != "" {
				fmt.Fprintf(&b, "Check target: %s (default flow progress is shown separately)\n", result.Stage)
			}
			for _, item := range result.Items {
				renderSummary(&b, item.ReqSummary, item.Flow)
				if e.Scope.Requirement != "" || result.Stage != "" {
					renderMissing(&b, item, result.Stage)
				}
			}
		case commitResult:
			fmt.Fprintf(&b, "%s: %d commits on this page | tracked: %t | archived: %t\n", result.Requirement.ID, len(result.Items), result.Requirement.Tracked, result.Requirement.Archived)
			for _, item := range result.Items {
				fmt.Fprintf(&b, "  %s %s %s %s — %s\n    %s\n", item.Repo, item.SHA, time.Unix(item.Time, 0).UTC().Format(time.RFC3339), item.Author, item.Subject, item.Path)
				if item.BodyErr != "" {
					fmt.Fprintf(&b, "    Body unreadable: %s\n", item.BodyErr)
				} else if result.Full && item.Body != nil {
					fmt.Fprintf(&b, "%s\n", *item.Body)
				}
			}
			if result.End {
				fmt.Fprintln(&b, "End of commits.")
			} else {
				fmt.Fprintf(&b, "Next page: sheaf --workspace %q commits %s --limit %d --cursor %q", e.Workspace, e.Scope.Requirement, e.Scope.Limit, result.Next)
				if e.Scope.Repo != "" {
					fmt.Fprintf(&b, " --repo %q", e.Scope.Repo)
				}
				fmt.Fprintln(&b)
			}
		case managementResult:
			fmt.Fprintf(&b, "%s %s | changed: %t | tracked: %t | archived: %t\n", result.Action, result.Requirement.ID, result.Changed, result.Requirement.Tracked, result.Requirement.Archived)
			if result.Requirement.Progress != "" {
				renderSummary(&b, result.Requirement, nil)
			}
		case json.RawMessage:
			fmt.Fprintf(&b, "Data directory: %s\nConfig file: %s\n%s\n", e.DataDir, e.ConfigFile, result)
		}
		if !summary && e.Scope.ObservedAt != 0 {
			fmt.Fprintf(&b, "Observed: %s | source: local Git refs | last successful fetch time: unknown\n", time.Unix(e.Scope.ObservedAt, 0).UTC().Format(time.RFC3339))
			var fields []string
			if len(e.Scope.Repositories) > 0 {
				fields = append(fields, "Repositories: "+strings.Join(e.Scope.Repositories, ", "))
			}
			if len(e.Scope.Excluded) > 0 {
				fields = append(fields, "excluded: "+strings.Join(e.Scope.Excluded, ", "))
			}
			if len(fields) > 0 {
				fmt.Fprintln(&b, strings.Join(fields, " | "))
			}
		}
		for _, problem := range e.Problems {
			fields := []string{"Problem " + problem.Code}
			if problem.Repo != "" {
				fields = append(fields, "repo: "+problem.Repo)
			}
			if problem.Stage != "" {
				fields = append(fields, "stage: "+problem.Stage)
			}
			if !summary && problem.Detail != "" {
				fields = append(fields, problem.Detail)
			}
			fmt.Fprintln(&b, strings.Join(fields, " | "))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func progress(stage string, flow []session.MergeStep) string {
	if at, ok := session.MergeStageIndex(stage); ok && at < len(flow) {
		return "awaiting " + flow[at].Name
	}
	switch stage {
	case session.StageLanded:
		return "flow complete"
	case session.StageDev:
		return "in development"
	case session.StageUnknown:
		return "unknown"
	case session.StageReading:
		return "reading uncommitted changes"
	}
	return "awaiting merge"
}

func renderSummary(b *strings.Builder, r session.ReqSummary, flow []session.MergeStep) {
	label := r.ID
	if r.Title != "" {
		label += " " + r.Title
	}
	fmt.Fprintf(b, "%s | %s | archived: %t | repos: %d | uncommitted: %d\n", label, progress(r.Progress, flow), r.Archived, r.Repos, r.Uncommitted)
	for _, gap := range r.Gaps {
		fields := []string{gap.Repo}
		if gap.Unknown != "" {
			fields = append(fields, "cannot determine: "+gap.Unknown)
		} else if gap.Next != "" {
			fields = append(fields, fmt.Sprintf("next=%s missing=%d", gap.Next, gap.Missing))
		}
		if gap.Uncommitted > 0 {
			fields = append(fields, fmt.Sprintf("uncommitted=%d", gap.Uncommitted))
		}
		fmt.Fprintf(b, "  %s\n", strings.Join(fields, " | "))
	}
}

func renderMissing(b *strings.Builder, r session.ReqReport, target string) {
	seen := map[string]bool{}
	for _, row := range r.Rows {
		for _, stage := range row.Stages {
			if !stage.Applies {
				continue
			}
			if target != "" {
				if stage.Name != target {
					continue
				}
			} else if !slices.ContainsFunc(r.Gaps, func(g session.RepoGap) bool { return g.Repo == row.Repo && g.Next == stage.Name }) {
				continue
			}
			for _, commit := range stage.Missing {
				key := row.Repo + ":" + commit.SHA
				if !seen[key] {
					seen[key] = true
					fmt.Fprintf(b, "    %s %s — %s\n", row.Repo, commit.SHA[:min(7, len(commit.SHA))], commit.Subject)
				}
			}
		}
	}
}

func renderReport(b *strings.Builder, r session.ReqReport, workspace string) {
	renderSummary(b, r.ReqSummary, r.Flow)
	fmt.Fprintf(b, "  Tracked: %t\n", r.Tracked)
	if !r.Found {
		fmt.Fprintln(b, "  No locatable records or associated working directories; progress cannot be determined.")
	}
	if r.Uncommitted > 0 {
		fmt.Fprintln(b, "  Uncommitted work remains; it may not have produced a commit yet.")
	}
	if len(r.Flow) > 0 {
		steps := make([]string, 0, len(r.Flow))
		for _, step := range r.Flow {
			label := step.Name
			if step.Branch != "" && step.Branch != step.Name {
				label += " (" + step.Branch + ")"
			}
			steps = append(steps, label)
		}
		fmt.Fprintf(b, "  Flow: %s\n", strings.Join(steps, " → "))
	}
	for _, row := range r.Rows {
		fields := []string{row.Repo}
		if row.Path != "" {
			fields = append(fields, row.Path)
		}
		if row.Branch != "" {
			fields = append(fields, "branch: "+row.Branch)
		}
		if row.Worktree != "" {
			fields = append(fields, "worktree: "+row.Worktree)
		}
		fields = append(fields, fmt.Sprintf("deleted: %t", row.Deleted), fmt.Sprintf("commits: %d", row.Commits), fmt.Sprintf("uncommitted: %d", row.Uncommitted))
		fmt.Fprintf(b, "  %s\n", strings.Join(fields, " | "))
		if row.UncommittedFiles != nil {
			files := *row.UncommittedFiles
			multiple := len(files) > 0 && files[0].Worktree != files[len(files)-1].Worktree
			worktree := ""
			for _, file := range files {
				indent := "    "
				if multiple {
					if file.Worktree != worktree {
						fmt.Fprintf(b, "    %s\n", file.Worktree)
						worktree = file.Worktree
					}
					indent = "      "
				}
				path := file.Path
				if file.OldPath != "" {
					path = file.OldPath + " -> " + path
				}
				fmt.Fprintf(b, "%s%s %s %s\n", indent, file.Status, file.Area, path)
			}
		} else if row.Uncommitted > 0 {
			fmt.Fprintln(b, "    run with --files to list them")
		}
		for _, stage := range row.Stages {
			if !stage.Applies {
				fmt.Fprintf(b, "    %s: not applicable", stage.Name)
				if stage.Skip != "" {
					fmt.Fprintf(b, " (%s)", stage.Skip)
				}
				fmt.Fprintln(b)
				continue
			}
			target := []string{}
			if stage.Ref != "" {
				target = append(target, stage.Ref)
			}
			if stage.SHA != "" {
				target = append(target, stage.SHA)
			}
			if len(target) > 0 {
				source := "local fallback"
				if stage.Remote {
					source = "remote-tracking"
				}
				target = append(target, "("+source+")")
			} else {
				target = append(target, "target unavailable")
			}
			fmt.Fprintf(b, "    %s: %s | covered=%d missing=%d\n", stage.Name, strings.Join(target, " "), len(stage.Covered), len(stage.Missing))
			for _, c := range stage.Covered {
				fmt.Fprintf(b, "      covered %s %s — %s\n", c.By, c.SHA, c.Subject)
			}
			for _, c := range stage.Missing {
				fmt.Fprintf(b, "      missing %s — %s\n", c.SHA, c.Subject)
			}
		}
	}
	fmt.Fprintf(b, "  Read commits: sheaf --workspace %q commits %s --json\n", workspace, r.ID)
}

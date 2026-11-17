// Package cli adapts workspace sessions to noninteractive commands.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sheaf/internal/appdir"
	"sheaf/internal/session"
)

type scope struct {
	Command        string        `json:"command"`
	Selection      session.Scope `json:"selection,omitempty"`
	Requirement    string        `json:"requirement,omitempty"`
	Stage          string        `json:"stage,omitempty"`
	Repo           string        `json:"repo,omitempty"`
	Limit          int           `json:"limit,omitempty"`
	Cursor         string        `json:"cursor,omitempty"`
	Search         string        `json:"search,omitempty"`
	Repositories   []string      `json:"repositories"`
	Excluded       []string      `json:"excluded"`
	ObservedAt     int64         `json:"observedAt"`
	Source         string        `json:"source"`
	FetchRequested bool          `json:"fetchRequested"`
	// There is no persisted last-fetch timestamp in a workspace session.
	LastFetchAt *int64 `json:"lastFetchAt"`
}

type envelope struct {
	Version      int               `json:"version"`
	Workspace    string            `json:"workspace"`
	Scope        scope             `json:"scope"`
	Complete     bool              `json:"complete"`
	Problems     []session.Problem `json:"problems"`
	Result       any               `json:"result"`
	Error        *commandError     `json:"error,omitempty"`
	DataDir      string            `json:"dataDir,omitempty"`
	ConfigFile   string            `json:"configFile,omitempty"`
	NextCommands [][]string        `json:"nextCommands,omitempty"`
}

type commandError struct {
	Code       string   `json:"code"`
	Message    string   `json:"message"`
	Workspaces []string `json:"workspaces,omitempty"`
}

type workspacesResult struct {
	Items []session.KnownWorkspace `json:"items"`
}

type checkResult struct {
	Passed bool                `json:"passed"`
	Stage  string              `json:"stage,omitempty"`
	Items  []session.ReqReport `json:"items"`
}

type statusResult struct {
	ObservedAt int64               `json:"observedAt"`
	Excluded   []string            `json:"excluded"`
	Flow       []session.MergeStep `json:"flow"`
	Items      []session.ReqReport `json:"items"`
}

type commitResult struct {
	session.CommitPage
	Requirement session.ReqReport `json:"requirement"`
	End         bool              `json:"end"`
	Full        bool              `json:"-"`
}

type managementResult struct {
	Action      string             `json:"action"`
	Changed     bool               `json:"changed"`
	Requirement session.ReqSummary `json:"requirement"`
}

func (e *envelope) fail(code string, err error, exit int) int {
	e.Complete = false
	e.Error = &commandError{Code: code, Message: err.Error()}
	return exit
}

// Main consumes os.Args and returns an exit code; it never exits the process.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		args = []string{"sheaf"}
	}
	if len(args) > 1 && args[1] == "__complete" {
		writeCompletions(args[0], args[2:], stdout)
		return 0
	}
	o, err := parse(args[1:])
	e := envelope{Version: 1, Complete: true, Problems: []session.Problem{},
		Scope: scope{Command: o.command, Requirement: o.id, Repo: o.repo, Stage: o.stage,
			Search: o.search, Repositories: []string{}, Excluded: []string{}, Source: "local", FetchRequested: o.fetch}}
	exit := 0
	if err != nil {
		exit = e.fail("invalid_input", err, 3)
	} else if o.help {
		e.Result = helpText(o.topic)
	} else if o.command == "completion" {
		if _, err := io.WriteString(stdout, completionScript(filepath.Base(args[0]), o.action)); err != nil {
			fmt.Fprintf(stderr, "write completion script: %v\n", err)
			return 2
		}
		return 0
	} else if o.command == "version" {
		if o.json {
			if err := json.NewEncoder(stdout).Encode(struct {
				Version int               `json:"version"`
				Result  map[string]string `json:"result"`
			}{1, map[string]string{"sheaf": version}}); err != nil {
				fmt.Fprintf(stderr, "write JSON: %v\n", err)
				return 2
			}
			return 0
		}
		e.Result = "sheaf " + version + "\n"
	} else {
		exit = run(context.Background(), args[0], o, &e, stderr)
	}
	if o.json || jsonRequested(args[1:]) {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(e); err != nil {
			fmt.Fprintf(stderr, "write JSON: %v\n", err)
			return 2
		}
	} else if err := render(stdout, e, exit); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return 2
	}
	return exit
}

func run(ctx context.Context, name string, o options, e *envelope, stderr io.Writer) int {
	dev := appdir.Dev(filepath.Base(name))
	root := appdir.Root(dev)
	data, cache := appdir.Data(root), appdir.Cache(root)
	if o.command == "workspaces" {
		e.DataDir = data
		cwd, err := os.Getwd()
		if err != nil {
			return e.fail("data_directory", err, 2)
		}
		items, err := session.KnownWorkspaces(data, cache, cwd)
		if err != nil {
			return e.fail("data_directory", err, 2)
		}
		e.Result = workspacesResult{Items: items}
		return 0
	}
	workspace := o.workspace
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return e.fail("workspace", err, 3)
		}
		matches, err := session.FindWorkspace(data, cache, cwd)
		if err != nil {
			return e.fail("workspace_read", err, 2)
		}
		if len(matches) != 1 {
			if len(matches) == 0 {
				matches = knownWorkspaces(data)
			}
			exit := e.fail("workspace", fmt.Errorf("cannot locate one workspace for %s; specify --workspace <root>", cwd), 3)
			e.Error.Workspaces = matches
			return exit
		}
		workspace = matches[0]
	}
	workspace, err := filepath.Abs(session.CanonicalRoot(data, workspace))
	if err != nil {
		return e.fail("workspace", err, 3)
	}
	e.Workspace = workspace
	e.DataDir = data
	e.ConfigFile = filepath.Join(data, "workspaces", session.WorkspaceID(workspace), "workspace.json")
	write := o.command == "track" || o.command == "untrack" || o.command == "archive" || o.command == "unarchive" || o.command == "config" && o.action == "apply"
	unlock, lockErr := appdir.TryLock(ctx, data, session.WorkspaceID(workspace))
	if lockErr != nil && (!errors.Is(lockErr, appdir.ErrBusy) || write) {
		if errors.Is(lockErr, appdir.ErrBusy) {
			return e.fail("workspace_busy", fmt.Errorf("workspace %s is busy; use the GUI or close that workspace and retry", workspace), 4)
		}
		return e.fail("workspace_lock", lockErr, 2)
	}
	if unlock != nil {
		defer unlock()
	}
	// Open does not surface malformed or unreadable saved configuration.
	// An explicit replacement must still be able to recover malformed JSON.
	if !(o.command == "config" && o.action == "apply") {
		if b, err := os.ReadFile(e.ConfigFile); err == nil {
			var cfg session.Config
			if err := json.Unmarshal(b, &cfg); err != nil {
				return e.fail("config_read", err, 2)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return e.fail("config_read", err, 2)
		}
	}
	s, err := session.Open(ctx, workspace, session.Options{DataDir: data, CacheDir: cache, NoAutoRefresh: true, ReuseScan: !o.rescan,
		CacheWrite: func(write func()) {
			if unlock != nil {
				write()
			}
		}})
	if err != nil {
		return e.fail("workspace", err, 3)
	}
	defer s.Close()
	if o.id != "" {
		id, ok := s.NormalizeRequirement(o.id)
		if !ok {
			return e.fail("invalid_requirement", fmt.Errorf("%q is not a complete requirement number for this workspace", o.id), 3)
		}
		o.id, e.Scope.Requirement = id, id
	}
	if o.command == "config" {
		if o.action == "apply" {
			b, err := os.ReadFile(o.file)
			if err != nil {
				return e.fail("config_input", fmt.Errorf("read config %s: %w", o.file, err), 3)
			}
			var wrapped struct {
				Version int             `json:"version"`
				Result  json.RawMessage `json:"result"`
			}
			if json.Unmarshal(b, &wrapped) == nil && wrapped.Version != 0 {
				if wrapped.Version != 1 || len(wrapped.Result) == 0 {
					return e.fail("config_input", errors.New("expected a version 1 config show result"), 3)
				}
				b = wrapped.Result
			}
			if err := s.ApplyConfig(b); err != nil {
				return e.fail("config_input", err, 3)
			}
		}
		e.Result = json.RawMessage(s.ConfigJSON())
		return 0
	}
	if o.stage != "" && !slices.ContainsFunc(s.MergeFlow(), func(step session.MergeStep) bool { return step.Name == o.stage }) {
		return e.fail("invalid_stage", fmt.Errorf("unknown stage %q; use config show to see the merge flow", o.stage), 3)
	}
	if o.fetch {
		fmt.Fprintln(stderr, "Refreshing workspace remotes…")
		// FetchAll selects from discovered repos; a first invocation has no scan cache yet.
		if err := s.Refresh(ctx); err != nil {
			e.Problems = append(e.Problems, session.Problem{Code: session.ProblemGitRead, Detail: err.Error()})
		}
		run, err := s.FetchAll(ctx, nil, "CLI refresh remotes")
		if err != nil {
			e.Problems = append(e.Problems, session.Problem{Code: session.ProblemFetchFailed, Detail: err.Error()})
		}
		for _, row := range run.Rows {
			if row.Outcome == "failed" || row.Outcome == "skipped" || row.Outcome == "stopped" {
				e.Problems = append(e.Problems, session.Problem{Code: session.ProblemFetchFailed, Repo: row.Repo, Detail: row.Error})
			}
		}
	}
	// Track/untrack require no Git evidence. Archive returns the current progress.
	switch {
	case o.command == "track" || o.command == "untrack":
	case o.command == "commits":
		e.Problems = append(e.Problems, s.SettleIndex(ctx)...)
	case o.id != "":
		e.Problems = append(e.Problems, s.Settle(ctx, []string{o.id})...)
	case o.command == "status" || o.command == "check":
		scope := session.ScopeActive
		if o.command == "status" {
			scope = selection(o)
		}
		ids, err := s.ScopeIDs(scope)
		if err != nil {
			return e.fail("query_failed", err, 2)
		}
		e.Problems = append(e.Problems, s.Settle(ctx, ids)...)
	default:
		e.Problems = append(e.Problems, s.Settle(ctx, nil)...)
	}
	info := s.Info()
	e.Scope.Excluded = append([]string{}, info.Ignored...)
	for _, repo := range info.Repos {
		e.Scope.Repositories = append(e.Scope.Repositories, repo.Name)
	}
	e.Scope.ObservedAt = info.State.Updated
	var exit int
	switch o.command {
	case "status":
		e.Scope.Selection = selection(o)
		result, err := s.Overview(ctx, e.Scope.Selection, o.search)
		if err != nil {
			return e.fail("query_failed", err, 2)
		}
		out := statusResult{ObservedAt: result.ObservedAt, Excluded: result.Excluded, Flow: s.MergeFlow(), Items: []session.ReqReport{}}
		for _, summary := range result.Items {
			r, err := s.Report(ctx, summary.ID)
			if err != nil {
				return e.fail("query_failed", err, 2)
			}
			out.Items = append(out.Items, r)
		}
		e.Result = out
		e.Problems = append(e.Problems, result.Problems...)
	case "show", "commits", "check":
		exit = query(ctx, s, o, e)
	default:
		exit = manage(ctx, s, o, e)
	}
	e.Problems = uniqueProblems(e.Problems)
	if len(e.Problems) > 0 {
		e.Complete = false
		if exit < 2 {
			exit = 2
		}
	}
	return exit
}

func selection(o options) session.Scope {
	if o.all {
		return session.ScopeAll
	}
	if o.archived {
		return session.ScopeArchived
	}
	return session.ScopeActive
}

func query(ctx context.Context, s *session.Session, o options, e *envelope) int {
	if o.command == "check" && o.id == "" {
		e.Scope.Selection = session.ScopeActive
		list, err := s.Overview(ctx, session.ScopeActive, "")
		if err != nil {
			return e.fail("query_failed", err, 2)
		}
		out := checkResult{Passed: true, Stage: o.stage, Items: []session.ReqReport{}}
		e.Problems = append(e.Problems, list.Problems...)
		for _, summary := range list.Items {
			r, err := report(ctx, s, summary.ID, o.stage)
			if err != nil {
				return e.fail("query_failed", err, 2)
			}
			out.Items = append(out.Items, r)
			e.Problems = append(e.Problems, r.Problems...)
			if !passed(r) {
				out.Passed = false
			}
		}
		if len(out.Items) == 0 {
			out.Passed = false
			e.Result = out
			return e.fail("no_objects", errors.New("no active tracked requirements to check; use track <id> or check <id>"), 2)
		}
		if len(e.Problems) > 0 {
			out.Passed = false
		}
		e.Result = out
		if !out.Passed {
			return 1
		}
		return 0
	}
	var r session.ReqReport
	var err error
	if o.files {
		r, err = s.ReportWithFiles(ctx, o.id)
	} else {
		r, err = report(ctx, s, o.id, o.stage)
	}
	if err != nil {
		return e.fail("query_failed", err, 2)
	}
	e.Problems = append(e.Problems, r.Problems...)
	if !r.Found && !r.Tracked {
		e.Result = r
		return e.fail("not_found", fmt.Errorf("requirement %s was not found in local history or working directories", o.id), 3)
	}
	switch o.command {
	case "show":
		e.Result = r
		e.NextCommands = [][]string{{"sheaf", "--workspace", e.Workspace, "commits", r.ID, "--json"}}
	case "check":
		ok := passed(r) && len(e.Problems) == 0
		e.Result = checkResult{Passed: ok, Stage: o.stage, Items: []session.ReqReport{r}}
		if !ok {
			return 1
		}
	case "commits":
		if o.repo != "" && !slices.ContainsFunc(s.Info().Repos, func(repo session.RepoInfo) bool { return repo.Name == o.repo }) {
			return e.fail("invalid_repo", fmt.Errorf("repository %q is not included in this workspace", o.repo), 3)
		}
		page, err := s.ReqCommits(ctx, o.id, o.repo, o.cursor, o.limit)
		if err != nil {
			if strings.Contains(err.Error(), "invalid cursor") {
				return e.fail("invalid_cursor", err, 3)
			}
			return e.fail("query_failed", err, 2)
		}
		for _, item := range page.Items {
			if item.BodyErr != "" {
				e.Problems = append(e.Problems, session.Problem{Code: "commit_body_unreadable", Repo: item.Repo, Detail: item.SHA + ": " + item.BodyErr})
			}
		}
		e.Scope.Limit, e.Scope.Cursor = o.limit, o.cursor
		e.Result = commitResult{CommitPage: page, Requirement: r, End: page.Next == "", Full: o.full}
	}
	return 0
}

func report(ctx context.Context, s *session.Session, id, stage string) (session.ReqReport, error) {
	if stage != "" {
		return s.StageCheck(ctx, id, stage)
	}
	return s.Report(ctx, id)
}

func passed(r session.ReqReport) bool {
	return r.Found && len(r.Problems) == 0 && len(r.Gaps) == 0 && r.Uncommitted == 0
}

func manage(ctx context.Context, s *session.Session, o options, e *envelope) int {
	var cfg session.Config
	if err := json.Unmarshal(s.ConfigJSON(), &cfg); err != nil {
		return e.fail("config_read", err, 2)
	}
	tracked, archived := slices.Contains(cfg.Tracked, o.id), slices.Contains(cfg.Archived, o.id)
	changed := false
	var err error
	switch o.command {
	case "track":
		changed = !tracked
		err = s.Track(o.id)
	case "untrack":
		changed = tracked
		err = s.Untrack(o.id)
	case "archive", "unarchive":
		if !tracked {
			return e.fail("not_tracked", fmt.Errorf("requirement %s is not tracked; run track %s first", o.id, o.id), 3)
		}
		changed = archived != (o.command == "archive")
		// ArchiveRequirement always saves: guard repeat commands to preserve the config bytes and mtime.
		if changed {
			err = s.ArchiveRequirement(o.id, o.command == "archive")
		}
	}
	if err != nil {
		return e.fail("config_write", err, 2)
	}
	summary := session.ReqSummary{ID: o.id, Tracked: o.command == "track" || tracked && o.command != "untrack", Archived: archived, Gaps: []session.RepoGap{}}
	if o.command == "archive" || o.command == "unarchive" {
		r, err := s.Report(ctx, o.id)
		if err != nil {
			return e.fail("query_failed", err, 2)
		}
		summary = r.ReqSummary
		e.Problems = append(e.Problems, r.Problems...)
	}
	e.Result = managementResult{Action: o.command, Changed: changed, Requirement: summary}
	return 0
}

func uniqueProblems(in []session.Problem) []session.Problem {
	out := []session.Problem{}
	seen := map[session.Problem]bool{}
	for _, p := range in {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func knownWorkspaces(data string) []string {
	roots := []string{}
	files, _ := filepath.Glob(filepath.Join(data, "workspaces", "*", "workspace.json"))
	for _, file := range files {
		b, err := os.ReadFile(file)
		var cfg session.Config
		if err == nil && json.Unmarshal(b, &cfg) == nil && cfg.Root != "" {
			roots = append(roots, cfg.Root)
		}
	}
	var recent struct {
		Recent []struct {
			Root string `json:"root"`
		} `json:"recent"`
		Open []string `json:"open"`
	}
	if b, err := os.ReadFile(filepath.Join(data, "recent.json")); err == nil && json.Unmarshal(b, &recent) == nil {
		roots = append(roots, recent.Open...)
		for _, item := range recent.Recent {
			if item.Root != "" {
				roots = append(roots, item.Root)
			}
		}
	}
	slices.Sort(roots)
	return slices.Compact(roots)
}

package cli

import (
	"fmt"
	"strings"
)

var version = "dev"

type optionSpec struct {
	name, value, description string
}

type commandSpec struct {
	name, usage, summary string
	options              []optionSpec
	details, effects     string
	exits, result        string
	examples             []string
}

func workspaceOptions(extra ...optionSpec) []optionSpec {
	return append([]optionSpec{
		{"workspace", "<root>", "Use this workspace; otherwise locate a saved workspace from the current directory."},
		{"json", "", "Write one versioned JSON document, including errors; progress goes to stderr."},
		{"rescan", "", "Discover new clones; otherwise reuse the scan and reload known worktrees."},
		{"help", "", "Show this command's help without opening a workspace (alias: -h)."},
	}, extra...)
}

func queryOptions(extra ...optionSpec) []optionSpec {
	return workspaceOptions(append(extra,
		optionSpec{"fetch", "", "Contact remotes and update remote-tracking refs before reading local state."})...)
}

const queryEffects = "Reads local Git state and may cache indexes in the Sheaf data directory. Does not change configuration or user branches/files. Network: only with --fetch."
const queryExits = "0: complete read, even if work remains; 2: incomplete evidence or operational failure; 3: invalid input, workspace or requirement not found; 4: workspace busy when a lock is required."
const writeExits = "0: configuration saved or already in the requested state; 2: operational failure or incomplete observation (the write may have succeeded; inspect changed); 3: invalid input/workspace; 4: workspace busy, close it in the GUI and retry."
const managementFields = "{action, changed, requirement}: action is the command, changed says whether configuration changed, requirement is a summary. See sheaf help json."

var commands = []commandSpec{
	{
		name: "workspaces", usage: "workspaces [--json]",
		summary: "List all known workspaces and their saved tracking counts.",
		options: []optionSpec{
			{"json", "", "Write one versioned JSON document, including errors."},
			{"help", "", "Show this command's help (alias: -h)."},
		},
		details:  "Which workspace should I query? Lists saved workspace roots and recent workspaces, including missing directories. Current marks every workspace containing the current directory or a known repository/worktree. Counts come from saved configuration; unreadable configurations have zero counts.",
		effects:  "Read-only: reads Sheaf metadata without opening a session, locking, calling Git or contacting the network. An absent data directory stays absent.",
		exits:    "0: workspace list read, including an empty list; 2: data directory could not be read (data_directory); 3: invalid arguments/options.",
		result:   "{items: KnownWorkspace[]}: roots sorted by path; items is [] when empty. workspace is empty and dataDir reports the actual data directory. See sheaf help json.",
		examples: []string{"sheaf workspaces", "sheaf workspaces --json"},
	},
	{
		name: "status", usage: "status [--all|--archived] [--search <number or title>] [options]",
		summary: "List tracked requirements and their merge progress.",
		options: queryOptions(
			optionSpec{"all", "", "Include active and archived tracked requirements."},
			optionSpec{"archived", "", "Include only archived tracked requirements; mutually exclusive with --all."},
			optionSpec{"search", "<text>", "Filter by complete requirement number or case-insensitive title text."}),
		details: "Which tracked requirements still need attention? Defaults to active (not archived) tracked requirements. An empty list is a successful read. Archive state is separate from merge progress.",
		effects: queryEffects, exits: queryExits,
		result:   "{observedAt, excluded, flow, items}: items are full requirement reports with rows, stages and gaps. See sheaf help json.",
		examples: []string{"sheaf status --json", "sheaf status --all --search PAY-142 --json", "sheaf status --rescan"},
	},
	{
		name: "show", usage: "show <id> [options]",
		summary: "Explain one requirement with repository paths and coverage evidence.",
		options: queryOptions(optionSpec{"files", "", "List uncommitted files from cached status; each file includes its worktree."}),
		details: "Where is this requirement, and which commits are still missing from each target?\nArguments:\n  <id>  A complete workspace requirement number, tracked or untracked.\nReading an id never tracks or unarchives it. Rows include absolute repository paths, stages and missing full commit SHAs. Coverage checks do not verify approval, deployment or semantic correctness.",
		effects: queryEffects, exits: queryExits,
		result:   "Requirement report: id, title, tracked, archived, progress, repos, latest, gaps, uncommitted, found, flow, rows, observedAt, problems. nextCommands suggests reading commits. See sheaf help json.",
		examples: []string{"sheaf show PAY-142 --json", "sheaf show PAY-142", "sheaf show PAY-142 --files --json", "sheaf show PAY-142 --fetch --json"},
	},
	{
		name: "commits", usage: "commits <id> [--repo <name>] [--limit <n>] [--cursor <cursor>] [options]",
		summary: "Page through one requirement's commits across repositories.",
		options: queryOptions(
			optionSpec{"repo", "<name>", "Restrict to one included repository name."},
			optionSpec{"limit", "<n>", "Positive page size (default: 50)."},
			optionSpec{"cursor", "<cursor>", "Opaque result.next from the previous page; omit on the first page."},
			optionSpec{"full", "", "Show complete messages in human output; JSON always includes body."}),
		details: "Which commits should I read to investigate this requirement?\nArguments:\n  <id>  A complete workspace requirement number, tracked or untracked.\nRepeat with --cursor using result.next until next is an empty string. Keep the workspace, id and repo filter the same. Read code with git -C <path> show <sha> or git -C <path> diff <a> <b>. A null body requires checking bodyError.",
		effects: queryEffects, exits: queryExits,
		result:   "{items, next, requirement, end}: items contain absolute path, full sha, parents, author, email, time, subject, body and optional bodyError. See sheaf help json.",
		examples: []string{"sheaf commits PAY-142 --json", "sheaf commits PAY-142 --limit 1 --json", "sheaf commits PAY-142 --full"},
	},
	{
		name: "check", usage: "check [id] [--stage <name>] [options]",
		summary:  "Check whether requirements have merged through the flow.",
		options:  queryOptions(optionSpec{"stage", "<name>", "Check only this configured target by name; see config show for names."}),
		details:  "Is everything merged through the workspace's flow?\nArguments:\n  [id]  One complete requirement number; omit to check all active tracked requirements.\nWith --stage, check just that target, even if a later target already covers the commits. Uncommitted files on requirement-named branches also prevent passing. No active tracked requirements means no check objects, not success.",
		effects:  queryEffects,
		exits:    "0: all checked requirements pass; 1: complete check with missing commits or uncommitted work; 2: incomplete evidence, operational failure or no check objects; 3: invalid input, workspace/requirement not found; 4: workspace busy when a lock is required.",
		result:   "{passed, stage?, items}: passed is boolean, stage is the selected target name, items are requirement reports. Default progress is shown separately from the selected target. See sheaf help json.",
		examples: []string{"sheaf check --json", "sheaf check PAY-142 --stage release --json", "sheaf check PAY-142"},
	},
	managementCommand("track", "Add a requirement to the tracked list.", "Should this requirement appear in status? Does not require Git evidence; repeating it is a no-op."),
	managementCommand("untrack", "Remove a requirement from the tracked list.", "Should this requirement stop appearing in status? Preserves its saved title and archive entry; repeating it is a no-op."),
	managementCommand("archive", "Archive a tracked requirement without changing merge progress.", "Should this requirement leave the active list? Requires it to be tracked. Archiving does not mean all changes have merged; repeating it is a no-op."),
	managementCommand("unarchive", "Return a tracked requirement to the active list.", "Should this requirement return to active status? Requires it to be tracked; repeating it is a no-op."),
	{
		name: "config", usage: "config (show | apply <file>) [options]",
		summary:  "Show workspace configuration, or replace it with config apply (writes).",
		options:  workspaceOptions(),
		details:  "How is this workspace configured?\nconfig show:\n  Read the effective workspace configuration and report dataDir and configFile.\nconfig apply <file>:\n  Replace Sheaf configuration with a local UTF-8 JSON file, either raw configuration or the version 1 envelope from config show --json. Export before editing; omitted settings are reset, not patched. Requires a nonempty mergeFlow with distinct names and targets, a valid requirement pattern and valid tracked/archived ids. Invalid input leaves configuration intact. Root is set to the selected workspace.\nArguments:\n  <file>  Path to the replacement JSON file (apply only).",
		effects:  "config show reads configuration. config apply writes only Sheaf configuration; no user repository writes. Neither action contacts the network.",
		exits:    "0: configuration read/applied; 2: operational failure; 3: invalid input, workspace or replacement file; 4: config apply blocked by a busy workspace. config show can read while the GUI holds the lock.",
		result:   "Workspace configuration object for both actions: root, requirement, tracked, archived, mergeFlow, bases, baselineOnly and other saved settings. See sheaf help json.",
		examples: []string{"sheaf config show --json", "sheaf config apply workspace.json --json", "sheaf config show"},
	},
	{
		name: "completion", usage: "completion (zsh | bash)",
		summary:  "Print a shell completion script for zsh or bash.",
		options:  []optionSpec{{"help", "", "Show this command's help (alias: -h)."}},
		details:  "Install Tab completion by adding this line to your shell configuration:\n  zsh (~/.zshrc, after compinit): source <(sheaf completion zsh)\n  bash (~/.bashrc): eval \"$(sheaf completion bash)\"\nUse the executable's actual name when running a renamed binary such as sheaf-dev.",
		effects:  "Writes only stdout. No workspace lookup, Git calls, data directory creation or network.",
		exits:    "0: script printed; 3: invalid shell or arguments.",
		result:   "Shell script (plain text). Dynamic candidates read saved metadata only.",
		examples: []string{"sheaf completion zsh", "sheaf completion bash"},
	},
	{
		name: "help", usage: "help [command|json]",
		summary:  "Read general help, command help or the JSON contract.",
		options:  []optionSpec{{"json", "", "Return the help text as result in a versioned JSON envelope."}, {"help", "", "Show help for this command (alias: -h)."}},
		details:  "How do I call Sheaf and interpret its answers?\nArguments:\n  [command|json]  A command name or json; omit for general help.\nThe equivalent command help form is sheaf <command> --help.",
		effects:  "Writes only stdout. No workspace lookup, Git calls, data directory creation or network.",
		exits:    "0: help displayed; 3: unknown topic or invalid arguments.",
		result:   "Help text (string) when --json is supplied. See sheaf help json.",
		examples: []string{"sheaf help show", "sheaf help json --json"},
	},
	{
		name: "version", usage: "version [--json]",
		summary:  "Print the CLI build version (also --version).",
		options:  []optionSpec{{"json", "", "Write {\"version\":1,\"result\":{\"sheaf\":\"<version>\"}}."}, {"help", "", "Show this command's help (alias: -h)."}, {"version", "", "Print the build version; usable without a command."}},
		details:  "Which Sheaf build is running? Development builds report dev; releases embed the version from mygo.json.",
		effects:  "Writes only stdout. No workspace lookup, Git calls, data directory creation or network.",
		exits:    "0: version displayed; 3: invalid arguments.",
		result:   "{sheaf: string}: build version. The version response contains only version and result.",
		examples: []string{"sheaf --version", "sheaf version --json"},
	},
}

func managementCommand(name, summary, details string) commandSpec {
	return commandSpec{
		name: name, usage: name + " <id> [options]", summary: summary + " (writes)",
		options: workspaceOptions(),
		details: details + "\nArguments:\n  <id>  A complete workspace requirement number.",
		effects: "Writes only Sheaf configuration. Does not change user repository branches/files or contact the network. Archive/unarchive also read the current merge progress.",
		exits:   writeExits, result: managementFields,
		examples: []string{"sheaf " + name + " PAY-142 --json", "sheaf " + name + " PAY-142"},
	}
}

func findCommand(name string) *commandSpec {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

func (c commandSpec) allows(name string) bool {
	if name == "h" {
		name = "help"
	}
	for _, option := range c.options {
		if option.name == name {
			return true
		}
	}
	return false
}

func commandNames() string {
	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.name)
	}
	return strings.Join(names, ", ")
}

const rootUsage = "Usage: sheaf [--workspace <root>] <command> [options]"

func helpText(topic string) string {
	if topic == "json" {
		return wrapHelp(jsonHelp)
	}
	if c := findCommand(topic); c != nil {
		var b strings.Builder
		fmt.Fprintf(&b, "Usage: sheaf %s\n\n%s\n\nOptions (before or after the command):\n", c.usage, c.details)
		for _, option := range c.options {
			label := "--" + option.name
			if option.value != "" {
				label += " " + option.value
			}
			fmt.Fprintf(&b, "  %-20s %s\n", label, option.description)
		}
		fmt.Fprintf(&b, "\nWrites and network:\n  %s\n\nExit codes:\n  %s\n\nResult:\n  %s\n\nExamples:\n", c.effects, c.exits, c.result)
		for _, example := range c.examples {
			fmt.Fprintf(&b, "  %s\n", example)
		}
		return wrapHelp(b.String())
	}
	var b strings.Builder
	b.WriteString(rootUsage + "\n\n")
	b.WriteString(`Sheaf combines commits by requirement number across Git repositories and worktrees, and checks
what remains to merge through the workspace's merge flow.

Concepts:
  workspace: a folder containing repositories/worktrees, with its own Sheaf configuration.
  requirement number: workspace-configured full match; PAY-14 does not match PAY-142.
  tracked / archived: chosen list membership; archived does not mean fully merged.
  merge flow / stage: ordered targets; name is a label, branch is the target; the last is final.
  main branch: config bases[repo], then origin/master, origin/main, master, main; never origin/HEAD.
  empty branch: use each repo's main branch; report stages[].ref, the actual ref read.
  flow scope: only configured targets count; an unconfigured release branch is ignored.
  own commits: subject-matching non-merges and merges with own edits; see sheaf help json.
  covered: an own commit is an ancestor of the target or has the same git patch-id --stable.
  path / worktree: path is the main repo for history; rows[].worktree is for uncommitted changes.
  uncommitted: files count only in worktrees whose branch name contains the requirement number.

Commands:
`)
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-11s %s\n", c.name, c.summary)
	}
	b.WriteString(`
Only track, untrack, archive, unarchive and config apply write Sheaf configuration.
No command writes user repository branches or files. --fetch contacts remotes and updates
remote-tracking refs. Queries may cache indexes inside the Sheaf data directory.

Global options (before or after the command; see command help for applicable options):
  --workspace <root>  Select a workspace explicitly; otherwise locate a saved workspace.
  --json              One versioned JSON document on stdout; progress on stderr.
  --fetch             Refresh remotes before status, show, commits or check (network access).
  --rescan            Discover new clones; workspace commands otherwise reuse the saved scan.
  --help, -h          General help, or help for the selected command.
  --version           Print the build version; supports --json.

Exit codes:
  0  Complete success; status/show/commits still return 0 when a requirement has unfinished work.
  1  check only: complete evidence, but commits or uncommitted work remain.
  2  Incomplete evidence, operational failure, or check has no objects; inspect problems/error.
  3  Invalid input, workspace lookup failure, or requirement not found; inspect error.code.
  4  Workspace busy when a lock is required; writes need the GUI workspace closed before retry.

For AI agents:
  Always pass --json. Decide by the exit code, complete, problems[].code and error.code;
  never parse human text.
  Typical flow: sheaf status --json -> pick an id -> sheaf show <id> --json.
  show --json answers most review questions: main repo paths, actual target refs, stages and
  missing commit SHAs. Report stages[].ref (the actual ref read), not just the stage label.
  If commit details are needed, use sheaf commits <id> --json. Repeat with --cursor <result.next>
  until next is "". Read history with git -C <path> show <sha> or git -C <path> diff <a> <b>.
  For uncommitted changes, pass show <id> --files for the list; use
  git -C <worktree> diff -- <path> for content, taking worktree and path from each file entry.
  Changes on branches without the requirement number are excluded; run git status yourself
  if those changes matter to the review.
  dev takes priority over pending merge stages; inspect gaps[].next / missing for merge gaps.
  Use check only when you need a pass/fail exit code; there is no need to run it after show.
  It answers "is everything merged through the flow"; check <id> --stage <name> checks one target.
  Coverage does not verify approval, deployment or semantic correctness.
  Queries never change tracked/archive state or configuration. Run write commands only when
  the user asks. --fetch contacts remotes.
  If unsure which workspace to use, or lookup fails (exit 3, error.code = workspace), first run
  the command sheaf workspaces --json, choose a root with exists = true and pass --workspace <root>.
  New clones appear only with --rescan; known worktrees are always reloaded. A GUI workspace
  refresh also updates the saved scan; a first query discovers repositories if no scan exists.

Data: ~/.sheaf; a binary named sheaf-dev uses ~/.sheaf-dev. SHEAF_HOME overrides the root.
Config: <data>/workspaces/<id>/workspace.json; config show reports dataDir and configFile.
See also: sheaf help <command>, sheaf help json
`)
	return wrapHelp(b.String())
}

func wrapHelp(text string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		content := strings.TrimLeft(line, " ")
		if gap := strings.Index(content, "  "); gap >= 0 {
			start := len(indent) + gap
			for start < len(line) && line[start] == ' ' {
				start++
			}
			indent = strings.Repeat(" ", start)
		}
		for len(line) > 100 {
			at := strings.LastIndexByte(line[:101], ' ')
			if at <= len(indent) {
				break
			}
			out.WriteString(strings.TrimRight(line[:at], " ") + "\n")
			line = indent + strings.TrimLeft(line[at:], " ")
		}
		out.WriteString(line + "\n")
	}
	return out.String()
}

const jsonHelp = `JSON contract (schema version 1)

Always pass --json. stdout is exactly one JSON document, including errors; progress is on stderr.
Use the exit code, complete, problems[].code and error.code; human text is not a parsing contract.
Optional fields marked ? may be omitted. Arrays may be null when no entries were recorded.
Times are Unix seconds (integer). Paths are absolute unless a field says otherwise.

Envelope:
  version: integer, currently 1; JSON schema version, not the application version.
  workspace: string, resolved workspace root; empty if lookup failed or no workspace was needed.
  scope: object describing what was read:
    command: string, invoked command; selection?: "active" | "archived" | "all" for list queries.
    requirement?: string, normalized full requirement number; stage?: string, selected target name.
    repo?: string, repository filter; limit?: integer, page size; cursor?: string, input cursor.
    search?: string, number/title filter; repositories: string[], included repository names.
    excluded: string[], ignored repository names; source: "local", local Git refs were read.
    observedAt: integer, this invocation's local-state read time; NOT last fetch time. 0 if unread.
    fetchRequested: boolean, whether --fetch was passed, not proof that every fetch succeeded.
    lastFetchAt: integer | null; null means the last successful fetch time is unknown.
  complete: boolean, whether the requested answer has complete evidence; not "fully merged".
  problems: Problem[], evidence gaps or partial failures; partial results can still be useful.
  result: command-specific object (or help string); null if no result could be produced.
  error?: {code: string, message: string, workspaces?: string[]} for failures.
    workspaces lists workspace candidates; retry with --workspace <candidate> for code workspace.
    message is for humans and may include command usage; use code for decisions.
  dataDir?: string, Sheaf data directory; configFile?: string, workspace configuration path.
  nextCommands?: string[][], suggested commands as argument arrays, not shell strings.

Results by command:
  workspaces: {items: KnownWorkspace[]}, all recorded roots sorted by root; items is [] when empty.
    workspace is empty; dataDir is the actual data directory. Does not open or lock a workspace.
  status: {observedAt: integer, excluded: string[], flow: MergeStep[], items: RequirementReport[]}.
    Lists tracked requirements; default selection active, --all includes archived, --archived only
    archived. An empty items array is successful (exit 0).
  show: RequirementReport, for one id even if untracked/archived; never changes its list state.
    show --json answers most review questions; check is only needed for a pass/fail exit code.
    There is no need to run check after show.
  commits: {items: Commit[], next: string, requirement: RequirementReport, end: boolean}.
    next is an opaque cursor; next = "" means the last page, and end = true is equivalent.
    Repeat with --cursor <result.next>, keeping workspace, requirement and repo filter the same.
    --full affects human output only; JSON always includes body.
  check: {passed: boolean, stage?: string, items: RequirementReport[]}.
    passed requires found evidence, no problems, no gaps and no uncommitted work for every item.
    stage is a target name selected by --stage; without id, checks active tracked requirements.
    No check objects gives exit 2 / no_objects; gaps give exit 1 only with complete evidence.
  track / untrack / archive / unarchive:
    {action: string, changed: boolean, requirement: RequirementSummary}.
    changed is false for an idempotent repeat. Archive/unarchive may save and still exit 2 if
    observing progress fails; inspect changed. Track/untrack summaries do not compute progress:
    progress/title may be "", repos/latest/uncommitted are 0 and gaps is empty.
  config show / config apply: WorkspaceConfig, the effective configuration after reading/applying.
    apply accepts raw WorkspaceConfig or a version 1 config show envelope. It replaces settings;
    omitted fields reset. Export before editing; invalid input leaves saved configuration intact.
  help: string, the requested help text when --json is supplied (also <command> --help --json).
  version / --version: {sheaf: string}, the build version (dev for development builds).
    Version uses only {"version":1,"result":{"sheaf":"<version>"}}, without workspace fields.

Shared structures:
  KnownWorkspace:
    root: string, recorded workspace root; name: string, root's base name; id: string, WorkspaceID(root).
    exists: boolean, root is still a directory; current: boolean, the current directory belongs to
    this workspace by saved roots or cached repository/worktree paths. Several roots may be current.
    tracked: integer, tracked requirements that are not archived; archived: integer, archived
    requirements that are still tracked. Unreadable configurations have zero counts.
    configFile: string, saved configuration path; "" for a workspace recorded only in recent.json.
  MergeStep: {name: string, branch: string}. name is only a label and the --stage selector;
    it is not a branch name. branch is the target branch; "" uses each repository's main branch:
    config bases[repo] first, else the first existing ref in order: origin/master, origin/main,
    master, main. origin/HEAD is never used. The last step is final.
    Report stages[].ref, the actual ref read, rather than just the stage label. Branches outside
    the configured flow (e.g. release when not configured) do not participate in the judgment.
  Own commits (coverage input):
    Non-merge commits whose subject contains the complete requirement number, plus merge commits
    whose subject contains that number and which actually introduce their own changes. Only the
    subject is matched, never the body. Clean synchronization merges and unclassified merges do
    not count as own commits. Unclassified merges still produce an unclassified_merge problem.
  RequirementSummary:
    id: string, requirement number; title: string, saved title or newest commit subject without id.
    tracked: boolean; archived: boolean, list state independent of progress.
    progress: string, least advanced repository's stage, regardless of archive state.
    repos: integer, repositories with commits or a requirement-named worktree.
    latest: integer, time of newest commit or branch tip.
    gaps: RepoGap[], repositories that have not met the requested coverage rules.
    uncommitted: integer, changed file count only on requirement-named worktree branches.
  RequirementReport extends RequirementSummary with:
    found: boolean, a commit or named worktree locates it (tracked alone is not evidence).
    flow: MergeStep[]; rows: Row[], per repository/branch evidence.
    observedAt: integer, local-state observation time; problems: Problem[], report-specific issues.
  RepoGap:
    repo: string, repository name; path: string, absolute main repository directory.
    next?: string, next target NAME (not a progress token); missing: integer, uncovered own commits.
    uncommitted: integer, changed file count; unknown?: string, a Problem code preventing judgment.
  Row:
    repo: string; path: string, absolute main repository directory; branch?: string, requirement branch.
    Read commits with git -C <path> show <sha>; path is not the worktree for uncommitted changes.
    worktree?: string, absolute working directory; deleted: boolean, no requirement branch remains.
    Read uncommitted content with git -C <worktree> diff -- <path>, using each file's worktree/path.
    Changes in worktrees whose branch name lacks the requirement number are excluded;
    run git status there if needed.
    commits: integer, all commits whose subject contains the complete requirement number, including
    merge commits. rows[].commits may exceed covered + missing for a target; coverage uses own commits.
    uncommitted: integer, changed file count.
    uncommittedFiles?: UncommittedFile[], only with show --files; [] means no matching changes.
    The list may span multiple worktrees; sorted by worktree, path, then area.
    --files preserves the rows and every other row field.
    uncommitted counts unique paths per worktree; a staged+unstaged file has two list entries.
    stage: string, progress token; stages: StageCheck[], coverage against individual targets.
  UncommittedFile:
    worktree: string, absolute working directory for this file.
    path: string, relative to this file's worktree; oldPath?: string, previous path for renames/copies.
    Read content with git -C <worktree> diff -- <path>, using this entry's worktree and path.
    area: "staged" | "unstaged" | "untracked"; status: "A" | "M" | "D" | "R" | "C" | "T" | "U".
    A file changed in both staged and unstaged areas has two entries.
  StageCheck:
    name: string, target name; applies: boolean; skip?: string, reason an intermediate target is skipped.
    ref?: string, actual ref read; sha?: string, full target hash; remote: boolean.
    remote = false means a local branch was used instead of a remote-tracking branch when a target
    was found; if ref/sha are absent the target was unavailable or skipped. This is not fetch status.
    covered: CoveredCommit[]; missing: CommitRef[], own commits absent from this target.
  CommitRef: {sha: string, subject: string}; sha is a full commit hash.
  CoveredCommit extends CommitRef with by: "ancestor" | "patch" (see values below).
  Commit:
    repo: string; path: string, absolute main repository directory; sha: string, full commit hash.
    parents: string[], full parent hashes; author: string; email: string; time: integer.
    subject: string; body: string | null; bodyError?: string, read failure detail.
    A null body is NOT an empty message: inspect bodyError. An empty string body was read.
  Problem: {code: string, repo?: string, stage?: string, detail?: string}.
    repo is a repository name, stage is a target name, detail is human-readable context.
  WorkspaceConfig:
    root?: string, workspace root set by apply; requirement?: string, requirement-number regex.
    tracked?: string[]; archived?: string[]; requirementNames?: object mapping id to saved title.
    mergeFlow?: MergeStep[], nonempty with distinct target names/branches for apply.
    bases: object mapping repository name to base branch; baselineOnly?: string[], final-only repos.
    ignored?: string[], excluded repos; meManual: string[], manually configured author emails.
    favs?: string[], favorite targets; release?: string; master?: string, legacy flow defaults.
    glabConfig?: string, glab configuration directory. Older configs may have me?: string[] and
    meSet?: boolean (legacy author selection); apply migrates these to meManual.

Progress values (progress and rows[].stage):
  dev         Uncommitted work remains, or tracked without locatable evidence; inspect problems.
              dev takes priority over pending merge stages; merge gaps may still exist. Inspect
              gaps[].next / missing for the next target label and uncovered own commit count.
  unknown     A deleted requirement branch has no own commits to establish coverage.
  merge:<i>   Awaiting flow[i], using a zero-based index (e.g. merge:0, merge:1).
  landed      Own commits are covered through the applicable flow; inspect uncommitted/problems.
  archived    Archive list-state label, not coverage. Reports keep progress and a separate archived
              boolean; rows[].stage does not become archived when the id is archived.
  check --stage may fail even when progress is landed: that target may lack commits a later one has.

problems[].code values and next actions:
  unclassified_merge     A merge's own edits versus synchronization are unresolved; retry, then
                         inspect the merge commit and its parents if it persists.
  patch_pending          Patch-id comparison did not finish; retry and inspect Git read problems.
  git_read               A required Git read failed; inspect repo/detail, repair access and retry.
  fetch_failed           Remote refresh failed; inspect remote/auth/access and retry --fetch.
  no_evidence            Tracked id lacks usable own commits or named worktree evidence;
                         verify the id, naming, workspace and exclusions; --rescan finds new clones.
  final_missing          Final target branch is missing; verify mergeFlow/bases and available refs,
                         then --fetch if a remote should supply it.
  commit_body_unreadable  A commit message could not be read; inspect bodyError and repository access.

error.code values and next actions:
  invalid_input        Bad arguments/options/unknown help topic; follow the usage in message.
  data_directory       Cannot read app data or current directory; inspect directory access.
  workspace            Cannot locate/open one workspace; run sheaf workspaces --json, choose a root
                       with exists = true and retry with --workspace <root>.
  workspace_read       Cannot read saved workspace metadata; repair file access or configuration.
  workspace_busy       A write needs the lock; close the GUI workspace or wait for its writer.
  workspace_lock       Lock acquisition failed; inspect data directory access and retry.
  config_read          Saved configuration is unreadable/invalid; restore it or use config apply.
  config_input         Replacement file is unreadable/invalid; export config show and fix the file.
  invalid_requirement  Id does not match the full configured pattern; inspect config show.
  invalid_stage        Unknown target name; inspect result.mergeFlow from config show.
  query_failed         Query could not be evaluated; inspect message and repository access.
  no_objects           No active tracked ids to check; track an id or supply check <id>.
  not_found            No local evidence for an untracked id; verify id/workspace, --fetch/--rescan.
  invalid_repo         Repository filter is outside this workspace; inspect scope.repositories.
  invalid_cursor       Invalid commits page cursor; restart without --cursor.
  not_tracked          Archive/unarchive requires tracking; track only if the user requests it.
  config_write         Cannot save Sheaf configuration; inspect permissions/disk and retry.

Coverage and skip values:
  covered[].by = ancestor    The target reaches this own commit by ancestry.
  covered[].by = patch       The target has the same git patch-id --stable (e.g. a cherry-pick).
  stages[].skip = baseline_only  Repository checks only the final branch; intermediate step skipped.
  stages[].skip = no_target      No branch exists for this intermediate target; step skipped.
  A missing final target is a final_missing problem, never a no_target skip.

See also: sheaf --help, sheaf help <command>
`

package session

import (
	"sheaf/internal/gitview"
	"sheaf/internal/index"
	"sheaf/internal/query"
)

// Types below are shared by workspace sessions and native views.

// State is the index status shown in the title bar.
type State struct {
	// Phase is what the title bar shows: loading (no index yet, nothing built so far),
	// indexing (first build, progress in Done/Total), updating (a queryable index is being brought up to date), ready.
	Phase    string `json:"phase"`
	Ready    bool   `json:"ready"`    // an index (maybe from the snapshot) can be queried
	Indexing bool   `json:"indexing"` // a build or update is running
	Done     int    `json:"done"`     // repos finished in the running build
	Total    int    `json:"total"`
	Scanned  int    `json:"scanned"` // repos found by the scan: the repo list in Info is complete from then on, before any index exists
	Commits  int    `json:"commits"`
	Updated  int64  `json:"updated"` // unix seconds of the last finished update; 0 = never
	Error    string `json:"error,omitempty"`
}

type Info struct {
	Root  string     `json:"root"`
	Name  string     `json:"name"`
	Repos []RepoInfo `json:"repos"`
	Me    []string   `json:"me"`
	// MeAuto is what the derivation from git config finds now; MeManual are the emails added by hand (without those
	// already derived). Me is both: what "me" matches.
	MeAuto   []MeFrom `json:"meAuto"`
	MeManual []string `json:"meManual"`
	State    State    `json:"state"`
	// Ignored are the repo names excluded from Repos.
	Ignored []string `json:"ignored"`
	// IgnoredWith: an ignored repo -> the repos inside its directory that went with it (not on the list themselves).
	IgnoredWith map[string][]string `json:"ignoredWith"`
	// Scanned are the names of every repo the last scan found, ignored or not (a repo that went with an ignored one
	// is left out: it has no switch of its own).
	Scanned []string `json:"scanned"`
	// Release and Master are the filter page's compare targets (Config); Master "" = each repo's own base.
	Release   string      `json:"release"`
	Master    string      `json:"master"`
	MergeFlow []MergeStep `json:"mergeFlow"`
	// Requirement is the pattern of requirement numbers as saved; "" = DefaultRequirement.
	Requirement string `json:"requirement"`
	// GlabConfig is the workspace's own glab configuration directory as typed (~ allowed); "" = the system's glab login.
	GlabConfig string `json:"glabConfig"`
}

// MeFrom is one derived "me" email and where it comes from.
type MeFrom struct {
	Email  string   `json:"email"`
	Global bool     `json:"global"` // it is the global user.email
	Repos  []string `json:"repos"`  // repos whose own user.email (repo config, includeIf) is it, when not global
}

type RepoInfo struct {
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`     // repo | submodule
	Base         string         `json:"base"`     // base branch: set by the user, else derived (BaseAuto); "" = none
	BaseAuto     bool           `json:"baseAuto"` // Base was derived, not set (autoBase)
	BaselineOnly bool           `json:"baselineOnly"`
	Worktrees    []WorktreeInfo `json:"worktrees"`
}

type WorktreeInfo struct {
	Path   string `json:"path"`
	Branch string `json:"branch"` // short name; "" when detached
	Main   bool   `json:"main"`
	// Requirement is the first tracked number matched in the branch, including archived requirements.
	Requirement string `json:"requirement"`
	// Ahead / Behind count commits against the repo's base branch (0 when no base is set).
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
	// Dirty is the number of files with uncommitted changes; -1 until git status has run.
	Dirty int `json:"dirty"`
}

// BranchView is one entry of the branch tree.
type BranchView struct {
	Name     string `json:"name"`     // short: master, origin/master, v3.18.2
	Kind     string `json:"kind"`     // local | remote | tag
	Worktree string `json:"worktree"` // path of the worktree that has it checked out; "" = none
}

// IndexUpdate tells views to rerun their queries after the index changes.
type IndexUpdate struct {
	ChangedRepos []string `json:"changedRepos"`
}

// WorktreeStatus is pushed when the uncommitted changes of worktrees may have changed.
type WorktreeStatus struct {
	Worktrees []string `json:"worktrees"` // their diff differs from the last status read
	// Read are worktrees whose status was read for the first time: their counts are new, but a diff read earlier
	// came from the same files, so it is not known to be out of date.
	Read []string `json:"read,omitempty"`
}

type FileStatus = gitview.FileStatus

type QueryResult struct {
	ID     int            `json:"id"` // 0 when Error is set: keep showing the previous result
	Total  int            `json:"total"`
	ByRepo map[string]int `json:"byRepo"` // hits per repo ignoring the repo filter
	// Explain is set when operators were used (plain search only).
	Explain *query.Expr        `json:"explain"`
	Error   *query.SyntaxError `json:"error"`
	// Relax is set when Total is 0.
	Relax []query.Suggestion `json:"relax"`
}

type RefView struct {
	Name string `json:"name"` // short: master, origin/master, v3.18.2
	Kind string `json:"kind"` // local | remote | tag
}

// Row is one line of the commit list.
type Row struct {
	Repo    string `json:"repo"`
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	Time    int64  `json:"time"`
	Merge   bool   `json:"merge"`
	// Resolved is a merge commit whose own edits beyond an automatic merge are known (filter page only): conflicts resolved
	// or files changed while merging.
	Resolved bool      `json:"resolved"`
	Refs     []RefView `json:"refs"`
	// Hit is the [start, end) of the search match in Subject, in UTF-16 units.
	Hit []int `json:"hit"`
}

// CommitKey names a commit across the API.
type CommitKey struct {
	Repo string `json:"repo"`
	Hash string `json:"hash"`
}

type CommitDetail struct {
	Repo    string    `json:"repo"`
	Hash    string    `json:"hash"`
	Parents []string  `json:"parents"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Time    int64     `json:"time"`
	Refs    []RefView `json:"refs"`
}

type FileChange = gitview.FileChange

type Patch struct {
	Binary bool   `json:"binary"`
	Hunks  []Hunk `json:"hunks"`
}

type Hunk struct {
	Header string `json:"header"`
	Lines  []Line `json:"lines"`
}

type Line struct {
	Kind string `json:"kind"` // " " "+" "-"
	Old  int    `json:"old"`  // 0 when not on the old side
	New  int    `json:"new"`  // 0 when not on the new side
	Text string `json:"text"`
}

// Option is a branch name and the number of repositories that have it.
type Option struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type TagView struct {
	Name  string           `json:"name"`
	Cond  query.Conditions `json:"cond"`
	Count int              `json:"count"`
}

// FileText is a file's content at one commit; a binary file has no lines.
type FileText struct {
	Lines  []string `json:"lines"`
	Binary bool     `json:"binary"`
	Cut    bool     `json:"cut"` // only the first 2MB (larger files are generated or data; the view says so)
}

// TagAggInput chooses the Tag of the Tag view and what it is compared with. Base "" uses each repo's own base branch
// because these differ between repos; Branches overrides the feature branch of a row (AggRepo.Key -> branch) for this view only, it is not saved with the Tag.
// Cond, when set, replaces the Tag's conditions (refined on the filter page, not saved).
type TagAggInput struct {
	Cond     *query.Conditions `json:"cond,omitempty"`
	Base     string            `json:"base"`
	Branches map[string]string `json:"branches"`
	// Release is excluded when inferring feature branches ("" uses the configured release branch).
	Release   string `json:"release"`
	DiffMode  string `json:"diffMode,omitempty"`
	DiffStage int    `json:"diffStage,omitzero"`
	// Requirement keeps only commits naming this whole requirement number; Cond.Q stays a substring for filters.
	Requirement string `json:"requirement,omitempty"`
	// keep, when set, drops the hits it refuses before anything is counted (the requirement list's whole-number check).
	keep func(r *index.Repo, c int32) bool
	// named, when set, tells which working directory branches belong to the requirement being read: only their
	// uncommitted files are the requirement's. Unset (a filter that is no requirement), every working directory counts.
	named func(branch string) bool
}

// TagAggResult is everything one Tag hits, per repo.
type TagAggResult struct {
	Tag         TagView   `json:"tag"`
	ResultID    int       `json:"resultID"` // the Tag's commits: page them with Rows
	Repos       []AggRepo `json:"repos"`    // most commits first
	Commits     int       `json:"commits"`
	Files       int       `json:"files"`       // committed files, summed over repos
	Uncommitted int       `json:"uncommitted"` // uncommitted files in the repos' worktrees (counts known so far)
}

// Where the feature branch of an AggRepo comes from.
const (
	FromOverride = "override" // chosen in the view (TagAggInput.Branches)
	FromTag      = "tag"      // the Tag's own branch condition
	FromInferred = "inferred" // the branch that holds most of the Tag's commits beyond the base
	FromMerged   = "merged"   // every Tag commit is in the base already: no branch, the diff is per commit
	FromNone     = "none"     // no base to infer against, or no branch holds the commits
)

// AggRepo is one row of the Tag view: a repo with Tag commits, its feature branch against the base, and the worktree that
// has it checked out. A repo holding several of the Tag's branches has a row for each, with the Tag commits on that branch.
type AggRepo struct {
	Key     string `json:"key"` // the row: the repo name, or RowKey(repo, branch) when the repo has several rows
	Repo    string `json:"repo"`
	Commits int    `json:"commits"`
	Files   int    `json:"files"` // distinct paths the Tag's commits changed
	Base    string `json:"base"`  // the base branch used; "" when none is set
	Branch  string `json:"branch"`
	// MergedBranch names the feature branch of a FromMerged row, which has no Branch to compare; "" when it cannot be told.
	MergedBranch string `json:"mergedBranch,omitempty"`
	BranchFrom   string `json:"branchFrom"` // override | tag | inferred | merged | none
	// Skip says why there is no comparison: base_unset, no_base, or no_head (the chosen branch is not in the repo).
	Skip   string `json:"skip,omitempty"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
	// Worktree has Branch checked out; "" when none, or when there is no comparison.
	Worktree    string `json:"worktree"`
	Uncommitted int    `json:"uncommitted"` // files with uncommitted changes in Worktree; -1 until git status has run
	// Auto is the branch the row would have without an override (the Tag's, or inferred); "" when none.
	Auto string `json:"auto"`
	// Main is the repo's own base branch, whatever the compare target (Base) is.
	Main string `json:"main"`
	// Stage is how far the row has got: Stage* constants.
	Stage string `json:"stage"`
}

// AggCommits is a page source (Rows) of the filter page's commits tab.
type AggCommits struct {
	ResultID int `json:"resultID"`
	Total    int `json:"total"`
	Merges   int `json:"merges"` // merge commits left out
}

// AggBranch is a candidate branch of one row of the filter page: a local or remote branch of the repo holding some of the
// row's Tag commits.
type AggBranch struct {
	Name     string `json:"name"`     // short: feat-x, origin/feat-x
	Commits  int    `json:"commits"`  // the row's Tag commits it holds (beyond the repo's base when it has one)
	Worktree string `json:"worktree"` // checked out there; "" = none
}

// AggFile is one file of the Tag view: changed by the Tag's commits, or uncommitted in the repo's worktree, or both.
type AggFile struct {
	Key         string `json:"key"` // the row (AggRepo.Key) whose commits or worktree changed it
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	OldPath     string `json:"oldPath,omitempty"`
	Status      string `json:"status"`  // A M D R C T, over all the Tag's commits (then the worktree)
	Commits     int    `json:"commits"` // Tag commits that changed it; 0 = only uncommitted
	Uncommitted bool   `json:"uncommitted"`
	Worktree    bool   `json:"worktree,omitempty"` // HEAD to the working directory, separate from committed changes
	Untracked   bool   `json:"untracked"`          // new in the worktree, unknown to git: its diff is the whole file
}

// AggPatch is the merged diff of one file. Mode range: one part, merge base to the branch plus the worktree's changes.
// Mode commits (the repo has no branch diff: merged into the base, or no base): one part per Tag commit, oldest first.
type AggPatch struct {
	Mode  string    `json:"mode"`
	Parts []AggPart `json:"parts"`
	More  int       `json:"more"` // commits mode: Tag commits that changed the file beyond the parts shown
}

type AggPart struct {
	Hash    string `json:"hash"` // "" in range mode
	Subject string `json:"subject"`
	Patch   Patch  `json:"patch"`
}

// DiffSource names the diff of one file anywhere in the app, so a diff pane can open it in a window of its own and annotate
// either side. Kind: "commit" (Repo, Hash: against the first parent), "wc" (Worktree, Area: an uncommitted change),
// or "agg" (Repo, Row, Agg, Untracked: the Tag view's merged diff).
// With Agg, the window steps through the files of one row (Row); All widens that to every row, Dir to the files under a
// directory, Only to the uncommitted ones (their kind is then "wc": the worktree against HEAD, as the Tag view shows them).
type DiffSource struct {
	Kind      string       `json:"kind"`
	Repo      string       `json:"repo"`
	Path      string       `json:"path"`
	OldPath   string       `json:"oldPath,omitempty"`
	Hash      string       `json:"hash,omitempty"`
	Worktree  string       `json:"worktree,omitempty"`
	Area      string       `json:"area,omitempty"`
	Agg       *TagAggInput `json:"agg,omitempty"`
	Row       string       `json:"row,omitempty"` // agg: the row (AggRepo.Key)
	Untracked bool         `json:"untracked,omitempty"`
	All       bool         `json:"all,omitempty"`  // agg: the list spans every row
	Dir       string       `json:"dir,omitempty"`  // agg: only files under this directory
	Only      bool         `json:"only,omitempty"` // agg: only uncommitted files
}

// DirtyDir is one working directory with uncommitted changes.
type DirtyDir struct {
	Requirement string `json:"requirement"`
	Repo        string `json:"repo"`
	Path        string `json:"path"` // also the row key of its files
	Branch      string `json:"branch"`
	Main        bool   `json:"main"`       // the repo's own directory, not a linked worktree
	OnMain      bool   `json:"onMain"`     // the branch is the repo's base branch: changes made straight on it
	Dirty       int    `json:"dirty"`      // files with uncommitted changes
	LastChange  int64  `json:"lastChange"` // unix time of the newest changed file; 0 when unknown (only deletions)
	Ahead       int    `json:"ahead"`
	Behind      int    `json:"behind"`
	Merging     bool   `json:"merging"`   // in the middle of a merge started outside the app
	Conflicts   int    `json:"conflicts"` // its unmerged files
}

// DirtyGroup is the directories of one repo.
type DirtyGroup struct {
	Name       string     `json:"name"`
	Dirs       []DirtyDir `json:"dirs"`
	Files      int        `json:"files"`
	LastChange int64      `json:"lastChange"`
}

// DirtyView is the uncommitted changes page: repo groups and their directories, most recently changed first.
type DirtyView struct {
	Groups []DirtyGroup `json:"groups"`
	Files  []AggFile    `json:"files"` // Key = DirtyDir.Path
}

// CmdPulse is pushed (at most 4 times a second) when commands start or end.
type CmdPulse struct {
	Running int `json:"running"`
	Last    int `json:"last"` // newest event id
}

// MRInput is the ticked rows of the filter page to open merge or pull requests for.
type MRInput struct {
	Agg     TagAggInput `json:"agg"`
	Rows    []string    `json:"rows"`    // AggRepo.Key
	Subject string      `json:"subject"` // what the task list calls the run: the requirement
}

// MRPlanRow is what one row would do, shown before anything runs.
type MRPlanRow struct {
	Tool     string   `json:"tool"`
	Missing  bool     `json:"missing"`
	Key      string   `json:"key"`
	Repo     string   `json:"repo"`
	Branch   string   `json:"branch"` // the source branch, its name on origin
	Target   string   `json:"target"` // the branch the 对主分支 column compares with, its name on origin
	Skip     string   `json:"skip"`   // why not: no_branch, no_target, is_target, no_remote; "" = it opens one
	Push     bool     `json:"push"`   // origin lacks some of the branch: push it first
	Commands []string `json:"commands"`
}

type MRPlan struct {
	Rows []MRPlanRow `json:"rows"`
	Open int         `json:"open"` // rows that will open (or find) one
}

// MRRow is what happened to one row.
type MRRow struct {
	Tool    string `json:"tool"`
	Missing bool   `json:"missing"`
	Key     string `json:"key"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Target  string `json:"target"`
	Outcome string `json:"outcome"` // created | exists | skipped | failed
	Skip    string `json:"skip"`
	Pushed  bool   `json:"pushed"`
	URL     string `json:"url"`  // the merge request, created or found
	Auth    bool   `json:"auth"` // failed because the row's CLI is not logged in
	Error   string `json:"error"`
}

// MRRun is one 创建 MR; the page keeps it (nothing to follow up on the server side).
type MRRun struct {
	At       int64   `json:"at"`
	Rows     []MRRow `json:"rows"`
	Created  int     `json:"created"`
	Existing int     `json:"existing"`
	Skipped  int     `json:"skipped"`
	Failed   int     `json:"failed"`
}

// FetchRun is what 获取全部 did: git fetch origin in each tracked repo.
type FetchRun struct {
	Rows     []FetchRow `json:"rows"`
	Fetched  int        `json:"fetched"`
	NoRemote int        `json:"noRemote"` // repos without origin: nothing to fetch
	Failed   int        `json:"failed"`
	Stopped  int        `json:"stopped"` // repos the user stopped before their fetch began
}

type FetchRow struct {
	Repo    string `json:"repo"`
	Outcome string `json:"outcome"` // fetched | no_remote | failed
	Error   string `json:"error"`
}

// BatchPlanInput says what a batch sheet looks at.
type BatchPlanInput struct {
	Repos    []string `json:"repos"`    // repo names; empty = every tracked repo
	Branches []string `json:"branches"` // 更新本地分支: the starred branches (Favs); origin/x counts as x, tags and unknown names drop out
}

// BatchPlanRow is what syncing one branch of a repo would do.
type BatchPlanRow struct {
	Repo     string   `json:"repo"`
	Branch   string   `json:"branch"`
	Remote   string   `json:"remote"`   // the remote branch it follows (origin/master); "" = none
	How      string   `json:"how"`      // ref | worktree | skip
	Skip     string   `json:"skip"`     // why not (how = skip): no_local, no_remote, merging, dirty, unpushed
	Worktree string   `json:"worktree"` // worktree that has the branch checked out; "" = none
	Dirty    int      `json:"dirty"`    // uncommitted files of the directory that is checked
	Ahead    int      `json:"ahead"`    // commits the local branch has that its remote branch lacks, before any fetch
	Behind   int      `json:"behind"`   // commits the remote branch has that the local branch lacks, before any fetch
	Commands []string `json:"commands"`
}

type BatchPlan struct {
	Rows     []BatchPlanRow `json:"rows"`
	Branches []string       `json:"branches"` // the branches that have a row, in the order given
	Run      int            `json:"run"`      // rows that will do something
}

// BatchPick is one branch of a repo selected for sync.
type BatchPick struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

type BatchRunInput struct {
	Picks   []BatchPick `json:"picks"`
	Subject string      `json:"subject"` // what the task list calls the run: the requirement
}

// BatchRow is what happened to one row of a batch action.
type BatchRow struct {
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	How      string `json:"how"`
	Outcome  string `json:"outcome"` // updated | up_to_date | skipped | failed
	Skip     string `json:"skip"`
	Commits  int    `json:"commits"` // fast-forwarded over
	Worktree string `json:"worktree"`
	Error    string `json:"error"`
}

type BatchRun struct {
	Rows     []BatchRow `json:"rows"`
	Updated  int        `json:"updated"`
	UpToDate int        `json:"upToDate"`
	Skipped  int        `json:"skipped"`
	Failed   int        `json:"failed"`
}

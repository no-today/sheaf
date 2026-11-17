package session

import "sheaf/internal/gitview"

// The results the command line reads: each one is complete once Settle has returned, and says what it could not tell
// instead of guessing.

// CompletionSnapshot contains only persisted metadata needed by shell completion.
type CompletionSnapshot struct {
	Requirements []CompletionRequirement
	Stages       []string
	Repositories []string
}

type CompletionRequirement struct {
	ID       string
	Title    string
	Archived bool
}

// KnownWorkspace describes a recorded workspace without opening a session.
type KnownWorkspace struct {
	Root       string `json:"root"`
	Name       string `json:"name"`
	ID         string `json:"id"`
	Exists     bool   `json:"exists"`
	Current    bool   `json:"current"`
	Tracked    int    `json:"tracked"`
	Archived   int    `json:"archived"`
	ConfigFile string `json:"configFile"`
}

// Problem is something a result could not decide, by a stable Code a script can test.
type Problem struct {
	Code   string `json:"code"` // Problem* constants
	Repo   string `json:"repo,omitempty"`
	Stage  string `json:"stage,omitempty"` // a merge flow stage name
	Detail string `json:"detail,omitempty"`
}

const (
	ProblemUnclassifiedMerge = "unclassified_merge" // a merge commit of the requirement is not yet known to be its own or a sync
	ProblemPatchPending      = "patch_pending"      // a patch-id comparison did not finish
	ProblemGitRead           = "git_read"           // git failed to read something the answer needs
	ProblemFetchFailed       = "fetch_failed"       // refreshing the remote of a repo failed
	ProblemNoEvidence        = "no_evidence"        // tracked, but no commit or named working directory locates it
	ProblemFinalMissing      = "final_missing"      // the repo has no final branch of the flow
)

// Scope picks the tracked requirements Overview lists.
type Scope string

const (
	ScopeActive   Scope = "active"   // not archived
	ScopeArchived Scope = "archived" // archived only
	ScopeAll      Scope = "all"
)

// ReqSummary is one requirement of Overview, and the head of its ReqReport.
type ReqSummary struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Tracked  bool      `json:"tracked"`
	Archived bool      `json:"archived"`
	Progress string    `json:"progress"` // Stage* of the least advanced repo, whatever the archive state
	Repos    int       `json:"repos"`
	Latest   int64     `json:"latest"` // unix seconds of its newest commit or branch tip
	Gaps     []RepoGap `json:"gaps"`   // repos not through the flow
	// Uncommitted counts files with uncommitted changes in working directories that belong to the requirement.
	Uncommitted int `json:"uncommitted"`
}

// RepoGap is a repo of a requirement that has not been through the merge flow.
type RepoGap struct {
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Next        string `json:"next,omitempty"`    // the name of the stage it waits for; "" when Unknown says why not
	Missing     int    `json:"missing"`           // own commits the next stage's target does not cover
	Uncommitted int    `json:"uncommitted"`       // files
	Unknown     string `json:"unknown,omitempty"` // a Problem code when the repo cannot be judged
}

// Overview is the tracked requirements of a Scope.
type Overview struct {
	ObservedAt int64        `json:"observedAt"` // unix seconds the local state was read; not the last fetch
	Excluded   []string     `json:"excluded"`   // repos the workspace ignores
	Items      []ReqSummary `json:"items"`
	Problems   []Problem    `json:"problems"`
}

// ReqReport is everything known about one requirement number, tracked or not.
type ReqReport struct {
	ReqSummary
	Found      bool        `json:"found"` // some commit or named working directory locates it
	Flow       []MergeStep `json:"flow"`
	Rows       []RowReport `json:"rows"`
	ObservedAt int64       `json:"observedAt"`
	Problems   []Problem   `json:"problems"`
}

// UncommittedFile is one cached status entry relative to the worktree.
type UncommittedFile = gitview.UncommittedFile

// RowReport is one repo (or one branch of a repo) of a requirement.
type RowReport struct {
	Repo             string             `json:"repo"`
	Path             string             `json:"path"`
	Branch           string             `json:"branch,omitempty"`
	Worktree         string             `json:"worktree,omitempty"`
	Deleted          bool               `json:"deleted"` // no branch named after the requirement is left
	Commits          int                `json:"commits"`
	Uncommitted      int                `json:"uncommitted"`
	UncommittedFiles *[]UncommittedFile `json:"uncommittedFiles,omitempty"`
	Stage            string             `json:"stage"`
	Stages           []StageCheck       `json:"stages"`
}

// StageCheck is how one stage's target covers a row's own commits.
type StageCheck struct {
	Name    string      `json:"name"`
	Applies bool        `json:"applies"`
	Skip    string      `json:"skip,omitempty"` // Skip* constants when it does not apply
	Ref     string      `json:"ref,omitempty"`  // the ref actually read, e.g. refs/remotes/origin/master
	SHA     string      `json:"sha,omitempty"`  // its full hash
	Remote  bool        `json:"remote"`         // a remote-tracking ref, not a local fallback
	Covered []Cover     `json:"covered"`
	Missing []CommitRef `json:"missing"`
}

const (
	SkipBaselineOnly = "baseline_only" // the repo is set to check only the final branch
	SkipNoTarget     = "no_target"     // the repo has no branch for this intermediate stage
)

// Cover is an own commit a stage's target has, and how.
type Cover struct {
	CommitRef
	By string `json:"by"` // CoverAncestor | CoverPatch
}

const (
	CoverAncestor = "ancestor"
	CoverPatch    = "patch" // a commit with the same git patch-id --stable
)

type CommitRef struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// CommitPage is a page of a requirement's commits; Next is "" on the last page.
type CommitPage struct {
	Items []CommitItem `json:"items"`
	Next  string       `json:"next"`
}

type CommitItem struct {
	Repo    string   `json:"repo"`
	Path    string   `json:"path"`
	SHA     string   `json:"sha"`
	Parents []string `json:"parents"`
	Author  string   `json:"author"`
	Email   string   `json:"email"`
	Time    int64    `json:"time"`
	Subject string   `json:"subject"`
	Body    *string  `json:"body"` // nil when it could not be read: BodyErr says why
	BodyErr string   `json:"bodyError,omitempty"`
}

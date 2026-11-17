package track

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"sheaf/internal/native/i18n"
	"sheaf/internal/session"
)

// The rules of the page that need no window: what is shown, what is ticked, the words of cells and sheets.

// The built-in entries of the left pane.
const (
	wcID  = "wc"  // 未提交
	tmpID = "tmp" // the current requirement or branch
)

// farBehind is how far behind a row's ↓ turns red.
const farBehind = 50

// fill puts values into the {name} marks of a text: fill(w.Meta, "repos", 3, "rows", 4).
func fill(s string, kv ...any) string {
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i] == "n" {
			if n, ok := kv[i+1].(int); ok {
				s = i18n.Plural(s, n)
			}
			break
		}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		s = strings.ReplaceAll(s, "{"+kv[i].(string)+"}", fmt.Sprint(kv[i+1]))
	}
	return s
}

// pickEntry keeps an explicit requirement or branch, otherwise falls back to uncommitted changes.
func pickEntry(want string, temp bool, dirty int) string {
	if want == wcID || (want == tmpID && temp) {
		return want
	}
	if dirty > 0 {
		return wcID
	}
	return ""
}

// withBranch is branches with row's branch set (or, for "", dropped: back to the automatic one); branches is not changed.
func withBranch(branches map[string]string, row, branch string) map[string]string {
	out := make(map[string]string, len(branches)+1)
	for k, v := range branches {
		out[k] = v
	}
	if branch != "" {
		out[row] = branch
	} else {
		delete(out, row)
	}
	return out
}

// rowLabel names a row: the repo, plus the branch when the repo has several rows (session.RowKey).
func rowLabel(key, repo string) string {
	if key == repo || len(key) <= len(repo) {
		return repo
	}
	return repo + " · " + key[len(repo)+1:]
}

// defaultChecked is what batch actions act on until the user changes it: every row but those already merged into the base.
func defaultChecked(repos []session.AggRepo) map[string]bool {
	out := map[string]bool{}
	for _, r := range repos {
		if r.BranchFrom != session.FromMerged {
			out[r.Key] = true
		}
	}
	return out
}

func checkedRepos(rows []session.AggRepo, checked map[string]bool) []string {
	var repos []string
	seen := make(map[string]bool)
	for _, r := range rows {
		if checked[r.Key] && !seen[r.Repo] {
			repos = append(repos, r.Repo)
			seen[r.Repo] = true
		}
	}
	return repos
}

func branchShown(w *words, r session.AggRepo) string {
	if r.BranchFrom == session.FromMerged {
		if r.MergedBranch != "" {
			return r.MergedBranch
		}
		if r.Stage != session.StageLanded {
			return w.NoBranch
		}
		return fill(w.Merged, "base", r.Base)
	}
	if r.Branch == "" {
		return w.NoBranch
	}
	return r.Branch
}

// abKind is what the 对主分支 cell shows.
type abKind int

const (
	abNone  abKind = iota // the repo lacks the compare branch
	abUnset               // no base is set
	abDash                // no branch, or merged: nothing to compare
	abCount               // ↑ahead ↓behind
)

type abResult struct {
	Kind     abKind
	Up, Down int
}

func abCell(r session.AggRepo) abResult {
	switch {
	case r.Skip == "no_base":
		return abResult{Kind: abNone}
	case r.Skip == "base_unset":
		return abResult{Kind: abUnset}
	case r.Skip != "" || r.Branch == "" || r.BranchFrom == session.FromMerged:
		return abResult{Kind: abDash}
	}
	return abResult{Kind: abCount, Up: r.Ahead, Down: r.Behind}
}

// keepOrder puts rows in the order of keys (the order before a batch action, kept until the page is entered again); rows not in
// it go last in their own order. nil keys keep the order of rows.
func keepOrder(rows []session.AggRepo, keys []string) []session.AggRepo {
	if keys == nil {
		return rows
	}
	at := func(i int) int {
		if k := slices.Index(keys, rows[i].Key); k >= 0 {
			return k
		}
		return len(keys) + i
	}
	idx := make([]int, len(rows))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return at(a) - at(b) })
	out := make([]session.AggRepo, len(rows))
	for i, k := range idx {
		out[i] = rows[k]
	}
	return out
}

// usedBy is the 1-based row of the same repo that already shows branch (it cannot be picked again), or 0.
func usedBy(repos []session.AggRepo, row session.AggRepo, branch string) int {
	for i, x := range repos {
		if x.Key != row.Key && x.Repo == row.Repo && x.Branch == branch {
			return i + 1
		}
	}
	return 0
}

// targetGroups splits the compare-target dropdown: favourites first in the user's order (also one no repo has any more),
// then the rest as listed; typing filters both.
func targetGroups(options []session.Option, favs []string, q string) (fav, rest []session.Option) {
	needle := strings.ToLower(strings.TrimSpace(q))
	hit := func(v string) bool { return needle == "" || strings.Contains(strings.ToLower(v), needle) }
	count := map[string]int{}
	for _, o := range options {
		count[o.Value] = o.Count
	}
	for _, f := range favs {
		if hit(f) {
			fav = append(fav, session.Option{Value: f, Count: count[f]})
		}
	}
	for _, o := range options {
		if !slices.Contains(favs, o.Value) && hit(o.Value) {
			rest = append(rest, o)
		}
	}
	return fav, rest
}

// toggleFav adds branch to the favourites, or takes it out.
func toggleFav(favs []string, branch string) []string {
	if i := slices.Index(favs, branch); i >= 0 {
		return slices.Delete(slices.Clone(favs), i, i+1)
	}
	return append(slices.Clone(favs), branch)
}

// Why a git command failed, in words that fit a result cell: the error text of a batch or MR row is "git fetch …: exit
// status 128: fatal: …", with git's own stderr lines after the last colon.

const maxReason = 110

var (
	failPrefix = regexp.MustCompile(`(?s)^.*?exit status \d+:\s*`)
	failLead   = regexp.MustCompile(`\b(?:fatal|error):\s`)
	authFail   = regexp.MustCompile(`(?i)could not read (?:Username|Password)|Authentication failed|terminal prompts disabled|\b401\b`)
	netFail    = regexp.MustCompile(`(?i)Could not resolve host|timed out|Connection (?:refused|reset)|Failed to connect|Network is unreachable`)
)

// failReason is the last `fatal:` / `error:` line (else the last line) without the command prefix, cut to maxReason characters.
func failReason(err string) string {
	var lines []string
	for _, l := range strings.Split(failPrefix.ReplaceAllString(err, ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	line := lines[len(lines)-1]
	for i := len(lines) - 1; i >= 0; i-- {
		if m := failLead.FindStringIndex(lines[i]); m != nil {
			line = lines[i][m[0]:]
			break
		}
	}
	if r := []rune(line); len(r) > maxReason {
		line = string(r[:maxReason-1]) + "…"
	}
	return line
}

// failHint is what to try, for the failures that have a usual cause; "" = nothing to add.
func failHint(w *words, err string) string {
	switch {
	case authFail.MatchString(err):
		return w.FailAuth
	case netFail.MatchString(err):
		return w.FailNetwork
	}
	return ""
}

// topReasonText names the most common reason among failed rows, with how many share it; "" when there is none.
func topReasonText(w *words, errs []string) string {
	count := map[string]int{}
	var order []string
	for _, e := range errs {
		if r := failReason(e); r != "" {
			if count[r] == 0 {
				order = append(order, r)
			}
			count[r]++
		}
	}
	best := ""
	for _, r := range order {
		if best == "" || count[r] > count[best] {
			best = r
		}
	}
	if best == "" {
		return ""
	}
	return fill(w.FailTop, "reason", best, "n", count[best])
}

type tone int

const (
	toneOK tone = iota
	toneSkip
	toneBad
)

// resultCell is a row's result column after a run.
type resultCell struct {
	Tone   tone
	Text   string
	Title  string // the hover text: the whole story (stderr)
	URL    string // the click opens the merge request
	Reason string // shown under the text: why it failed
}

func failedCell(w *words, text, err string, auth bool) resultCell {
	hint := failHint(w, err)
	if auth {
		hint = w.MrAuthHint
	}
	title := err
	if hint != "" {
		title = hint + "\n\n" + err
	}
	c := resultCell{Tone: toneBad, Text: text, Title: title}
	if !auth {
		c.Reason = failReason(err)
	}
	return c
}

func mrShort(w *words, skip string) string {
	switch skip {
	case "no_branch":
		return w.MrShortNoBranch
	case "no_target":
		return w.MrShortNoTarget
	case "is_target":
		return w.MrShortIsTarget
	case "no_remote":
		return w.MrShortNoRemote
	case "no_project":
		return w.MrShortNoProject
	case "diverged":
		return w.MrShortDiverged
	}
	return skip
}

// mrRef labels the request using its host's convention.

func mrRef(url string) string { return session.MRRef(url) }

func mrCell(w *words, r session.MRRow) resultCell {
	switch r.Outcome {
	case "created":
		return resultCell{Tone: toneOK, Text: fill(w.MrResCreated, "mr", mrRef(r.URL)), Title: r.URL, URL: r.URL}
	case "exists":
		return resultCell{Tone: toneSkip, Text: fill(w.MrResExists, "mr", mrRef(r.URL)), Title: r.URL, URL: r.URL}
	case "skipped":
		return resultCell{Tone: toneSkip, Text: fill(w.ResSkipped, "why", mrShort(w, r.Skip))}
	}
	text := w.ResFailed
	if r.Auth {
		text = w.MrResAuth
	}
	if r.Auth && r.Tool == "gh" {
		return resultCell{Tone: toneBad, Text: w.MrGhResAuth, Title: w.MrGhAuthHint + "\n\n" + r.Error}
	}
	if r.Missing {
		hint := w.MrGlabMissing
		if r.Tool == "gh" {
			hint = w.MrGhMissing
		}
		return resultCell{Tone: toneBad, Text: w.ResFailed, Title: hint + "\n\n" + r.Error}
	}
	return failedCell(w, text, r.Error, r.Auth)
}

// mrSummary: 2 个 MR 已创建 · 1 个已存在 · 最常见原因….
func mrSummary(w *words, run session.MRRun) string {
	created, existing := w.MrSumCreated, w.MrSumExisting
	for _, r := range run.Rows {
		if r.Tool == "gh" {
			created = strings.ReplaceAll(created, "MR", "MR/PR")
			existing = strings.ReplaceAll(existing, "MR", "MR/PR")
			break
		}
	}
	s := joinCounts([]int{run.Created, run.Existing, run.Failed, run.Skipped}, []string{created, existing, w.SumFailed, w.SumSkipped})
	if run.Failed > 0 {
		var errs []string
		for _, r := range run.Rows {
			if r.Outcome == "failed" {
				reason := r.Error
				if r.Auth {
					reason = w.MrResAuth
					if r.Tool == "gh" {
						reason = w.MrGhResAuth
					}
				}
				errs = append(errs, reason)
			}
		}
		if top := topReasonText(w, errs); top != "" {
			if s != "" {
				s += " · "
			}
			s += top
		}
	}
	return s
}

func syncSummary(w *words, run session.BatchRun) string {
	return joinCounts(
		[]int{run.Updated, run.UpToDate, run.Failed, run.Skipped},
		[]string{w.SyncSumUpdated, w.SumUpToDate, w.SumFailed, w.SumSkipped})
}

func joinCounts(ns []int, forms []string) string {
	var parts []string
	for i, n := range ns {
		if n > 0 {
			parts = append(parts, fill(forms[i], "n", n))
		}
	}
	return strings.Join(parts, " · ")
}

// mrBrief is mrHow in a few words, for the right of the row; the route shows the branches.
func mrBrief(w *words, r session.MRPlanRow) string {
	kind := "MR"
	if r.Tool == "gh" {
		kind = "PR"
	}
	switch {
	case r.Missing && r.Skip == "":
		return fill(w.MrBriefMissing, "tool", cmpOr(r.Tool, "glab"))
	case r.Skip == "" && r.Push:
		return fill(w.MrBriefPush, "kind", kind)
	case r.Skip == "":
		return fill(w.MrBriefOpen, "kind", kind)
	}
	return mrShort(w, r.Skip)
}

// mrRoute is where a merge request goes: "feat → main", or the branch alone when there is no target.
func mrRoute(r session.MRPlanRow) string {
	if r.Target == "" || r.Skip == "is_target" {
		return r.Branch
	}
	return r.Branch + " → " + r.Target
}

// syncBrief is syncHow in a few words, with how far behind its remote branch the branch is.
func syncBrief(w *words, r session.BatchPlanRow) string {
	wt := path.Base(r.Worktree)
	switch r.How {
	case "ref", "worktree":
		text := w.SyncBriefRef
		if r.How == "worktree" {
			text = fill(w.SyncBriefWorktree, "wt", wt)
		}
		if r.Behind > 0 {
			text += " ↓" + itoa(r.Behind)
		}
		return text
	}
	switch r.Skip {
	case "no_local":
		return w.SyncBriefNoLocal
	case "no_remote":
		return w.SyncBriefNoRemote
	case "merging":
		return fill(w.SyncBriefMerging, "wt", wt)
	case "dirty":
		return fill(w.SyncBriefDirty, "wt", wt)
	case "unpushed":
		return w.SyncBriefUnpushed
	}
	return r.Skip
}

// syncRoute is the branch and the remote it follows: "release ← origin" when they share the name.
func syncRoute(r session.BatchPlanRow) string {
	if r.Remote == "" {
		return r.Branch
	}
	if remote, ok := strings.CutSuffix(r.Remote, "/"+r.Branch); ok {
		return r.Branch + " ← " + remote
	}
	return r.Branch + " ← " + r.Remote
}

func cmpOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// mrHow is what a row of the MR sheet says: push first or not, or why it gets no merge request.
func mrHow(w *words, r session.MRPlanRow) (text string) {
	defer func() {
		if r.Tool == "gh" {
			text = strings.NewReplacer("an MR", "a PR", "MR", "PR").Replace(text)
		}
	}()
	if r.Missing && r.Skip == "" {
		if r.Tool == "gh" {
			return w.MrGhMissing
		}
		return w.MrGlabMissing
	}
	switch r.Skip {
	case "":
		if r.Push {
			return fill(w.MrHowPush, "b", r.Branch, "t", r.Target)
		}
		return fill(w.MrHowOpen, "b", r.Branch, "t", r.Target)
	case "no_branch":
		return w.MrSkipNoBranch
	case "no_target":
		return w.MrSkipNoTarget
	case "is_target":
		return fill(w.MrSkipIsTarget, "b", r.Branch)
	case "no_remote":
		return w.MrSkipNoRemote
	case "no_project":
		return w.MrSkipNoProject
	case "diverged":
		return w.MrSkipDiverged
	}
	return r.Skip
}

func syncHow(w *words, r session.BatchPlanRow) string {
	wt := path.Base(r.Worktree)
	switch r.How {
	case "ref":
		return fill(w.SyncHowRef, "b", r.Branch, "remote", r.Remote)
	case "worktree":
		return fill(w.SyncHowWorktree, "wt", wt, "b", r.Branch, "remote", r.Remote)
	case "skip":
		switch r.Skip {
		case "no_local":
			return fill(w.SyncSkipNoLocal, "b", r.Branch)
		case "no_remote":
			return fill(w.SyncSkipNoRemote, "b", r.Branch)
		case "merging":
			return fill(w.SyncSkipMerging, "wt", wt)
		case "dirty":
			return fill(w.SyncSkipDirty, "wt", wt, "n", r.Dirty)
		case "unpushed":
			return w.SyncSkipUnpushed
		}
		return r.Skip
	}
	return ""
}

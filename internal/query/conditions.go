package query

import (
	"encoding/json"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Conditions is one combination of filters: what the filter bar shows and
// what a Tag saves. Paths and Branches are applied by the engine (they need the
// index); the rest is matched here.
type Conditions struct {
	Q     string `json:"q"`
	Regex bool   `json:"regex"`
	// Author filter: Me and/or specific other authors (lowercased emails).
	// Me false and no Authors means everyone.
	Me        bool     `json:"me"`
	Authors   []string `json:"authors"`
	AuthorNot bool     `json:"authorNot"` // everyone except the chosen authors
	// Branches: each branch's own commits (from the repo's base), combined; none = all.
	Branches  []string `json:"branches"`
	BranchNot bool     `json:"branchNot"` // commits on none of Branches
	// Date: Hours back from now (24, 168), else the range Since..Until (unix seconds, either end open when 0).
	Hours int   `json:"hours"`
	Since int64 `json:"since"`
	Until int64 `json:"until"`
	// Paths: commits that hit any of them; none = all. Each is "repo", "repo/dir" (a path prefix inside that
	// repo, see SplitPath) or text contained in a changed file's path.
	Paths []string `json:"paths"`
}

// UnmarshalJSON also reads what Tags saved before: "branch" (one branch), "days" (Hours = days×24), and
// "repo" / "path" (now Paths; both set becomes "repo/path", which reads path as a prefix instead of a substring).
func (c *Conditions) UnmarshalJSON(b []byte) error {
	type plain Conditions
	var x struct {
		plain
		Branch string `json:"branch"`
		Days   int    `json:"days"`
		Repo   string `json:"repo"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	*c = Conditions(x.plain)
	if x.Branch != "" && len(c.Branches) == 0 {
		c.Branches = []string{x.Branch}
	}
	if x.Days > 0 && c.Hours == 0 && c.Since == 0 && c.Until == 0 {
		c.Hours = x.Days * 24
	}
	if len(c.Paths) == 0 {
		repo, path := strings.TrimSpace(x.Repo), strings.Trim(strings.TrimSpace(x.Path), "/")
		switch {
		case repo != "" && path != "":
			c.Paths = []string{repo + "/" + path}
		case repo != "":
			c.Paths = []string{repo}
		case path != "":
			c.Paths = []string{strings.TrimSpace(x.Path)}
		}
	}
	return nil
}

// Default is the filter bar on first open: no conditions.
func Default() Conditions {
	return Conditions{Authors: []string{}, Branches: []string{}, Paths: []string{}}
}

// HasDate is false when every date passes.
func (c Conditions) HasDate() bool { return c.Hours > 0 || c.Since > 0 || c.Until > 0 }

// CleanPaths are the non-blank Paths, trimmed.
func (c Conditions) CleanPaths() []string {
	out := []string{}
	for _, p := range c.Paths {
		if p = strings.TrimSpace(p); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// SplitPath reads one Paths entry: repo is the longest repo name it starts with (as a whole path segment), dir the
// rest without slashes at the ends. repo "" means the entry is text to find in a changed file's path.
func SplitPath(entry string, isRepo func(string) bool) (repo, dir string) {
	e := strings.TrimSpace(entry)
	for end := len(e); end > 0; end = strings.LastIndexByte(e[:end], '/') {
		if name := strings.TrimRight(e[:end], "/"); name != "" && isRepo(name) {
			return name, strings.Trim(e[end:], "/")
		}
	}
	return "", e
}

// SoloRepo is the repo when Paths is exactly one whole repo: what the repo dropdown shows, and a scope that draws
// branch lines.
func (c Conditions) SoloRepo(isRepo func(string) bool) string {
	if ps := c.CleanPaths(); len(ps) == 1 {
		if repo, dir := SplitPath(ps[0], isRepo); dir == "" {
			return repo
		}
	}
	return ""
}

// AllAuthors is true when no author filter applies.
func (c Conditions) AllAuthors() bool { return !c.Me && len(c.Authors) == 0 }

// Same reports whether two condition sets are the same filter, used for
// "已存为 xxx" and duplicate-Tag warnings.
func Same(a, b Conditions) bool {
	return a.Q == b.Q && a.Regex == b.Regex && a.Me == b.Me && sameSet(a.Authors, b.Authors) && a.AuthorNot == b.AuthorNot &&
		sameSet(a.Branches, b.Branches) && a.BranchNot == b.BranchNot && sameDate(a, b) && sameSet(a.CleanPaths(), b.CleanPaths())
}

// sameDate: a relative date ignores the range (Compile does too).
func sameDate(a, b Conditions) bool {
	if a.Hours > 0 || b.Hours > 0 {
		return a.Hours == b.Hours
	}
	return a.Since == b.Since && a.Until == b.Until
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// Env is what matching needs from outside.
type Env struct {
	Me  []string // lowercased emails of "me" for this workspace
	Now time.Time
}

// Matcher tests commits against the parts of Conditions that need no index.
type Matcher struct {
	C         Conditions
	expr      Expr // lowercased
	re        *regexp.Regexp
	authors   map[string]bool
	authorNot bool
	since     int64
	until     int64
}

// Compile prepares c for matching. A regex error is returned as *SyntaxError;
// the caller keeps showing the last good result.
func Compile(c Conditions, env Env) (*Matcher, *SyntaxError) {
	m := &Matcher{C: c}
	q := strings.TrimSpace(c.Q)
	if c.Regex && q != "" {
		re, err := CompileRegex(q)
		if err != nil {
			return nil, err
		}
		m.re = re
	} else {
		m.expr = Parse(q).lower()
	}
	if !c.AllAuthors() {
		m.authors, m.authorNot = map[string]bool{}, c.AuthorNot
		if c.Me {
			for _, e := range env.Me {
				m.authors[strings.ToLower(e)] = true
			}
		}
		for _, e := range c.Authors {
			m.authors[strings.ToLower(e)] = true
		}
	}
	if c.Hours > 0 {
		m.since = env.Now.Add(-time.Duration(c.Hours) * time.Hour).Unix()
	} else {
		m.since, m.until = c.Since, c.Until
	}
	return m, nil
}

// Text matches the search against strings.ToLower("subject hash repo"),
// precomputed by the caller. Regexes are case-insensitive, so matching the
// lowercased text gives the same result.
func (m *Matcher) Text(lower string) bool {
	if m.re != nil {
		return m.re.MatchString(lower)
	}
	return m.expr.match(lower)
}

func (m *Matcher) HasText() bool { return m.re != nil || !m.expr.Empty() }

// Author tests a lowercased email.
func (m *Matcher) Author(email string) bool {
	return m.authors == nil || m.authors[email] != m.authorNot
}

// HasAuthor is false when every author passes.
func (m *Matcher) HasAuthor() bool { return m.authors != nil }

// Time tests a commit time against the date condition; Since and Until both count as inside.
func (m *Matcher) Time(unix int64) bool { return m.After(unix) && m.Before(unix) }

// After is the lower end alone, Before the upper end alone: a newest-first list is cut with them.
func (m *Matcher) After(unix int64) bool  { return m.since == 0 || unix >= m.since }
func (m *Matcher) Before(unix int64) bool { return m.until == 0 || unix <= m.until }

// Hit returns the byte range in subject to highlight: the earliest match of
// any positive term, or the regex match.
func (m *Matcher) Hit(subject string) (start, end int, ok bool) {
	if m.re != nil {
		loc := m.re.FindStringIndex(subject)
		if loc == nil || loc[0] == loc[1] {
			return 0, 0, false
		}
		return loc[0], loc[1], true
	}
	lower := strings.ToLower(subject)
	if len(lower) != len(subject) { // ToLower changed byte lengths; offsets would drift
		return 0, 0, false
	}
	best := -1
	bestLen := 0
	for _, c := range m.expr.Clauses {
		for _, t := range c {
			if i := strings.Index(lower, t); i >= 0 && (best < 0 || i < best) {
				best, bestLen = i, len(t)
			}
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	return best, best + bestLen, true
}

// ValidateEmail returns "" or an error code: empty | invalid | duplicate.
// others are the emails already in the list (compared lowercased).
func ValidateEmail(email string, others []string) string {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return "empty"
	}
	if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
		return "invalid"
	}
	for _, o := range others {
		if strings.ToLower(strings.TrimSpace(o)) == e {
			return "duplicate"
		}
	}
	return ""
}

// Suggestion is one way to widen a filter that found nothing.
type Suggestion struct {
	Kind string     `json:"kind"` // authors | paths | date | branch | q | all
	N    int        `json:"n"`    // results after applying it; 0 for "all"
	Cond Conditions `json:"cond"`
}

// Relax lists suggestions that would find something, always ending with "clear everything".
func Relax(c Conditions, count func(Conditions) int) []Suggestion {
	var out []Suggestion
	try := func(kind string, apply bool, f func(*Conditions)) {
		if !apply {
			return
		}
		x := c
		f(&x)
		if n := count(x); n > 0 {
			out = append(out, Suggestion{kind, n, x})
		}
	}
	try("authors", !c.AllAuthors(), func(x *Conditions) { x.Me, x.Authors, x.AuthorNot = false, []string{}, false })
	try("paths", len(c.CleanPaths()) > 0, func(x *Conditions) { x.Paths = []string{} })
	try("date", c.HasDate(), func(x *Conditions) { x.Hours, x.Since, x.Until = 0, 0, 0 })
	try("branch", len(c.Branches) > 0, func(x *Conditions) { x.Branches, x.BranchNot = []string{}, false })
	try("q", strings.TrimSpace(c.Q) != "", func(x *Conditions) { x.Q = "" })
	return append(out, Suggestion{Kind: "all", Cond: Default()})
}

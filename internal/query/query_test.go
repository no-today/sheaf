package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		q      string
		want   [][]string
		neg    []string
		hasOps bool
	}{
		// examples from the 搜索语法 help
		{"YT03374 | YT3374", [][]string{{"YT03374", "YT3374"}}, nil, true},
		{"超量 弹窗", [][]string{{"超量"}, {"弹窗"}}, nil, true},
		{"YT03374 NOT 测试", [][]string{{"YT03374"}}, []string{"测试"}, true},
		{`"审核 规则"`, [][]string{{"审核 规则"}}, nil, false},
		// other spelling of OR
		{"A OR B", [][]string{{"A", "B"}}, nil, true},
		{"A|B", [][]string{{"A", "B"}}, nil, true}, // extension: bare | splits
		{"A OR B OR C", [][]string{{"A", "B", "C"}}, nil, true},
		{"or", [][]string{{"or"}}, nil, false}, // only uppercase OR is an operator
		// NOT
		{"A NOT B", [][]string{{"A"}}, []string{"B"}, true},
		{"A not B", [][]string{{"A"}, {"not"}, {"B"}}, nil, true},
		// mixed: OR binds tighter than AND
		{"A | B C", [][]string{{"A", "B"}, {"C"}}, nil, true},
		{"A NOT B | C", [][]string{{"A", "C"}}, []string{"B"}, true},
		// commas, full-width commas and a leading dash are plain text
		{"A, B", [][]string{{"A,"}, {"B"}}, nil, true},
		{"A，B", [][]string{{"A，B"}}, nil, false},
		{"修复, 优化", [][]string{{"修复,"}, {"优化"}}, nil, true},
		{"A -B", [][]string{{"A"}, {"-B"}}, nil, true},
		{"-B", [][]string{{"-B"}}, nil, false},
		// edge cases
		{"YT03374", [][]string{{"YT03374"}}, nil, false},
		{"-", [][]string{{"-"}}, nil, false}, // a lone dash is a term
		{"| A", [][]string{{"A"}}, nil, true},
		{"A |", [][]string{{"A"}}, nil, true},
		{`"" A`, [][]string{{"A"}}, nil, false},
		{`"-x"`, [][]string{{"-x"}}, nil, false}, // quoted dash is literal
		{"  ", nil, nil, false},
	}
	for _, c := range cases {
		e := Parse(c.q)
		if !reflect.DeepEqual(e.Clauses, c.want) || !reflect.DeepEqual(e.Neg, c.neg) || e.HasOps != c.hasOps {
			t.Errorf("Parse(%q) = %v neg=%v ops=%v; want %v neg=%v ops=%v", c.q, e.Clauses, e.Neg, e.HasOps, c.want, c.neg, c.hasOps)
		}
	}
}

func TestMatch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	env := Env{Me: []string{"Me@Example.com"}, Now: now}
	text := "YT03374 处方审核增加超量用药校验 a3f9c21deadbeef yt-med"
	cases := []struct {
		q     string
		regex bool
		want  bool
	}{
		{"yt03374", false, true},
		{"YT03374 NOT 超量", false, false},
		{"YT03374 -超量", false, false}, // "-超量" is a plain term, not in the text
		{"YT9 | 处方", false, true},
		{"处方 不存在", false, false},
		{"a3f9c21", false, true}, // hash prefix
		{"yt-med", false, true},  // repo name
		{"审核.*超量", true, true},
		{"^yt0337\\d", true, true},
		{"", false, true},
	}
	for _, c := range cases {
		m, err := Compile(Conditions{Q: c.q, Regex: c.regex}, env)
		if err != nil {
			t.Fatalf("%q: %v", c.q, err)
		}
		if got := m.Text(strings.ToLower(text)); got != c.want {
			t.Errorf("Text(%q regex=%v) = %v, want %v", c.q, c.regex, got, c.want)
		}
	}

	m, _ := Compile(Conditions{Me: true, Authors: []string{"li.na@example.com"}, Hours: 7 * 24}, env)
	if !m.Author("me@example.com") || !m.Author("li.na@example.com") || m.Author("x@example.com") {
		t.Error("author filter")
	}
	if !m.Time(now.Unix()-6*86400) || m.Time(now.Unix()-8*86400) {
		t.Error("days filter")
	}
	all, _ := Compile(Conditions{}, env)
	if all.HasAuthor() || all.HasText() || !all.Time(0) {
		t.Error("empty conditions should match everything")
	}
}

func TestHit(t *testing.T) {
	m, _ := Compile(Conditions{Q: "超量 | 审核"}, Env{})
	s := "处方审核增加超量"
	a, b, ok := m.Hit(s)
	if !ok || s[a:b] != "审核" {
		t.Errorf("hit = %q", s[a:b])
	}
	r, _ := Compile(Conditions{Q: "增加.量", Regex: true}, Env{})
	a, b, _ = r.Hit(s)
	if s[a:b] != "增加超量" {
		t.Errorf("regex hit = %q", s[a:b])
	}
}

func TestRegexErrors(t *testing.T) {
	for q, code := range map[string]string{
		"(abc":   "paren",
		"abc)":   "paren",
		"[abc":   "bracket",
		"*abc":   "repeat",
		`abc\`:   "backslash",
		"a(?=b)": "unsupported",
	} {
		_, err := Compile(Conditions{Q: q, Regex: true}, Env{})
		if err == nil || err.Code != code {
			t.Errorf("regex %q: got %v, want %s", q, err, code)
		}
	}
	// plain mode never errors on regex characters
	if _, err := Compile(Conditions{Q: "(abc"}, Env{}); err != nil {
		t.Error("plain search with ( failed")
	}
}

func TestSame(t *testing.T) {
	a := Conditions{Q: "YT03374", Me: true, Authors: []string{"a", "b"}, Hours: 720, Paths: []string{"src/", "r"}}
	b := a
	b.Authors = []string{"b", "a"}
	b.Paths = []string{"r", " src/ ", ""}
	if !Same(a, b) {
		t.Error("author order / path order and spaces should not matter")
	}
	b.Hours = 168
	if Same(a, b) {
		t.Error("days differ")
	}
}

func TestRelax(t *testing.T) {
	c := Conditions{Q: "x", Me: true, Paths: []string{"r"}, Hours: 168, Since: 5}
	counts := map[string]int{"authors": 17, "paths": 0, "date": 3, "q": 12}
	got := Relax(c, func(x Conditions) int {
		switch {
		case x.AllAuthors():
			return counts["authors"]
		case len(x.Paths) == 0:
			return counts["paths"]
		case !x.HasDate():
			return counts["date"]
		case x.Q == "":
			return counts["q"]
		}
		return 0
	})
	var kinds []string
	for _, s := range got {
		kinds = append(kinds, s.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"authors", "date", "q", "all"}) {
		t.Errorf("relax kinds %v", kinds)
	}
	if got[0].N != 17 || !got[0].Cond.AllAuthors() || got[0].Cond.Q != "x" {
		t.Errorf("relax authors %+v", got[0])
	}
}

func TestValidateEmail(t *testing.T) {
	others := []string{"me@cathub.me", "Old@Example.com"}
	for email, code := range map[string]string{
		"":                  "empty",
		"   ":               "empty",
		"not-an-email":      "invalid",
		"a b@example.com":   "invalid",
		"Name <a@b.com>":    "invalid",
		"me@cathub.me":      "duplicate",
		" ME@cathub.me ":    "duplicate",
		"old@example.com":   "duplicate",
		"new@example.com":   "",
		" New@Example.com ": "",
	} {
		if got := ValidateEmail(email, others); got != code {
			t.Errorf("ValidateEmail(%q) = %q, want %q", email, got, code)
		}
	}
}

func TestParseMore(t *testing.T) {
	cases := []struct {
		q      string
		want   [][]string
		neg    []string
		hasOps bool
	}{
		{"A OR OR B", [][]string{{"A", "B"}}, nil, true},
		{"A | | B", [][]string{{"A", "B"}}, nil, true},
		{"A NOT", [][]string{{"A"}}, nil, true},
		{"NOT B", nil, []string{"B"}, true},
		{"NOT -B", nil, []string{"-B"}, true},
		{`A "NOT" B`, [][]string{{"A"}, {"NOT"}, {"B"}}, nil, true},   // quoted NOT is a term
		{`A "|" B`, [][]string{{"A"}, {" | "}, {"B"}}, nil, true},     // a quoted | is rewritten like OR and keeps the padding
		{`"A OR B"`, [][]string{{"A | B"}}, nil, false},               // OR is rewritten even inside quotes
		{`A -"B C"`, [][]string{{"A"}, {"-\"B"}, {"C\""}}, nil, true}, // quotes after a dash are not a phrase
		{`A NOT "B C"`, [][]string{{"A"}}, []string{"B C"}, true},
		{"-A B", [][]string{{"-A"}, {"B"}}, nil, true},
		{"A NOT B | C", [][]string{{"A", "C"}}, []string{"B"}, true}, // neg clears the pending OR... then | sets it again
		{"A | NOT B C", [][]string{{"A"}, {"C"}}, []string{"B"}, true},
		{"A | -B C", [][]string{{"A", "-B"}, {"C"}}, nil, true},
		{"A ，B，C", [][]string{{"A"}, {"，B，C"}}, nil, true},
		{"A,B,C", [][]string{{"A,B,C"}}, nil, false},
		{"a OR b c OR d", [][]string{{"a", "b"}, {"c", "d"}}, nil, true},
		{"ORA", [][]string{{"ORA"}}, nil, false},
		{"A ORB", [][]string{{"A"}, {"ORB"}}, nil, true},
		{"A-B", [][]string{{"A-B"}}, nil, false}, // dash inside a word is literal
		{"--x", [][]string{{"--x"}}, nil, false},
	}
	for _, c := range cases {
		e := Parse(c.q)
		if !reflect.DeepEqual(e.Clauses, c.want) || !reflect.DeepEqual(e.Neg, c.neg) || e.HasOps != c.hasOps {
			t.Errorf("Parse(%q) = %v neg=%v ops=%v; want %v neg=%v ops=%v", c.q, e.Clauses, e.Neg, e.HasOps, c.want, c.neg, c.hasOps)
		}
	}
}

func TestExprHelpers(t *testing.T) {
	if !Parse("").Empty() || !Parse("  ").Empty() || Parse("NOT x").Empty() || Parse("x").Empty() {
		t.Error("Empty")
	}
	for q, want := range map[string]string{"abc": "abc", "A OR B": "", "A B": "", "A NOT B": "", `"a b"`: "a b", "": ""} {
		got, ok := Parse(q).SingleTerm()
		if got != want || ok != (want != "") {
			t.Errorf("SingleTerm(%q) = %q,%v", q, got, ok)
		}
	}
}

func TestMatchMore(t *testing.T) {
	env := Env{}
	text := strings.ToLower("YT03374 处方审核 a3f9c21 yt-med")
	for _, c := range []struct {
		q     string
		regex bool
		want  bool
	}{
		{"YT03374 YT-MED", false, true}, // terms are case-insensitive
		{"NOT yt-med", false, false},    // negation only
		{"NOT nothing", false, true},
		{"-med", false, true}, // a leading dash is part of the term: "yt-med" has "-med"
		{"nope | 处方", false, true},
		{"nope | nada", false, false},
		{"处方 审核", false, true}, // AND in any order
		{`"处方审核"`, false, true},
		{`"处方 审核"`, false, false}, // phrase is literal
		{"   ", false, true},
		{"(abc", false, false}, // plain mode treats regex chars literally
		{"YT0337\\d", true, true},
		{"YT0337\\d", false, false},
		{"A.*Z", true, false},
		{"YT03374", true, true},
		{"^YT", true, true}, // regex is case-insensitive
		{"   ", true, true}, // blank regex matches all
	} {
		m, err := Compile(Conditions{Q: c.q, Regex: c.regex}, env)
		if err != nil {
			t.Fatalf("%q: %v", c.q, err)
		}
		if got := m.Text(text); got != c.want {
			t.Errorf("Text(%q regex=%v) = %v, want %v", c.q, c.regex, got, c.want)
		}
	}
	// HasText: empty, blank, only a negation
	for q, want := range map[string]bool{"": false, "  ": false, "x": true, "-x": true} {
		m, _ := Compile(Conditions{Q: q}, env)
		if m.HasText() != want {
			t.Errorf("HasText(%q) = %v", q, !want)
		}
	}
	if m, _ := Compile(Conditions{Q: "x", Regex: true}, env); !m.HasText() {
		t.Error("HasText regex")
	}
	if m, _ := Compile(Conditions{Q: " ", Regex: true}, env); m.HasText() {
		t.Error("HasText blank regex")
	}
}

func TestMatcherFilters(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	env := Env{Me: []string{"Me@Example.com", "me2@example.com"}, Now: now}
	// Me alone: all of "my" emails
	m, _ := Compile(Conditions{Me: true}, env)
	if !m.HasAuthor() || !m.Author("me@example.com") || !m.Author("me2@example.com") || m.Author("x@example.com") {
		t.Error("me only")
	}
	// authors alone (no Me): only those
	m, _ = Compile(Conditions{Authors: []string{"X@Example.com"}}, env)
	if m.Author("me@example.com") || !m.Author("x@example.com") {
		t.Error("authors only")
	}
	// no filter
	m, _ = Compile(Conditions{}, env)
	if m.HasAuthor() || !m.Author("anyone") {
		t.Error("everyone")
	}
	// date: hours back from now win over a range; a range includes both ends, 0 leaves an end open
	const h = 3600
	t0 := now.Unix()
	for _, tc := range []struct {
		c    Conditions
		at   int64
		want bool
	}{
		{Conditions{Hours: 24}, t0 - 23*h, true},
		{Conditions{Hours: 24}, t0 - 24*h, true},
		{Conditions{Hours: 24}, t0 - 25*h, false},
		{Conditions{Hours: 168}, t0 - 167*h, true},
		{Conditions{Hours: 168}, t0 - 169*h, false},
		{Conditions{}, 0, true},
		{Conditions{Since: 100, Until: 200}, 100, true},
		{Conditions{Since: 100, Until: 200}, 200, true},
		{Conditions{Since: 100, Until: 200}, 99, false},
		{Conditions{Since: 100, Until: 200}, 201, false},
		{Conditions{Since: 100}, t0, true},
		{Conditions{Until: 200}, 0, true},
		{Conditions{Until: 200}, 201, false},
		{Conditions{Hours: 24, Since: 100, Until: 200}, 150, false},
		{Conditions{Hours: 24, Since: 100, Until: 200}, t0, true},
	} {
		m, _ := Compile(tc.c, env)
		if got := m.Time(tc.at); got != tc.want {
			t.Errorf("%+v at %d: %v", tc.c, tc.at, got)
		}
	}
	if d := Default(); d.Me || d.HasDate() || d.Authors == nil || d.Branches == nil || d.Paths == nil || len(d.Paths) > 0 {
		t.Errorf("default %+v", d)
	}
	if (Conditions{Me: true}).AllAuthors() || !(Conditions{}).AllAuthors() || (Conditions{Authors: []string{"a"}}).AllAuthors() {
		t.Error("AllAuthors")
	}
}

func TestHitMore(t *testing.T) {
	s := "Fix YT03374 超量 bug"
	cases := []struct {
		q     string
		regex bool
		want  string
		ok    bool
	}{
		{"bug | fix", false, "Fix", true},           // earliest position wins, not clause order
		{"YT03374 NOT fix", false, "YT03374", true}, // negated terms are not highlighted
		{"NOT fix", false, "", false},
		{"nothing", false, "", false},
		{"", false, "", false},
		{"FIX", false, "Fix", true}, // highlight keeps the subject's case
		{`"yt03374 超量"`, false, "YT03374 超量", true},
		{"BUG", true, "bug", true},
		{"y+", true, "", true},
		{"zzz", true, "", false},
		{"x*", true, "", false}, // empty match is no highlight
	}
	for _, c := range cases {
		m, err := Compile(Conditions{Q: c.q, Regex: c.regex}, Env{})
		if err != nil {
			t.Fatal(err)
		}
		a, b, ok := m.Hit(s)
		if ok != c.ok {
			t.Errorf("Hit(%q regex=%v) ok=%v", c.q, c.regex, ok)
			continue
		}
		if ok && c.want != "" && s[a:b] != c.want {
			t.Errorf("Hit(%q regex=%v) = %q, want %q", c.q, c.regex, s[a:b], c.want)
		}
	}
	// lowercasing that changes byte length (İ is 2 bytes, i̇ is 3) disables highlight instead of drifting
	m, _ := Compile(Conditions{Q: "bug"}, Env{})
	if _, _, ok := m.Hit("İ bug"); ok {
		t.Error("offset drift should give no hit")
	}
}

func TestRegexErrorsMore(t *testing.T) {
	for q, code := range map[string]string{
		`(a)\1`:  "syntax",
		"a{2,1}": "syntax",
		"[z-a]":  "syntax",
		"a**":    "repeat",
		"+a":     "repeat",
		"a|*":    "repeat",
	} {
		_, err := CompileRegex(q)
		if err == nil || err.Code != code {
			t.Errorf("CompileRegex(%q) = %v, want %s", q, err, code)
		}
	}
	if _, err := CompileRegex("a(?<n>b)"); err != nil { // named groups are valid in Go 1.22+
		t.Error(err)
	}
	if _, err := CompileRegex("審核.*"); err != nil {
		t.Error(err)
	}
	if (&SyntaxError{Code: "paren"}).Error() != "regex: paren" {
		t.Error("Error()")
	}
	// blank regex search never errors
	if m, err := Compile(Conditions{Q: "  ", Regex: true}, Env{}); err != nil || m.HasText() {
		t.Error("blank regex")
	}
}

func TestSameMore(t *testing.T) {
	a := Conditions{Q: "x", Me: true, Authors: []string{"a"}, Branches: []string{"b"}, Hours: 168, Paths: []string{"r", "p"}}
	if !Same(a, a) {
		t.Fatal("identical")
	}
	for name, mod := range map[string]func(*Conditions){
		"q":       func(c *Conditions) { c.Q = "y" },
		"qspace":  func(c *Conditions) { c.Q = "x " }, // Q is compared as typed
		"regex":   func(c *Conditions) { c.Regex = true },
		"me":      func(c *Conditions) { c.Me = false },
		"authors": func(c *Conditions) { c.Authors = []string{"b"} },
		"authlen": func(c *Conditions) { c.Authors = nil },
		"branch":  func(c *Conditions) { c.Branches = nil },
		"branchs": func(c *Conditions) { c.Branches = []string{"b", "c"} },
		"bnot":    func(c *Conditions) { c.BranchNot = true },
		"anot":    func(c *Conditions) { c.AuthorNot = true },
		"hours":   func(c *Conditions) { c.Hours = 0 },
		"range":   func(c *Conditions) { c.Hours, c.Since = 0, 1 },
		"path":    func(c *Conditions) { c.Paths = []string{"r", "q"} },
		"paths":   func(c *Conditions) { c.Paths = []string{"r"} },
	} {
		b := a
		mod(&b)
		if Same(a, b) {
			t.Errorf("%s should differ", name)
		}
	}
	// branch order does not matter
	if !Same(Conditions{Branches: []string{"a", "b"}}, Conditions{Branches: []string{"b", "a"}}) {
		t.Error("branch order")
	}
	// nil and empty lists are the same
	if !Same(Conditions{}, Conditions{Authors: []string{}, Paths: []string{" "}}) {
		t.Error("nil vs empty authors / paths")
	}
	// a range under an hours condition does not count
	if !Same(Conditions{Hours: 24}, Conditions{Hours: 24, Since: 5}) || Same(Conditions{Since: 5}, Conditions{Since: 6}) || Same(Conditions{Until: 5}, Conditions{}) {
		t.Error("date")
	}
}

func TestRelaxMore(t *testing.T) {
	c := Conditions{Q: "x", Me: true, Authors: []string{"a"}, Branches: []string{"b"}, Since: 7, Until: 9, Paths: []string{"r", "p"}}
	var seen []Conditions
	got := Relax(c, func(x Conditions) int { seen = append(seen, x); return 5 })
	var kinds []string
	for _, s := range got {
		kinds = append(kinds, s.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"authors", "paths", "date", "branch", "q", "all"}) {
		t.Fatalf("kinds %v", kinds)
	}
	// each suggestion relaxes exactly one condition
	want := []func(*Conditions){
		func(x *Conditions) { x.Me, x.Authors, x.AuthorNot = false, []string{}, false },
		func(x *Conditions) { x.Paths = []string{} },
		func(x *Conditions) { x.Hours, x.Since, x.Until = 0, 0, 0 },
		func(x *Conditions) { x.Branches, x.BranchNot = []string{}, false },
		func(x *Conditions) { x.Q = "" },
	}
	for i, f := range want {
		w := c
		f(&w)
		if !reflect.DeepEqual(got[i].Cond, w) || got[i].N != 5 {
			t.Errorf("%s: %+v", got[i].Kind, got[i])
		}
	}
	if !reflect.DeepEqual(got[5].Cond, Default()) || got[5].N != 0 {
		t.Errorf("all: %+v", got[5])
	}
	// the regex flag is kept when only q is cleared
	r := Relax(Conditions{Q: "x", Regex: true, Authors: []string{}}, func(Conditions) int { return 1 })
	if len(r) != 2 || r[0].Kind != "q" || !r[0].Cond.Regex {
		t.Errorf("regex: %+v", r)
	}
	// nothing to relax, or nothing found by relaxing: only "all"
	if r := Relax(Conditions{Authors: []string{}}, func(Conditions) int { t.Error("counted"); return 1 }); len(r) != 1 || r[0].Kind != "all" {
		t.Errorf("empty: %+v", r)
	}
	if r := Relax(c, func(Conditions) int { return 0 }); len(r) != 1 || r[0].Kind != "all" {
		t.Errorf("zero: %+v", r)
	}
	// blank path / q count as unset
	if r := Relax(Conditions{Q: " ", Paths: []string{" "}, Authors: []string{}}, func(Conditions) int { return 1 }); len(r) != 1 {
		t.Errorf("blank: %+v", r)
	}
}

// Tags saved before branches became a list have a single "branch".
func TestConditionsLegacyBranch(t *testing.T) {
	for in, want := range map[string][]string{
		`{"branch":"feat-x"}`:                       {"feat-x"},
		`{"branch":""}`:                             nil,
		`{"branches":["a","b"]}`:                    {"a", "b"},
		`{"branch":"old","branches":["new"]}`:       {"new"},
		`{"q":"YT1","branch":"x","branchNot":true}`: {"x"},
	} {
		var c Conditions
		if err := json.Unmarshal([]byte(in), &c); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c.Branches, want) {
			t.Errorf("%s: %v", in, c.Branches)
		}
	}
	var c Conditions
	_ = json.Unmarshal([]byte(`{"q":"YT1","branch":"x","branchNot":true}`), &c)
	if c.Q != "YT1" || !c.BranchNot {
		t.Errorf("other fields: %+v", c)
	}
	// written back without the old field
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), `"branch":`) {
		t.Errorf("marshal: %s", b)
	}
}

// Filters saved before Paths and the hours / range date: days, repo and path are read into the new fields, and the
// old fields are not written back.
func TestConditionsLegacy(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Conditions
	}{
		{`{"days":30}`, Conditions{Hours: 720}},
		{`{"days":7,"me":true}`, Conditions{Hours: 168, Me: true}},
		{`{"days":0}`, Conditions{}},
		{`{"days":30,"hours":24}`, Conditions{Hours: 24}},
		{`{"days":30,"since":100}`, Conditions{Since: 100}},
		{`{"repo":"yt-med"}`, Conditions{Paths: []string{"yt-med"}}},
		{`{"path":" src/audit "}`, Conditions{Paths: []string{"src/audit"}}},
		{`{"repo":"yt-med","path":"/src/audit/"}`, Conditions{Paths: []string{"yt-med/src/audit"}}},
		{`{"repo":"","path":""}`, Conditions{}},
		{`{"repo":"old","paths":["new"]}`, Conditions{Paths: []string{"new"}}},
		{`{"branch":"feat-x","days":30,"repo":"a","path":"p","q":"YT1"}`, Conditions{Q: "YT1", Branches: []string{"feat-x"}, Hours: 720, Paths: []string{"a/p"}}},
	} {
		var c Conditions
		if err := json.Unmarshal([]byte(tc.in), &c); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c, tc.want) {
			t.Errorf("%s: %+v, want %+v", tc.in, c, tc.want)
		}
		b, _ := json.Marshal(c)
		for _, old := range []string{`"days"`, `"repo"`, `"path"`, `"branch"`} {
			if strings.Contains(string(b), old) {
				t.Errorf("%s written back with %s: %s", tc.in, old, b)
			}
		}
	}
}

func TestSplitPath(t *testing.T) {
	repos := map[string]bool{"yt-med": true, "group/app": true, "group": true}
	isRepo := func(n string) bool { return repos[n] }
	for _, tc := range []struct{ in, repo, dir string }{
		{"yt-med", "yt-med", ""},
		{" yt-med/ ", "yt-med", ""},
		{"yt-med/src/audit", "yt-med", "src/audit"},
		{"yt-med/src/audit/", "yt-med", "src/audit"},
		{"yt-med//src", "yt-med", "src"},
		{"yt-medx/src", "", "yt-medx/src"},    // a repo name only as a whole segment
		{"group/app/src", "group/app", "src"}, // the longest repo name wins
		{"group/other", "group", "other"},
		{"src/audit", "", "src/audit"},
		{"/yt-med", "", "/yt-med"},
		{"YT-MED", "", "YT-MED"}, // repo names are exact
		{"", "", ""},
	} {
		if repo, dir := SplitPath(tc.in, isRepo); repo != tc.repo || dir != tc.dir {
			t.Errorf("SplitPath(%q) = %q, %q; want %q, %q", tc.in, repo, dir, tc.repo, tc.dir)
		}
	}
	for _, tc := range []struct {
		paths []string
		want  string
	}{
		{nil, ""},
		{[]string{"yt-med"}, "yt-med"},
		{[]string{" yt-med ", ""}, "yt-med"},
		{[]string{"yt-med", "group"}, ""},
		{[]string{"yt-med/src"}, ""},
		{[]string{"src"}, ""},
	} {
		if got := (Conditions{Paths: tc.paths}).SoloRepo(isRepo); got != tc.want {
			t.Errorf("SoloRepo(%q) = %q, want %q", tc.paths, got, tc.want)
		}
	}
}

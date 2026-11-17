// Package query is pure logic with no dependencies: the search syntax, filter
// conditions, Tag equality and naming rules, and relax suggestions.
package query

import (
	"regexp"
	"regexp/syntax"
	"strings"
)

// Expr is a parsed plain-text search: every clause must match (AND), any
// term of a clause may match (OR), no Neg term may match.
type Expr struct {
	Clauses [][]string `json:"clauses"`
	Neg     []string   `json:"neg"`
	// HasOps is true when an operator was used; the UI then shows the explanation.
	HasOps bool `json:"hasOps"`
}

var orWord = regexp.MustCompile(`(^|\s)OR(\s|$)`)
var tokenRe = regexp.MustCompile(`"([^"]*)"|(\|)|(\S+)`)

// Parse parses the plain-text syntax:
//
//	A | B, A OR B   any of them
//	A B             all of them
//	NOT B           exclude B
//	"A B"           the phrase
//
// A bare "|" inside a word ("A|B") is an operator too. Commas and "-B" are plain
// text because they are common in commit messages.
func Parse(q string) Expr {
	s := strings.ReplaceAll(q, "|", " | ")
	for orWord.MatchString(s) { // overlapping matches ("A OR OR B") need repeats
		s = orWord.ReplaceAllString(s, "$1|$2")
	}
	var e Expr
	pendOr, pendNot := false, false
	for _, m := range tokenRe.FindAllStringSubmatchIndex(s, -1) {
		quoted := m[2] >= 0
		var term string
		switch {
		case quoted:
			term = s[m[2]:m[3]]
		case m[4] >= 0:
			pendOr, e.HasOps = true, true
			continue
		default:
			term = s[m[6]:m[7]]
			if term == "NOT" {
				pendNot, e.HasOps = true, true
				continue
			}
		}
		neg := pendNot
		pendNot = false
		if term == "" {
			continue
		}
		if neg {
			e.Neg = append(e.Neg, term)
			e.HasOps, pendOr = true, false
			continue
		}
		if pendOr && len(e.Clauses) > 0 {
			last := len(e.Clauses) - 1
			e.Clauses[last] = append(e.Clauses[last], term)
		} else {
			e.Clauses = append(e.Clauses, []string{term})
			if len(e.Clauses) > 1 {
				e.HasOps = true
			}
		}
		pendOr = false
	}
	return e
}

// Empty is true when the expression matches everything.
func (e Expr) Empty() bool { return len(e.Clauses) == 0 && len(e.Neg) == 0 }

// SingleTerm returns the term when the search is exactly one word.
func (e Expr) SingleTerm() (string, bool) {
	if len(e.Clauses) == 1 && len(e.Clauses[0]) == 1 && len(e.Neg) == 0 {
		return e.Clauses[0][0], true
	}
	return "", false
}

func (e Expr) lower() Expr {
	out := Expr{HasOps: e.HasOps, Neg: make([]string, len(e.Neg)), Clauses: make([][]string, len(e.Clauses))}
	for i, t := range e.Neg {
		out.Neg[i] = strings.ToLower(t)
	}
	for i, c := range e.Clauses {
		out.Clauses[i] = make([]string, len(c))
		for j, t := range c {
			out.Clauses[i][j] = strings.ToLower(t)
		}
	}
	return out
}

func (e Expr) match(lower string) bool {
	for _, n := range e.Neg {
		if strings.Contains(lower, n) {
			return false
		}
	}
	for _, c := range e.Clauses {
		ok := false
		for _, t := range c {
			if strings.Contains(lower, t) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// SyntaxError is a regex error with a reason code the UI translates.
type SyntaxError struct {
	Code string `json:"code"` // paren | bracket | repeat | backslash | unsupported | syntax
}

func (e *SyntaxError) Error() string { return "regex: " + e.Code }

// CompileRegex compiles a case-insensitive regex search..
func CompileRegex(q string) (*regexp.Regexp, *SyntaxError) {
	re, err := regexp.Compile("(?i)" + q)
	if err == nil {
		return re, nil
	}
	code := "syntax"
	if se, ok := err.(*syntax.Error); ok {
		switch se.Code {
		case syntax.ErrMissingParen, syntax.ErrUnexpectedParen:
			code = "paren"
		case syntax.ErrMissingBracket:
			code = "bracket"
		case syntax.ErrMissingRepeatArgument, syntax.ErrInvalidRepeatOp:
			code = "repeat"
		case syntax.ErrTrailingBackslash:
			code = "backslash"
		case syntax.ErrInvalidPerlOp:
			code = "unsupported" // lookahead, backreferences: RE2 has none
		}
	}
	return nil, &SyntaxError{Code: code}
}

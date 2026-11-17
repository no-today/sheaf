package diffview

import (
	"slices"
	"testing"

	"sheaf/internal/session"
)

func hunk(header string, lines ...session.Line) session.Hunk {
	return session.Hunk{Header: header, Lines: lines}
}

func TestTextsOfAggTitlesEveryCommit(t *testing.T) {
	p := session.Patch{Hunks: []session.Hunk{hunk("@@ -1 +1,2 @@", session.Line{Kind: " ", Old: 1, New: 1, Text: "a"}, session.Line{Kind: "+", New: 2, Text: "b"})}}
	for _, c := range []struct {
		ap     session.AggPatch
		titles []string
		more   int
		binary bool
	}{
		{session.AggPatch{Mode: "range", Parts: []session.AggPart{{Patch: p}}}, []string{""}, 0, false},
		{session.AggPatch{Mode: "commits", More: 3, Parts: []session.AggPart{{Hash: "1234567890", Subject: "old", Patch: p}, {Hash: "abcdef0123", Subject: "new", Patch: p}}},
			[]string{"1234567  old", "abcdef0  new"}, 3, false},
		{session.AggPatch{Mode: "range", Parts: []session.AggPart{{Patch: session.Patch{Binary: true}}}}, nil, 0, true},
		{session.AggPatch{Mode: "commits"}, nil, 0, false},
	} {
		tx := TextsOfAgg(c.ap)
		var titles []string
		for _, pt := range tx.Parts {
			titles = append(titles, pt.Title)
		}
		if !slices.Equal(titles, c.titles) || tx.More != c.more || tx.Binary != c.binary {
			t.Errorf("%+v: titles %q more %d binary %v", c.ap, titles, tx.More, tx.Binary)
		}
	}
}

func TestPartsJoinIntoOnePatchWithTitledHeaders(t *testing.T) {
	a := session.Patch{Hunks: []session.Hunk{
		hunk("@@ -3,2 +3,3 @@ func f()", session.Line{Kind: " ", Old: 3, New: 3, Text: "x"}, session.Line{Kind: "+", New: 4, Text: "y"}, session.Line{Kind: " ", Old: 4, New: 5, Text: "z"}),
		hunk("@@ -20 +21 @@", session.Line{Kind: "-", Old: 20, Text: "p"}, session.Line{Kind: "+", New: 21, Text: "q"}),
	}}
	b := session.Patch{Hunks: []session.Hunk{hunk("@@ -1,0 +1 @@", session.Line{Kind: "+", New: 1, Text: "top"})}}
	fp, titles := patchOf("f.go", []Part{{Title: "1111111  one", Patch: a}, {Title: "2222222  two", Patch: b}})
	if len(fp.Hunks) != 3 || !slices.Equal(titles, []string{"1111111  one", "2222222  two"}) {
		t.Fatalf("hunks %d titles %q", len(fp.Hunks), titles)
	}
	h := fp.Hunks[0]
	if h.OldStart != 3 || h.OldLines != 2 || h.NewStart != 3 || h.NewLines != 3 || !slices.Equal(h.Lines, []string{" x", "+y", " z"}) {
		t.Errorf("first hunk %+v", h)
	}
	if h := fp.Hunks[1]; h.OldStart != 20 || h.OldLines != 1 || h.Section != "" {
		t.Errorf("second hunk %+v", h)
	}
	if h := fp.Hunks[2]; h.OldStart != 1 || h.OldLines != 0 || h.NewLines != 1 {
		t.Errorf("third hunk %+v", h)
	}
	// The viewer's header of a part's first hunk (editor.SetPatch: ranges, a space, the section) leads back to its title;
	// the others are plain headers.
	if title, rest, ok := partOf("@@ -3,2 +3,3 @@ "+fp.Hunks[0].Section, titles); !ok || title != "1111111  one" || rest != "@@ -3,2 +3,3 @@ func f()" {
		t.Errorf("first header: %q %q %v", title, rest, ok)
	}
	if _, rest, ok := partOf("@@ -20,1 +21,1 @@", titles); ok || rest != "@@ -20,1 +21,1 @@" {
		t.Errorf("plain header: %q %v", rest, ok)
	}
	if title, _, ok := partOf("@@ -1,0 +1,1 @@ "+fp.Hunks[2].Section, titles); !ok || title != "2222222  two" {
		t.Errorf("third header: %q %v", title, ok)
	}
}

func TestViewShowsTheParts(t *testing.T) {
	p := session.Patch{Hunks: []session.Hunk{hunk("@@ -1 +1,2 @@", session.Line{Kind: " ", Old: 1, New: 1, Text: "a"}, session.Line{Kind: "+", New: 2, Text: "b"})}}
	tx := TextsOfAgg(session.AggPatch{Mode: "commits", More: 2, Parts: []session.AggPart{{Hash: "1234567890", Subject: "old one", Patch: p}, {Hash: "abcdef0123", Subject: "new one", Patch: p}}})
	s := New([]File{{Path: "f.txt", Status: "M", Texts: &tx}}, nil)
	tt := tester(s)
	tt.Frame()
	for _, want := range []string{"1234567  old one", "abcdef0  new one", "另有 2 个提交改过它，未列出"} {
		if !tt.HasText(want) && !tt.HasText(trEn(want)) {
			t.Errorf("missing %q; texts %q", want, tt.Texts())
		}
	}
	if d := s.Diff(); d == nil || len(d.Hunks()) != 2 {
		t.Errorf("changes of the parts: %v", d)
	}
}

// trEn is want in English where the Tester's language is not Chinese.
func trEn(want string) string {
	if want == "另有 2 个提交改过它，未列出" {
		return "2 more commits changed it, not listed"
	}
	return want
}

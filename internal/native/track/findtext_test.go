package track

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func typeFind(t *testing.T, h *harness, text string) {
	t.Helper()
	if err := h.TT.Click(wordsOf(h).FindReq); err != nil {
		t.Fatal(err)
	}
	h.TT.Key(ui.Cmd, ui.KeyA)
	h.TT.Type(text)
	h.TT.Frame()
}

func TestSearchFieldOpensCommitTextWhenNoRequirementHits(t *testing.T) {
	h := open(t, "en-US")
	typeFind(t, h, "work")
	if !h.TT.HasText("Search commit messages for “work”") {
		t.Fatalf("no entry line; texts %q", h.TT.Texts())
	}
	// "work" hits requirement YT1's title: Enter takes the requirement.
	h.TT.Key(0, ui.KeyEnter)
	h.TT.Frame()
	if h.p.shownReq() != "YT1" {
		t.Errorf("Enter with a hit opened %q", h.p.shownReq())
	}

	typeFind(t, h, "second in a")
	h.TT.Key(0, ui.KeyEnter)
	h.Until("the text result", func() bool {
		return h.p.temp.Text == "second in a" && h.p.res != nil && !h.p.aggBusy && len(h.p.rows) > 0
	})
	if h.p.temp.Req != "" || h.p.shownReq() != "" || h.p.input.Requirement != "" || h.p.input.Cond.Q != "second in a" || h.p.rows[0].Repo != "repo-a" {
		t.Errorf("text result: temp %+v input %+v rows %+v", h.p.temp, h.p.input, h.p.rows)
	}

	// Clicking the line opens another text in its place: there is one temporary entry.
	typeFind(t, h, "only in b")
	if err := h.TT.Click("Search commit messages for “only in b”"); err != nil {
		t.Fatal(err)
	}
	h.Until("the second text", func() bool {
		return h.p.temp.Text == "only in b" && h.p.res != nil && !h.p.aggBusy && len(h.p.rows) > 0
	})
	if h.p.rows[0].Repo != "repo-b" {
		t.Errorf("rows %+v", h.p.rows)
	}
}

func TestSearchEntryLineIsGoneWithoutText(t *testing.T) {
	h := open(t, "en-US")
	if h.TT.HasText("Find commits saying") {
		t.Fatal("entry line without text")
	}
}

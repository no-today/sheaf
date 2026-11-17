package track

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
)

// openUntracked is the page on the fixture with an empty tracked list: YT1 is only a candidate.
func openUntracked(t *testing.T, locale string) *harness {
	t.Helper()
	h := viewtest.Open(t, fixture(t))
	if err := h.S.SetRequirement(`YT\d+`); err != nil {
		t.Fatal(err)
	}
	h.DockOf = Dock()
	hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
	x := &harness{hr, hr.V.(*Page)}
	x.Until("candidates", func() bool { return x.p.reqsOK && len(x.p.cands) > 0 })
	x.TT.Frame()
	return x
}

func TestEmptyListOffersCandidatesAndTrackingIsTheUsersAct(t *testing.T) {
	h := openUntracked(t, "en-US")
	for _, s := range []string{"Type a requirement number to follow it", "Can follow", "YT1", "Follow YT1"} {
		if !h.TT.HasText(s) {
			t.Fatalf("missing %q; texts %q", s, h.TT.Texts())
		}
	}
	if len(h.p.reqs) != 0 || len(h.p.s.Tracked()) != 0 || h.p.shownReq() != "" {
		t.Fatalf("nothing is tracked or shown yet: %v %v %q", h.p.reqs, h.p.s.Tracked(), h.p.shownReq())
	}

	// The row only looks.
	if err := h.TT.Click("YT1 "); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	if h.p.shownReq() != "YT1" || len(h.p.s.Tracked()) != 0 {
		t.Errorf("looking tracked it: shown %q tracked %v", h.p.shownReq(), h.p.s.Tracked())
	}

	// The button tracks and shows.
	if err := h.TT.Click("Follow YT1"); err != nil {
		t.Fatal(err)
	}
	h.Until("tracked and listed", func() bool { return len(h.p.reqs) == 1 && h.p.reqs[0].ID == "YT1" })
	if !slices.Equal(h.p.s.Tracked(), []string{"YT1"}) || h.p.shownReq() != "YT1" {
		t.Errorf("tracked %v shown %q", h.p.s.Tracked(), h.p.shownReq())
	}
	if len(h.p.cands) != 0 {
		t.Errorf("a tracked number is no candidate: %v", h.p.cands)
	}
}

func TestSearchListsCandidatesAndTheTypedNumber(t *testing.T) {
	h := openUntracked(t, "en-US")
	typeFind(t, h, "yt88")
	if !h.TT.HasText("Follow YT88") || h.TT.HasText("Follow YT1") {
		t.Errorf("the typed number should be offered, and only it; texts %q", h.TT.Texts())
	}
	typeFind(t, h, "YT")
	if !h.TT.HasText("Follow YT1") || !h.TT.HasText("Search commit messages for “YT”") {
		t.Errorf("candidate by number; texts %q", h.TT.Texts())
	}
}

func TestEnterInTheSearchFieldHasThreeRoutes(t *testing.T) {
	h := openUntracked(t, "en-US")

	// A whole number: tracked and opened, the field emptied.
	typeFind(t, h, "yt77")
	h.TT.Key(0, ui.KeyEnter)
	h.Until("tracked", func() bool { return len(h.p.s.Tracked()) == 1 && len(h.p.reqs) == 1 })
	if h.p.s.Tracked()[0] != "YT77" || h.p.shownReq() != "YT77" || h.p.find != "" {
		t.Errorf("tracked %v shown %q find %q", h.p.s.Tracked(), h.p.shownReq(), h.p.find)
	}
	if r := h.p.reqs[0]; r.Stage != "dev" || r.Repos != 0 {
		t.Errorf("a number found nowhere: %+v", r)
	}

	// A tracked hit: opened, nothing added.
	typeFind(t, h, "YT77")
	h.TT.Key(0, ui.KeyEnter)
	h.TT.Frame()
	if len(h.p.s.Tracked()) != 1 {
		t.Errorf("Enter on a tracked hit added %v", h.p.s.Tracked())
	}

	// Anything else: the commits whose message says it.
	typeFind(t, h, "second in a")
	h.TT.Key(0, ui.KeyEnter)
	h.Until("the text result", func() bool { return h.p.temp.Text == "second in a" && h.p.res != nil && !h.p.aggBusy })
	if len(h.p.s.Tracked()) != 1 {
		t.Errorf("text search tracked %v", h.p.s.Tracked())
	}
}

func TestStopTrackingFromTheRowKeepsTheScreenAndUndoes(t *testing.T) {
	h := open(t, "en-US")
	rightClickFirstRow(t, h, "Awaiting master")
	if err := h.TT.ChooseMenuItem("Unfollow"); err != nil {
		t.Fatal(err)
	}
	h.Until("the row is gone", func() bool { return len(h.p.reqs) == 0 && len(h.p.s.Tracked()) == 0 })
	if h.p.shownReq() != "YT1" {
		t.Errorf("the requirement on screen changed to %q", h.p.shownReq())
	}
	h.Until("the toast", func() bool { h.TT.Frame(); return h.TT.HasText("Unfollowed YT1") })
	if err := h.TT.Click("Undo"); err != nil {
		t.Fatalf("%v; texts %q", err, h.TT.Texts())
	}
	h.Until("tracked again", func() bool { return slices.Equal(h.p.s.Tracked(), []string{"YT1"}) && len(h.p.reqs) == 1 })
}

// trackingShots are the left pane with nothing tracked, a search that lists candidates, and the toast after stopping.
func trackingShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h := viewtest.Open(t, landedFixture(t))
		if err := h.S.SetRequirement(`[A-Z]+-?\d+`); err != nil {
			t.Fatal(err)
		}
		h.DockOf = Dock()
		hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
		x := &harness{hr, hr.V.(*Page)}
		x.Until("candidates", func() bool { return x.p.reqsOK && len(x.p.cands) > 0 })
		x.TT.Frame()
		x.Shot("req-track-empty-" + locale)

		for _, id := range []string{"YT1", "LAND-1001"} {
			if err := x.p.s.Track(id); err != nil {
				t.Fatal(err)
			}
		}
		x.p.loadReqs()
		x.Until("tracked", func() bool { return len(x.p.reqs) == 2 })
		x.p.chooseReq(x.p.reqs[0])
		x.Until("the stream", func() bool { return !x.p.filesBusy && x.p.commits != nil && x.p.stream.Loaded() > 0 })
		x.p.find = "LAND"
		x.TT.Frame()
		x.Shot("req-track-find-" + locale)

		x.p.find = ""
		x.railRightClick(t, x.p.reqs[0].ID+" "+x.p.reqs[0].Title)
		if err := x.TT.ChooseMenuItem(wordsOf(x).Untrack); err != nil {
			t.Fatal(err)
		}
		x.Until("the row is gone", func() bool { return len(x.p.reqs) == 1 })
		x.TT.Frame()
		x.Shot("req-track-untracked-" + locale)
	}
}

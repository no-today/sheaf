package track

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
)

func TestShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			empty := openWithoutFilters(t, locale)
			empty.Shot("track-no-filters-" + locale)
			empty.p.choose(wcID)
			empty.Until("clean working directories", func() bool { return empty.p.dirty.view != nil })
			empty.Shot("track-clean-" + locale)

			h := open(t, locale)
			h.Until("filter details", func() bool { return !h.p.filesBusy && h.p.commits != nil && h.p.stream.Loaded() > 0 })
			h.Shot("track-overview-" + locale)
			h.TT.SetSize(1180, 760)
			h.Shot("track-filter-1180-" + locale)
			h.TT.SetSize(1400, 900)
			h.p.diffMode = session.DiffWorktree
			h.p.apply(false)
			h.Until("worktree mode", func() bool { return !h.p.filesBusy && h.p.filesIn.DiffMode == session.DiffWorktree })
			h.Shot("track-no-files-" + locale)
			h.p.chooseTemp("no-matching-fixture-branch")
			h.Until("no matching commits", func() bool { return !h.p.aggBusy && len(h.p.rows) == 0 })
			h.Shot("track-no-matches-" + locale)
		})
	}
	t.Run("empty-commits", func(t *testing.T) {
		for _, locale := range []string{"zh-Hans", "en-US"} {
			p := &Page{commits: &commitList{AggCommits: session.AggCommits{Merges: 2}}}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Themed(c, theme.Theme()).Grow(1).Children(func() { p.commitsList(c, tr.Of(c)) })
			}, 600, 260)
			tt.SetLocale(locale)
			hr := &viewtest.Harness{T: t, H: &viewtest.Host{}, TT: tt}
			hr.Shot("track-no-commits-" + locale)
			tt.SetSize(1180, 760)
			hr.Shot("track-no-commits-1180-" + locale)
		}
	})
	t.Run("requirement", requirementShots)
	t.Run("rail", railShots)
	t.Run("tracking", trackingShots)
	t.Run("pointer", trackPointerShots)
	t.Run("remerge", remergeShots)
	t.Run("review", reviewShots)
	t.Run("uncommitted", uncommittedShots)
	t.Run("create-mr", mrReviewShots)
	t.Run("sync", syncShots)
	t.Run("first-index", firstIndexShots)
}

func uncommittedShots(t *testing.T) {
	h := openDirty(t, "zh-Hans")
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h.TT.SetLocale(locale)
		for _, width := range []int{820, 1180, 1440} {
			h.TT.SetSize(width, 900)
			w := tr.For(locale)
			h.p.dirty.focus = ""
			h.p.dirty.tab = 0
			h.TT.Frame()
			h.p.dirty.stream.ScrollToTop()
			h.Until("uncommitted first file", func() bool {
				return h.p.dirty.stream.Current() == 0 && h.p.dirty.stream.Loaded() == len(h.p.dirty.stream.Files())
			})
			h.Shot(fmt.Sprintf("uncommitted-stream-%d-%s", width, locale))
			shotClick(t, h, w.TabFiles+" 4")
			h.Until("visible file line counts", func() bool {
				for i, f := range h.p.dirty.stream.Files() {
					if !h.p.dirty.fileStat(h.p, i, f.Source.(session.AggFile)).ready {
						return false
					}
				}
				return true
			})
			h.Shot(fmt.Sprintf("uncommitted-files-%d-%s", width, locale))
			shotClick(t, h, "mobile-YT1")
			h.Until("selected directory files", func() bool { return h.p.dirty.focus != "" && len(h.p.dirty.stream.Files()) == 2 })
			h.Shot(fmt.Sprintf("uncommitted-selected-files-%d-%s", width, locale))
			shotClick(t, h, w.TabChanges+" 2")
			h.Until("selected directory diffs", func() bool { return h.p.dirty.stream.Loaded() == 2 })
			h.Shot(fmt.Sprintf("uncommitted-selected-stream-%d-%s", width, locale))
		}
	}
	previousView := h.p.dirty.view
	if err := h.H.S.Track("YT1"); err != nil {
		t.Fatal(err)
	}
	h.H.Inf = h.H.S.Info()
	h.p.Event(view.ConfigChanged{})
	h.Until("tracked and untracked directory fixture", func() bool {
		return h.p.dirty.view != previousView && len(h.p.dirty.dirs()) == h.p.dirtyDirs() && h.p.dirty.stream.Loaded() == len(h.p.dirty.stream.Files())
	})
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h.TT.SetLocale(locale)
		for _, width := range []int{820, 1180, 1440} {
			h.TT.SetSize(width, 900)
			h.p.dirty.focus = ""
			h.p.dirty.tab = 0
			h.TT.Frame()
			h.p.dirty.stream.ScrollToTop()
			h.Until("tracked directory stream", func() bool {
				return h.p.dirty.stream.Loaded() == len(h.p.dirty.stream.Files())
			})
			h.Shot(fmt.Sprintf("uncommitted-tracked-%d-%s", width, locale))
		}
	}
	empty := openWithoutFilters(t, "zh-Hans")
	empty.p.choose(wcID)
	empty.Until("no dirty working directories", func() bool { return empty.p.dirty.view != nil })
	for _, locale := range []string{"zh-Hans", "en-US"} {
		empty.TT.SetLocale(locale)
		for _, width := range []int{1180, 1440} {
			empty.TT.SetSize(width, 900)
			empty.Shot(fmt.Sprintf("uncommitted-empty-%d-%s", width, locale))
		}
	}
}

func openWithoutFilters(t *testing.T, locale string) *harness {
	t.Helper()
	h := viewtest.Open(t, fixture(t))
	viewtest.TrackAll(t, h.S)
	h.DockOf = Dock()
	hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
	x := &harness{hr, hr.V.(*Page)}
	x.Until("requirements loaded", func() bool { return x.p.reqsOK })
	return x
}

func trackPointerShots(t *testing.T) {
	h := open(t, "zh-Hans")
	if err := h.TT.Click("查看"); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	h.Shot("track-target-popover")
	h.TT.Key(0, ui.KeyEscape)
	h.TT.Frame()
	h.TT.Key(0, ui.KeyTab)
	h.TT.Key(0, ui.KeyTab)
	h.TT.Frame()
	h.Shot("track-tab-focus")
}

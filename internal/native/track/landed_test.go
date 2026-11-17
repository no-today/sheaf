package track

import (
	"path/filepath"
	"testing"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

// landedFixture is the page fixture plus repo-c with three requirements already in its main branch.
func landedFixture(t *testing.T) string {
	t.Helper()
	root := fixture(t)
	r := testutil.NewRepo(t, filepath.Join(root, "repo-c"))
	r.Commit("init", map[string]string{"f": "0\n"})
	main := r.Git("rev-parse", "--abbrev-ref", "HEAD")
	for _, id := range []string{"LAND-1001", "LAND-1002", "LAND-1003"} {
		r.Git("checkout", "-q", "-b", "feat-"+id, main)
		r.Commit(id+" landed "+id[len(id)-1:], map[string]string{id + ".txt": "1\n"})
		r.Git("checkout", "-q", main)
		r.Merge("feat-"+id, "Merge branch 'feat-"+id+"'")
	}
	r.Git("update-ref", "refs/remotes/origin/"+main, main)
	for _, id := range []string{"LAND-1001", "LAND-1002", "LAND-1003"} {
		r.Git("worktree", "add", "-q", filepath.Join(root, "repo-c-"+id), "feat-"+id)
	}
	return root
}

func openLanded(t *testing.T, locale string) *harness {
	t.Helper()
	h := viewtest.Open(t, landedFixture(t))
	if err := h.S.SetMergeFlow([]session.MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.S.SetRequirement(`[A-Z]+-?\d+`); err != nil {
		t.Fatal(err)
	}
	h.DockOf = Dock()
	viewtest.TrackAll(t, h.S)
	hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
	x := &harness{hr, hr.V.(*Page)}
	x.TT.SetSize(1440, 900)
	x.Until("requirements", func() bool {
		landed := 0
		for _, r := range x.p.reqs {
			if r.Stage == session.StageLanded {
				landed++
			}
		}
		return landed == 3 && x.p.shownReq() != ""
	})
	return x
}

func TestMergedDirtyRequirementShowsUncommittedRepo(t *testing.T) {
	root := landedFixture(t)
	shotWrite(t, filepath.Join(root, "repo-c-LAND-1001", "scratch.txt"), "unfinished\n", 0o644)
	host := viewtest.Open(t, root)
	if err := host.S.SetRequirement(`[A-Z]+-?\d+`); err != nil {
		t.Fatal(err)
	}
	host.DockOf = Dock()
	viewtest.TrackAll(t, host.S)
	mounted := viewtest.Mount(t, host, "zh-Hans", func(host view.Host) view.View { return New(host) })
	h := &harness{mounted, mounted.V.(*Page)}
	h.TT.SetSize(1440, 900)
	h.Until("dirty requirement", func() bool { return h.p.reqsOK })
	h.p.chooseReq(session.Requirement{ID: "LAND-1001"})
	h.Until("merged requirement with dirty worktree", func() bool {
		if h.p.aggBusy || h.p.filesBusy || len(h.p.rows) != 1 {
			return false
		}
		_, shown := h.TT.Find("repo-c 开发中")
		return h.p.rows[0].Stage == session.StageDev && h.p.rows[0].Uncommitted == 1 && shown
	})
	h.Shot("track-merged-uncommitted-zh-Hans")
}

func TestRequirementWithoutCommitsShowsUncommittedRepo(t *testing.T) {
	root := fixture(t)
	repo := testutil.NewRepo(t, filepath.Join(root, "repo-c"))
	repo.Commit("init", map[string]string{"base.txt": "base\n"})
	worktree := filepath.Join(root, "repo-c-YT03847")
	repo.Git("worktree", "add", "-q", "-b", "feat-2611-YT03847", worktree)
	shotWrite(t, filepath.Join(worktree, "new.txt"), "unfinished\n", 0o644)
	h := openAt(t, root, "zh-Hans")
	h.p.chooseReq(session.Requirement{ID: "YT03847"})
	h.Until("requirement without commits", func() bool {
		return h.p.res != nil && !h.p.aggBusy && !h.p.filesBusy
	})
	h.Shot("track-before-first-commit-zh-Hans")
	if len(h.p.rows) != 1 || h.p.rows[0].Uncommitted != 1 || h.p.rows[0].Stage != session.StageDev {
		t.Fatalf("uncommitted repo missing: %+v", h.p.rows)
	}
	h.Until("uncommitted section loaded", func() bool {
		return h.p.stream.Loaded() == 1 && h.TT.HasText("new.txt")
	})
	h.Shot("track-before-first-commit-loaded-zh-Hans")
	h.p.diffMode = session.DiffWorktree
	h.p.apply(true)
	h.Until("requirement's uncommitted mode loaded", func() bool {
		return !h.p.filesBusy && h.p.filesIn.DiffMode == session.DiffWorktree && h.p.stream.Loaded() == 1
	})
	if len(h.p.files) != 1 || h.p.files[0].Path != "new.txt" {
		t.Fatalf("uncommitted files missing: %+v", h.p.files)
	}
	h.Shot("track-before-first-commit-uncommitted-zh-Hans")
}

func openArchived(t *testing.T, locale string) *harness {
	t.Helper()
	h := openLanded(t, locale)
	for _, id := range []string{"LAND-1001", "LAND-1002", "LAND-1003"} {
		if err := h.H.S.ArchiveRequirement(id, true); err != nil {
			t.Fatal(err)
		}
	}
	h.p.loadReqs()
	h.Until("archived group", func() bool {
		count := 0
		for _, req := range h.p.reqs {
			if req.Stage == session.StageArchived {
				count++
			}
		}
		return count == 3
	})
	return h
}

func TestArchivedGroupIsFoldedUntilOpened(t *testing.T) {
	h := openArchived(t, "en-US")
	if h.p.archivedOpen || h.TT.HasText("LAND-1002") {
		t.Fatalf("landed requirements show though the group is folded: %q", h.TT.Texts())
	}
	if !h.TT.HasText("Archived") || !h.TT.HasText("3") {
		t.Errorf("the folded group is one line with its count: %q", h.TT.Texts())
	}
	if err := h.TT.Click("Archived"); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	if !h.p.archivedOpen || !h.TT.HasText("LAND-1002") {
		t.Errorf("opening shows the group: %q", h.TT.Texts())
	}
	head, _ := h.TT.Find("Archived")
	first, _ := h.TT.Find("LAND-1001")
	if gap := first.Y - (head.Y + head.H); gap < 4 {
		t.Errorf("the first archived row sits on its heading: heading %+v, row %+v", head, first)
	}
	h.Shot("track-archived-open")
	if err := h.TT.Click("Archived"); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	if h.p.archivedOpen || h.TT.HasText("LAND-1002") {
		t.Errorf("folding hides it again")
	}
}

func TestFoldedArchivedGroupStillShowsSearchHitsAndTheShownRequirement(t *testing.T) {
	h := openArchived(t, "en-US")
	h.p.find = "1002"
	h.TT.Frame()
	if !h.TT.HasText("LAND-1002") || h.TT.HasText("LAND-1003") {
		t.Errorf("a search shows the landed hits only: %q", h.TT.Texts())
	}
	h.p.find = ""
	h.p.chooseReq(session.Requirement{ID: "LAND-1003"})
	h.Until("the landed requirement", func() bool { return h.p.shownReq() == "LAND-1003" })
	h.TT.Frame()
	if !h.TT.HasText("LAND-1003") || h.TT.HasText("LAND-1001") {
		t.Errorf("the requirement on screen stays in the folded group, the others do not: %q", h.TT.Texts())
	}
	// The other groups cannot be folded.
	if !h.TT.HasText("Awaiting master") || !h.TT.HasText("YT1") {
		t.Errorf("developing group: %q", h.TT.Texts())
	}
}

// railShots are the left pane's states at 1440 wide: landed folded and open, a title being edited, a search with its
// commit-text line.
func railShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h := openArchived(t, locale)
		h.Until("the stream", func() bool { return !h.p.filesBusy && h.p.commits != nil && h.p.stream.Loaded() > 0 })
		h.Shot("req-rail-folded-" + locale)
		if err := h.TT.Click(wordsOf(h).StageArchived); err != nil {
			t.Fatal(err)
		}
		h.TT.Frame()
		h.Shot("req-rail-open-" + locale)
		h.p.archivedOpen = false
		h.p.renaming, h.p.renameText = "YT1", "Billing rework"
		h.TT.Frame()
		h.Shot("req-rail-rename-" + locale)
		h.p.renaming = ""
		h.p.find = "refund timeout"
		h.TT.Frame()
		h.Shot("req-rail-find-" + locale)
	}
}

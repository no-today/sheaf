package track

import (
	"path/filepath"
	"testing"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func openMergedTrack(t *testing.T, locale string) *harness {
	t.Helper()
	root := fixture(t)
	r := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	r.Git("checkout", "-q", "-b", "uat", "master")
	r.Merge("feat-YT1", "land YT1")
	h := viewtest.Open(t, root)
	viewtest.TrackAll(t, h.S)
	if err := h.S.SetCompareTargets(session.DefaultRelease, "uat"); err != nil {
		t.Fatal(err)
	}
	h.Inf = h.S.Info()
	h.DockOf = Dock()
	hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
	x := &harness{hr, hr.V.(*Page)}
	x.p.chooseTemp("feat-YT1")
	x.Until("merged branch details", func() bool {
		return x.p.res != nil && x.p.res.Commits == 1 && !x.p.aggBusy && !x.p.filesBusy && x.p.commits != nil
	})
	return x
}

func TestMergedBranchStaysVisible(t *testing.T) {
	h := openMergedTrack(t, "zh-Hans")
	a := h.p.rows[0]
	if a.BranchFrom != session.FromMerged || a.Branch != "" || a.Worktree != "" || h.p.checked[a.Key] {
		t.Errorf("merged row=%+v, checked=%v", a, h.p.checked)
	}
	if len(h.p.files) != 1 || h.p.files[0].Path != "y.txt" {
		t.Errorf("merged files include base history: %+v", h.p.files)
	}
	h.p.Open(view.TempIntent{Branch: "feat-YT1"})
	h.Until("temporary merged filter", func() bool {
		return h.p.picked() == tmpID && !h.p.aggBusy && h.p.res != nil && h.p.res.Commits == 1
	})
	if h.p.rows[0].BranchFrom != session.FromMerged {
		t.Errorf("temporary filter input=%+v, rows=%+v", h.p.input, h.p.rows)
	}
}

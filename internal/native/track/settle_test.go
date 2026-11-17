package track

import (
	"path/filepath"
	"testing"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

// A requirement cherry-picked into master shows as pending until git has compared the patches; then the left pane is
// read again and the requirement on screen stays.
func TestSettleUpdatesTheListAndKeepsTheSelection(t *testing.T) {
	root := viewtest.Fixture(t)
	r := testutil.NewRepo(t, filepath.Join(root, "repo-c"))
	r.Commit("init", map[string]string{"f": "0\n"})
	main := r.Git("rev-parse", "--abbrev-ref", "HEAD")
	r.Git("checkout", "-q", "-b", "feat-PICK-1001")
	pick := r.Commit("PICK-1001 picked", map[string]string{"pick.txt": "1\n"})
	r.Git("checkout", "-q", main)
	r.Git("cherry-pick", pick)
	r.Git("checkout", "-q", "-b", "feat-OTHR-1001")
	r.Commit("OTHR-1001 other", map[string]string{"other.txt": "1\n"})
	r.Git("checkout", "-q", main)
	for _, b := range []string{main, "feat-PICK-1001", "feat-OTHR-1001"} {
		r.Git("update-ref", "refs/remotes/origin/"+b, b)
	}
	for _, b := range []string{"feat-PICK-1001", "feat-OTHR-1001"} { // a working directory branch named after the number is what finds it
		r.Git("worktree", "add", "-q", filepath.Join(root, "repo-c-"+b), b)
	}
	h := viewtest.Open(t, root)
	viewtest.TrackAll(t, h.S)
	h.DockOf = Dock()
	hr := viewtest.Mount(t, h, "zh-Hans", func(h view.Host) view.View { return New(h) })
	p := hr.V.(*Page)
	stage := func(id string) string {
		for _, q := range p.reqs {
			if q.ID == id {
				return q.Stage
			}
		}
		return ""
	}
	hr.Until("the list", func() bool { return p.reqsOK && stage("OTHR-1001") != "" })
	p.chooseReq(session.Requirement{ID: "OTHR-1001"})
	hr.Until("settled stages", func() bool { return stage("PICK-1001") == session.StageLanded })
	if p.shownReq() != "OTHR-1001" {
		t.Errorf("re-reading the list changed the selection to %q", p.shownReq())
	}
}

func TestSettledRequirementsRefreshSelectedRepoStages(t *testing.T) {
	root := viewtest.Fixture(t)
	r := testutil.NewRepo(t, filepath.Join(root, "repo-c"))
	r.Commit("init", map[string]string{"f": "0\n"})
	r.Git("checkout", "-q", "-b", "feat-PICK-1001")
	pick := r.Commit("PICK-1001 picked", map[string]string{"pick.txt": "1\n"})
	r.Git("checkout", "-q", "master")
	r.Git("cherry-pick", pick)
	for _, branch := range []string{"master", "feat-PICK-1001"} {
		r.Git("update-ref", "refs/remotes/origin/"+branch, branch)
	}
	h := viewtest.Open(t, root)
	if err := h.S.SetMergeFlow([]session.MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.S.Track("PICK-1001"); err != nil {
		t.Fatal(err)
	}
	h.Drain()
	reqs, err := h.S.Requirements(t.Context())
	if err != nil || len(reqs) != 1 || reqs[0].Stage != session.MergeStage(1) {
		t.Fatalf("initial list=%+v err=%v", reqs, err)
	}
	h.St.Ready = false
	h.DockOf = Dock()
	hr := viewtest.Mount(t, h, "zh-Hans", func(h view.Host) view.View { return New(h) })
	h.St.Ready = true
	p := hr.V.(*Page)
	p.gotReqs(reqs, nil, false)
	hr.Until("initial repo stage", func() bool {
		return !p.aggBusy && len(p.rows) == 1 && p.rows[0].Stage == session.MergeStage(1)
	})
	if _, err := h.S.RequirementsSettle(t.Context()); err != nil {
		t.Fatal(err)
	}
	reqs, err = h.S.Requirements(t.Context())
	if err != nil || reqs[0].Stage != session.StageLanded {
		t.Fatalf("settled list=%+v err=%v", reqs, err)
	}
	hr.Until("initial files loaded", func() bool { return !p.filesBusy && p.filesVer > 0 })
	filesSeq, reread := p.filesSeq, p.reread
	p.gotReqs(reqs, nil, true)
	hr.Until("settled repo stage", func() bool {
		return !p.aggBusy && len(p.rows) == 1 && p.rows[0].Stage == session.StageLanded
	})
	if p.shownReq() != "PICK-1001" {
		t.Fatalf("selection changed to %q", p.shownReq())
	}
	if p.filesSeq != filesSeq || p.reread != reread {
		t.Fatal("settling stages must preserve the loaded diffs")
	}
}

package track

import (
	"path/filepath"
	"testing"
	"time"

	"sheaf/internal/native/view"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestRepositoryBaselineOnlyRefreshesProgress(t *testing.T) {
	root := fixture(t)
	repo := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	repo.Git("branch", "release", "master")
	h := openAt(t, root, "zh-Hans")
	h.Until("release pending", func() bool {
		return len(h.p.rows) == 1 && h.p.rows[0].Stage == session.MergeStage(0)
	})
	if err := h.H.S.SetRepoBaselineOnly("repo-a", true); err != nil {
		t.Fatal(err)
	}
	h.Until("baseline pending", func() bool {
		return len(h.p.rows) == 1 && h.p.rows[0].Stage == session.MergeStage(1) &&
			len(h.p.reqs) == 1 && h.p.reqs[0].Stage == session.MergeStage(1)
	})
}

func TestPageFollowsSavedSettings(t *testing.T) {
	h := open(t, "zh-Hans")
	time.Sleep(100 * time.Millisecond) // the saves of open tell the page too
	h.Until("settled", func() bool { return true })
	reread, reqs, input := h.p.reread, h.p.reqSeq, h.p.input

	h.p.Event(view.ConfigChanged{})
	if h.p.reread != reread || h.p.reqSeq != reqs || h.p.input != input {
		t.Fatal("read again with nothing changed")
	}

	// The settings window saves a release branch: the page reads the filter against it, without saving it back.
	if err := h.H.S.SetCompareBase("master"); err != nil {
		t.Fatal(err)
	}
	h.Until("the new base branch", func() bool { return h.p.master == "master" && h.p.input.Base == "master" })
	if inf := h.H.S.Info(); inf.Master != "master" {
		t.Fatalf("saved base %q", inf.Master)
	}

	reqs = h.p.reqSeq
	if err := h.H.S.SetRequirement(`YT\d{1,3}`); err != nil {
		t.Fatal(err)
	}
	h.Until("the requirements read again", func() bool { return h.p.reqSeq > reqs })
}

func TestWorkspaceFlowChangesRefreshStagesAndLabels(t *testing.T) {
	h := open(t, "zh-Hans")
	flow := []session.MergeStep{{Name: "验收", Branch: "missing"}, {Name: "基线", Branch: "feat-YT1"}}
	if err := h.H.S.SetMergeFlow(flow); err != nil {
		t.Fatal(err)
	}
	hasStage := func(stage string) bool {
		for _, req := range h.p.reqs {
			if req.ID == "YT1" {
				return req.Stage == stage
			}
		}
		return false
	}
	h.Until("new baseline applied", func() bool {
		return len(h.p.flow) == 2 && h.p.flow[0].Name == "验收" &&
			hasStage(session.StageLanded) && len(h.p.rows) == 1 && h.p.rows[0].Stage == session.StageLanded
	})
	if err := h.H.S.SetMergeFlow([]session.MergeStep{{Name: "审核", Branch: "missing"}}); err != nil {
		t.Fatal(err)
	}
	h.Until("missing target applied", func() bool {
		return len(h.p.flow) == 1 && h.p.flow[0].Name == "审核" &&
			hasStage(session.StagePending) && len(h.p.rows) == 1 && h.p.rows[0].Stage == session.StagePending &&
			h.TT.HasText("待合 审核")
	})
	if h.p.shownReq() != "YT1" {
		t.Fatalf("flow changes replaced the selected requirement: %s", h.p.shownReq())
	}
}

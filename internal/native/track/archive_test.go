package track

import (
	"testing"

	"sheaf/internal/session"
)

func TestMergedRequirementsWaitForManualArchive(t *testing.T) {
	harness := openLanded(t, "en-US")
	if !harness.TT.HasText("LAND-1002") {
		t.Fatal("merged requirements must remain visible until manually archived")
	}
	harness.p.chooseReq(session.Requirement{ID: "LAND-1002"})
	harness.TT.Frame()
	harness.railRightClick(t, "LAND-1002 landed 2")
	if err := harness.TT.ChooseMenuItem("Archive"); err != nil {
		t.Fatal(err)
	}
	stage := func() string {
		for _, req := range harness.p.reqs {
			if req.ID == "LAND-1002" {
				return req.Stage
			}
		}
		return ""
	}
	harness.Until("archived", func() bool { return stage() == session.StageArchived })
	if harness.p.shownReq() != "LAND-1002" {
		t.Fatalf("archive changed the selection to %s", harness.p.shownReq())
	}
	harness.railRightClick(t, "LAND-1002 landed 2")
	if err := harness.TT.ChooseMenuItem("Restore"); err != nil {
		t.Fatal(err)
	}
	harness.Until("restored", func() bool { return stage() == session.StageLanded })
}

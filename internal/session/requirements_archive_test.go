package session

import (
	"slices"
	"testing"
)

func TestRequirementArchiveKeepsProgress(t *testing.T) {
	s, _ := requirementFixture(t)
	for _, tc := range []struct {
		id       string
		progress string
	}{
		{"PEND-1001", StagePending},
		{"LAND-1001", StageLanded},
	} {
		t.Run(tc.id, func(t *testing.T) {
			if err := s.Track(tc.id); err != nil {
				t.Fatal(err)
			}
			for _, archived := range []bool{false, true, true, false, false} {
				if err := s.ArchiveRequirement(tc.id, archived); err != nil {
					t.Fatal(err)
				}
				tracked := s.Tracked()
				for range 2 {
					reqs, err := s.Requirements(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					req := stageOnly(reqs, tc.id)
					want := tc.progress
					if archived {
						want = StageArchived
					}
					if req.Stage != want || req.Archived != archived || req.Progress != tc.progress {
						t.Fatalf("requirement=%+v want stage=%s archived=%v progress=%s", req, want, archived, tc.progress)
					}
				}
				if !slices.Equal(s.Tracked(), tracked) {
					t.Fatal("query changed the tracked list")
				}
			}
		})
	}
}

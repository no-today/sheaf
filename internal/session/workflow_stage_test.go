package session

import (
	"slices"
	"testing"

	"sheaf/internal/index"
)

func TestStageOrderAndRank(t *testing.T) {
	flow := []MergeStep{{Name: "qa"}, {Name: "prod"}}
	want := []string{StageDev, StageUnknown, MergeStage(0), MergeStage(1), StageLanded, StageArchived}
	if got := StageOrder(flow); !slices.Equal(got, want) {
		t.Fatalf("order=%v want=%v", got, want)
	}
	for _, tc := range []struct {
		stage string
		rank  int
	}{
		{StageDev, 0},
		{StageUnknown, 1},
		{MergeStage(0), 2},
		{MergeStage(1), 3},
		{StageLanded, int(^uint(0)>>1) - 1},
		{StageArchived, int(^uint(0) >> 1)},
	} {
		if got := StageRank(tc.stage); got != tc.rank {
			t.Errorf("rank(%s)=%d want=%d", tc.stage, got, tc.rank)
		}
	}
}

func TestStageWithoutOwnCommits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		deleted bool
		want    string
		noOwn   bool
	}{
		{"branch exists", false, StageLanded, true},
		{"branch deleted", true, StageUnknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{}
			a := aggRepo{r: &index.Repo{}, deleted: tc.deleted}
			s.planStage(&a)
			if a.view.Stage != tc.want || a.noOwn != tc.noOwn {
				t.Errorf("stage=%s noOwn=%v want=%s %v", a.view.Stage, a.noOwn, tc.want, tc.noOwn)
			}
		})
	}
}

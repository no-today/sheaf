package index

import (
	"slices"
	"testing"
)

// dag builds a repo from parent lists; commit 0 is the newest.
func dag(parents [][]int32) *Repo {
	r := &Repo{ParOff: []uint32{0}}
	for i, ps := range parents {
		r.Time = append(r.Time, int64(len(parents)-i))
		r.Par = append(r.Par, ps...)
		r.ParOff = append(r.ParOff, uint32(len(r.Par)))
	}
	return r
}

func TestHolding(t *testing.T) {
	// Newest first: 0 feat-a tip (parent 1), 1 (parent 3), 2 feat-b tip (parent 3), 3 master tip = base (parent 4),
	// 4 root, 5 a side branch off 4 that master does not reach.
	r := dag([][]int32{{1}, {3}, {3}, {4}, {}, {4}})
	tests := []struct {
		name    string
		tips    []int32
		base    int32
		commits []int32
		want    []int
	}{
		{"each branch holds its own", []int32{0, 2, 5}, 3, []int32{0, 1, 2}, []int{2, 1, 0}},
		{"commits in the base count for nobody", []int32{0, 2}, 3, []int32{3, 4}, []int{0, 0}},
		{"no base: everything reachable", []int32{0, 3}, -1, []int32{1, 3, 4}, []int{3, 2}},
		{"the base itself as a tip", []int32{3}, 3, []int32{0, 1}, []int{0}},
		{"no tips", nil, 3, []int32{0}, []int{}},
	}
	for _, tc := range tests {
		if got := r.Holding(tc.tips, tc.base, tc.commits); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

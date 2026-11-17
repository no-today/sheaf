package app

import "testing"

func TestFindDiff(t *testing.T) {
	keys := map[int]string{3: "1\x00{a}", 4: "1\x00{b}", 5: "2\x00{a}"}
	for _, tc := range []struct {
		key  string
		id   int
		want bool
	}{
		{"1\x00{a}", 3, true},
		{"2\x00{a}", 5, true}, // another workspace window: its own
		{"1\x00{c}", 0, false},
		{"", 0, false},
	} {
		if id, ok := findDiff(keys, tc.key); ok != tc.want || id != tc.id {
			t.Errorf("findDiff(%q) = %d, %v; want %d, %v", tc.key, id, ok, tc.id, tc.want)
		}
	}
}

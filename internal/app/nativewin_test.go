package app

import "testing"

func TestLateUpdaterQueuesUntilAttached(t *testing.T) {
	u := &lateUpdater{}
	var got []int
	u.update(func() { got = append(got, 1) })
	u.update(func() { got = append(got, 2) })
	u.attach(func(f func()) { f() })
	u.update(func() { got = append(got, 3) })
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("got %v", got)
	}
}

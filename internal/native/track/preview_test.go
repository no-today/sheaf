package track

import "testing"

func TestThePageDocksOnlyItsRail(t *testing.T) {
	if d := Dock(); !d.Left.Open || d.Right.Open || d.Bottom.Open {
		t.Errorf("dock %+v", d)
	}
}

package shell

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

func TestClampKeepsMainRoom(t *testing.T) {
	for _, tc := range []struct {
		name        string
		w, h        float32
		in          Dock
		left, right float32
		bottom      float32
	}{
		{"fits", 1440, 900, Dock{Left: Side{300, true, ""}, Right: Side{340, true, ""}, Bottom: Side{260, true, ""}}, 300, 340, 260},
		{"the wider dock gives way", 1000, 900, Dock{Left: Side{300, true, ""}, Right: Side{400, true, ""}}, 300, 1000 - theme.Gap - theme.WStrip - 300 - 2*theme.Gap - theme.MinMain, 260 / 2},
		{"never below the minimum", 600, 900, Dock{Left: Side{300, true, ""}, Right: Side{300, true, ""}}, theme.MinDock, theme.MinDock, 260 / 2},
		{"a closed dock takes no room", 1000, 900, Dock{Left: Side{500, true, ""}, Right: Side{600, false, ""}}, 500, 600, 260 / 2},
		{"the bottom leaves the main island its room", 1440, 500, Dock{Bottom: Side{400, true, ""}}, theme.MinDock, theme.MinDock, 500 - theme.HTitleBar - theme.HStatusBar - theme.Gap - theme.MinMain - theme.Gap},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.in
			d.clamp(tc.w, tc.h, true, true, true)
			if d.Left.Size != max(theme.MinDock, tc.left) || d.Right.Size != tc.right || (tc.in.Bottom.Open && d.Bottom.Size != tc.bottom) {
				t.Errorf("left %v right %v bottom %v", d.Left.Size, d.Right.Size, d.Bottom.Size)
			}
		})
	}
}

func TestRailStaysVisibleAndRightStripTogglesItsPane(t *testing.T) {
	d := DefaultDock()
	frame := func(c *ui.Context) {
		d.View(c, Frame{
			Left:  []Pane{{ID: "a", Title: "Alpha", Icon: theme.IconSidebar, Body: func(c *ui.Context) { ui.Text(c, "alpha body") }}},
			Right: []Pane{{ID: "b", Title: "Beta", Icon: theme.IconFiles, Body: func(c *ui.Context) { ui.Text(c, "beta body") }}},
			Main:  Pane{ID: "m", Title: "Main", Body: func(c *ui.Context) { ui.Text(c, "main body") }},
		})
	}
	tt := ui.NewTester(frame, 1400, 900)
	if !tt.HasText("alpha body") || !tt.HasText("beta body") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("Hide"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("alpha body") || tt.HasText("beta body") || !d.Left.Open || d.Right.Open {
		t.Fatal("hide changed the left rail or left the right pane open")
	}
	if err := tt.Click("Beta"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("alpha body") || !tt.HasText("beta body") || !tt.HasText("main body") {
		t.Errorf("texts %q", tt.Texts())
	}
}

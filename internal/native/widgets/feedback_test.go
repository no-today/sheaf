package widgets

import (
	"bytes"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

func TestDelayedSpinner(t *testing.T) {
	for _, tc := range []struct {
		name string
		stop time.Duration
	}{
		{name: "fast", stop: 80 * time.Millisecond},
		{name: "just before delay", stop: 179 * time.Millisecond},
		{name: "slow", stop: 800 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			busy, frames := false, 0
			tt := ui.NewTester(func(c *ui.Context) {
				frames++
				c.SetTheme(theme.Theme())
				ui.Column(c).Fill().Background(theme.Island).Center().Children(func() {
					DelayedSpinner(c, "load", busy).Label("Loading")
				})
			}, 100, 100)
			blank := bytes.Clone(tt.Image().Pix)
			busy = true
			tt.Frame()
			before := frames
			tt.Advance(min(tc.stop, 179*time.Millisecond))
			if frames != before || !bytes.Equal(blank, tt.Image().Pix) || tt.FramePending() {
				t.Fatal("spinner drew during the first 180 ms")
			}
			if tc.stop > 180*time.Millisecond {
				tt.Advance(time.Millisecond)
				if bytes.Equal(blank, tt.Image().Pix) {
					t.Fatal("spinner did not appear after 180 ms")
				}
				if frames != before+1 {
					t.Fatalf("delay woke %d builds, want one", frames-before)
				}
				tt.Advance(tc.stop - 180*time.Millisecond)
				if frames != before+1 {
					t.Fatal("spinner animation rebuilt the view")
				}
			}
			busy = false
			tt.Frame()
			before = frames
			tt.Advance(time.Second)
			if frames != before || !bytes.Equal(blank, tt.Image().Pix) || tt.FramePending() {
				t.Fatal("finished load kept drawing")
			}
			busy = true
			tt.Frame()
			tt.Advance(179 * time.Millisecond)
			if !bytes.Equal(blank, tt.Image().Pix) {
				t.Fatal("new busy state inherited the previous delay")
			}
			tt.Advance(time.Millisecond)
			if bytes.Equal(blank, tt.Image().Pix) {
				t.Fatal("new busy state never showed its spinner")
			}
		})
	}
}

func TestDelayedSpinnerKeyChangesRestartDelay(t *testing.T) {
	key := "first"
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme.Theme())
		ui.Column(c).Fill().Background(theme.Island).Center().Children(func() {
			DelayedSpinner(c, key, true)
		})
	}, 100, 100)
	tt.Advance(179 * time.Millisecond)
	blank := bytes.Clone(tt.Image().Pix)
	key = "second"
	tt.Frame()
	tt.Advance(179 * time.Millisecond)
	if !bytes.Equal(blank, tt.Image().Pix) {
		t.Fatal("new key inherited the previous delay")
	}
	tt.Advance(time.Millisecond)
	if bytes.Equal(blank, tt.Image().Pix) {
		t.Fatal("new key never showed its spinner")
	}
}

func imageHasColor(tt *ui.Tester, color ui.Color) bool {
	r := tt.Image().Bounds()
	return rectHasColor(tt, ui.Rect{W: float32(r.Dx()), H: float32(r.Dy())}, color)
}

func rectHasColor(tt *ui.Tester, r ui.Rect, color ui.Color) bool {
	im := tt.Image()
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := im.RGBAAt(x, y)
			if p.R == color.R && p.G == color.G && p.B == color.B && p.A == 255 {
				return true
			}
		}
	}
	return false
}

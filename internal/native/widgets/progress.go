package widgets

import (
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

// DelayedSpinner reserves no room until work lasts 180 ms. Give independent
// loads their own key; busy=false resets a control that stays mounted.
func DelayedSpinner(c *ui.Context, key any, busy bool) ui.Element {
	root := ui.Box(c.Key(key))
	ready := ui.Local(root, "spinner-ready", func() time.Time { return time.Time{} })
	if !busy {
		*ready = time.Time{}
		return root.Hidden(true)
	}
	if ready.IsZero() {
		*ready = c.Now().Add(180 * time.Millisecond)
	}
	if wait := ready.Sub(c.Now()); wait > 0 {
		c.After(wait)
		return root.Hidden(true)
	}
	return root.Children(func() { ui.Spinner(c).Size(theme.IconM, theme.IconM).TextColor(theme.Text3) })
}

// ProgressStrip returns its segments so callers can describe each one on hover.
func ProgressStrip(c *ui.Context, colors []ui.Color) []ui.Element {
	segments := make([]ui.Element, len(colors))
	if len(colors) == 0 {
		return segments
	}
	width, gap := float32(8), float32(2)
	if len(colors) > 24 {
		gap = min(gap, 120/float32(len(colors)))
		width = (240 - gap*float32(len(colors)-1)) / float32(len(colors))
	}
	ui.Row(c).Gap(gap).AlignItems(ui.Center).Shrink(0).Children(func() {
		for i, color := range colors {
			segments[i] = ui.Box(c.Key(i)).Size(width, 12).Center().Shrink(0).Children(func() {
				ui.Box(c).Size(width, 8).Radius(theme.RSmall).Background(color).Shrink(0)
			})
		}
	})
	return segments
}

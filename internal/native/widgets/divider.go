package widgets

import (
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
)

// Divider is the hairline between two panes, dragged to resize the pane whose size it holds; sign is +1 when dragging
// right (or down) grows it. It reports a drag; the caller keeps *size within bounds.
func Divider(c *ui.Context, size *float32, sign float32, vertical bool) bool {
	// A usable drag target around the hairline without separating the panes.
	h := ui.Box(c).ZIndex(1).Shrink(0)
	margin := -float32(theme.WResizeHit-theme.Gap) / 2
	if vertical {
		h.Height(theme.WResizeHit).Margin(margin, 0).FillWidth().Cursor(ui.CursorResizeNS)
	} else {
		h.Width(theme.WResizeHit).Margin(0, margin).FillHeight().Cursor(ui.CursorResizeEW)
	}
	h.Label(i18n.Of(c).Divider)
	dx, dy, moved := h.Dragged()
	if moved {
		if vertical {
			*size += sign * dy
		} else {
			*size += sign * dx
		}
	}
	color := theme.Line
	if h.Hovered() || h.Pressed() {
		color = theme.TextStrong
	}
	h.Draw(func(p *ui.Painter, r ui.Rect) {
		if vertical {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + (r.H-theme.Gap)/2, W: r.W, H: theme.Gap}, color, 0)
		} else {
			p.Fill(ui.Rect{X: r.X + (r.W-theme.Gap)/2, Y: r.Y, W: theme.Gap, H: r.H}, color, 0)
		}
	})
	return moved
}

// Caret makes a text field's insertion caret plain to see: the strong text colour, whatever accent the theme around the
// field has (a list's accent is its selection grey, on which a caret disappears), and two DIPs wide.
func Caret(e ui.Element) ui.Element {
	return e.CaretColor(theme.TextStrong).CaretWidth(2)
}

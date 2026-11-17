package diffview

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
)

func foldSeparator(c *ui.Context, sep ui.Element, s *editor.DiffSeparator) {
	w := tr.Of(c)
	label := w.Folded(s.Lines)
	sep.Label(label).Padding(4).Children(func() {
		bar := ui.Row(c).Grow(1).AlignSelf(ui.Stretch).AlignItems(ui.Center).Gap(4).PaddingX(8).Radius(theme.RRow)
		if bar.Hovered() {
			bar.Background(theme.DiffFoldHover)
		}
		bar.Children(func() {
			expand := func(fn func()) {
				if c.Modifiers()&ui.Shift != 0 {
					fn = s.ExpandAll
				}
				fn()
				c.Invalidate()
			}
			button := func(icon *ui.SVG, label string, fn func()) {
				if widgets.IconButton(c.Key(label), icon, label).Size(20, 20).TextColor(theme.Text4).KeepFocus().Clicked() {
					expand(fn)
				}
			}
			switch {
			case s.First:
				button(theme.IconArrowUp, w.ExpandUp, s.ExpandUp)
			case s.Last:
				button(theme.IconArrowDown, w.ExpandDown, s.ExpandDown)
			case s.Lines <= s.Step:
				button(theme.IconExpandAll, w.ExpandAll, s.ExpandAll)
			default:
				button(theme.IconArrowDown, w.ExpandDown, s.ExpandDown)
				button(theme.IconArrowUp, w.ExpandUp, s.ExpandUp)
			}
			if widgets.FlatButton(c, w.ExpandAll, func() {
				ui.Text(c, label).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine()
			}).Height(20).PaddingX(4).KeepFocus().Clicked() {
				s.ExpandAll()
				c.Invalidate()
			}
		})
	})
}

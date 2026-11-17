package diffview

import (
	"fmt"
	"path"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
)

// View builds Preview Diff: a header (status, path, change n of m, +/−, file i / n, layout) over the diff of the
// current file. Give the element a size. Its keys work while it or the diff has the keyboard focus (it is focusable
// itself, so a page can focus it); a page that keeps the focus elsewhere, as a file list, calls NextChange and the
// others from its own keys.
func View(c *ui.Context, s *State) ui.Element {
	if s.Theme == nil {
		s.Theme = dockTheme()
	}
	th := s.Theme
	th.Font.Family = theme.CodeFont() // the setting may have changed since the theme was made
	w := tr.Of(c)
	s.viewed = true
	s.settle()
	if s.cur >= 0 {
		svc := c.Services()
		s.fetch(s.cur, svc)
		s.fetch(s.cur+1, svc) // Down at the last change, or Right, shows it at once
	}
	root := ui.Column(c).Fill().Background(th.Background).Focusable().FocusRing(false)
	var showsDiff bool
	root.Children(func() {
		if s.cur >= 0 {
			header(c, s, th, w)
		}
		showsDiff = body(c, s, th, w)
	})
	// The sides' editors have the focus as one steps through files; a file without them (loading, binary, unchanged)
	// passes it to the root, so the keys go on working.
	if s.hadFocus && !showsDiff {
		root.Focus()
	}
	s.hadFocus = root.FocusWithin()
	switch {
	case root.Shortcut(0, ui.KeyDown), root.Shortcut(0, ui.KeyF7):
		s.NextChange()
	case root.Shortcut(0, ui.KeyUp), root.Shortcut(ui.Shift, ui.KeyF7):
		s.PrevChange()
	case root.Shortcut(0, ui.KeyRight):
		s.NextFile()
	case root.Shortcut(0, ui.KeyLeft):
		s.PrevFile()
	default:
		return root
	}
	c.Invalidate()
	return root
}

func header(c *ui.Context, s *State, th *Theme, w *words) {
	ui.Row(c).FillWidth().Height(th.HeaderHeight).Shrink(0).Padding(0, 8, 0, 12).Gap(8).AlignItems(ui.Center).
		FontSize(th.HeaderFont).Children(func() {
		if s.cur < 0 {
			ui.Box(c).Grow(1)
			return
		}
		f := s.files[s.cur]
		ui.Text(c, f.Status).FontSize(theme.FSSmall).FontWeight(600).TextColor(th.statusColor(f.Status)).Shrink(0)
		dir, name := path.Split(f.Path)
		ui.Text(c, name).FontWeight(600).TextColor(th.Text).SingleLine().Shrink(1).MinWidth(40)
		if f.OldPath != "" && f.OldPath != f.Path {
			ui.Text(c, "← "+f.OldPath).FontSize(theme.FSSmall).TextColor(th.TextDim).SingleLine().Shrink(2).MinWidth(0)
		} else if dir != "" {
			ui.Text(c, dir).FontSize(theme.FSSmall).TextColor(th.TextDim).SingleLine().Shrink(2).MinWidth(0)
		}
		ui.Box(c).Grow(1)
		if d := s.Diff(); d != nil {
			add, del := d.Stats()
			if add > 0 || del > 0 {
				ui.Row(c).Gap(4).Shrink(0).Font(th.Font.Family).Children(func() {
					if add > 0 {
						ui.Text(c, fmt.Sprintf("+%d", add)).TextColor(th.Added)
					}
					if del > 0 {
						ui.Text(c, fmt.Sprintf("−%d", del)).TextColor(th.Deleted)
					}
				})
			}
		}
		if !s.ToolsOutside {
			tools(c, s, th, w, true)
		}
	})
}

// Tools is the header's navigation (change n of m, file i / n) and layout switch, for a host that places them itself
// (ToolsOutside).
func Tools(c *ui.Context, s *State) {
	if s.cur < 0 {
		return
	}
	if s.Theme == nil {
		s.Theme = dockTheme()
	}
	ui.Row(c).Gap(8).Shrink(0).AlignItems(ui.Center).Children(func() { tools(c, s, s.Theme, tr.Of(c), false) })
}

// tools draws the header's controls; after says something stands before them, so the file navigation is ruled off.
func tools(c *ui.Context, s *State, th *Theme, w *words, after bool) {
	// The buttons keep their places and grey out where they cannot go: stepping through files would move them otherwise.
	// They step as Up and Down do, past the last change of a file to the next file.
	d := s.Diff()
	n := 0
	if d != nil {
		n = len(d.Hunks())
	}
	if n > 1 || len(s.files) > 1 {
		after = true
		i, inside := -1, false
		if d != nil {
			i, inside = d.CurrentHunk()
		}
		label := ""
		switch {
		case i >= 0 && n > 1:
			label = w.Change(i+1, n)
		case d != nil:
			label = w.Changes(n)
		}
		prev := i
		if inside {
			prev--
		}
		canPrev := d != nil && (prev >= 0 || s.cur > 0)
		canNext := d != nil && (i+1 < n || s.cur < len(s.files)-1)
		ui.Row(c).Gap(4).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).Font(th.Font.Family).TextColor(th.TextMuted).SingleLine()
			if widgets.Disable(widgets.IconButton(c.Key("prev-change"), theme.IconArrowUp, w.PrevChange), !canPrev).KeepFocus().Clicked() && canPrev {
				s.PrevChange()
				c.Invalidate()
			}
			if widgets.Disable(widgets.IconButton(c.Key("next-change"), theme.IconArrowDown, w.NextChange), !canNext).KeepFocus().Clicked() && canNext {
				s.NextChange()
				c.Invalidate()
			}
		})
	}
	if len(s.files) > 1 {
		if after {
			ui.Box(c).Width(1).Height(18).Background(th.Border).Shrink(0)
		}
		first, last := s.cur == 0, s.cur == len(s.files)-1
		if widgets.Disable(widgets.IconButton(c.Key("prev-file"), theme.IconChevLeft, w.PrevFile), first).KeepFocus().Clicked() && !first {
			s.PrevFile()
		}
		ui.Text(c, fmt.Sprintf(w.FileOf, s.cur+1, len(s.files))).Font(th.Font.Family).FontFeatures("tnum").TextColor(th.TextMuted).SingleLine().Shrink(0)
		if widgets.Disable(widgets.IconButton(c.Key("next-file"), theme.IconChevRight, w.NextFile), last).KeepFocus().Clicked() && !last {
			s.NextFile()
		}
	}
	if !s.ForceUnified {
		layout := int(s.layout())
		if widgets.Segmented(c, &layout, w.Layout, w.Split, w.Unified).KeepFocus().Changed() {
			s.pick(editor.DiffLayout(layout))
		}
	}
}

// Empty fills a preview whose host has not selected a file yet.
func Empty(c *ui.Context) ui.Element {
	w := tr.Of(c)
	return widgets.EmptyState(c, theme.IconDiff, w.PickFile, w.PreviewHint, nil)
}

// body reports whether it shows the diff viewer.
func body(c *ui.Context, s *State, th *Theme, w *words) bool {
	note := func(text string, color ui.Color) {
		ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
			ui.Text(c, text).TextColor(color).FontSize(theme.FSSmall)
		})
	}
	if s.cur < 0 {
		Empty(c).FillWidth()
		return false
	}
	e := s.entries[s.cur]
	switch {
	case e == nil || !e.ready:
		note(w.Loading, th.TextMuted)
		return false
	case e.err != nil:
		note(w.Failed(e.err), th.Deleted)
		return false
	case e.binary:
		note(w.Binary, th.TextMuted)
		return false
	case e.diff == nil:
		note(w.Unchanged, th.TextMuted)
		return false
	}
	d := e.diff
	if e.cut {
		ui.Text(c, w.Cut).TextColor(th.TextMuted).FontSize(theme.FSSmall).Padding(4, 12)
	}
	if e.more > 0 {
		ui.Text(c, w.More(e.more)).TextColor(th.TextMuted).FontSize(theme.FSSmall).Padding(4, 12)
	}
	if len(e.bins) > 0 {
		ui.Text(c, w.BinaryParts(e.bins)).TextColor(th.TextMuted).FontSize(theme.FSSmall).Padding(4, 12).Wrap()
	}
	d.RenderSeparator = partSeparator(e.titles, th)
	if len(d.Hunks()) == 0 {
		note(w.Unchanged, th.TextMuted)
		return false
	}
	d.Layout, d.Theme, d.Font, d.LineHeight = s.layout(), th.Editor, th.Font, th.LineHeight
	editor.DiffView(c, d).Grow(1).FillWidth().DrawOver(func(p *ui.Painter, r ui.Rect) { frameChange(p, r, d) })
	// GoToHunk scrolls in the viewer's geometry, which exists once the viewer was built.
	switch e.goTo {
	case goFirst:
		d.GoToHunk(0)
	case goLast:
		d.GoToHunk(len(d.Hunks()) - 1)
	default:
		return true
	}
	e.goTo = goNone
	c.Invalidate()
	return true
}

// frameChange rules off the change the caret is in, above and below with a bar at its left, across both sides, so a
// step to the previous or next change shows which lines it reached when changes sit close together.
func frameChange(p *ui.Painter, r ui.Rect, d *editor.DiffState) {
	top, bottom, ok := changeSpan(d)
	if !ok {
		return
	}
	y0, y1 := max(r.Y, r.Y+float32(top)), min(r.Y+r.H, r.Y+float32(bottom))
	if y1 <= y0 {
		return
	}
	p.Fill(ui.Rect{X: r.X, Y: y0, W: 3, H: y1 - y0}, theme.Mark, 0)
	if top >= 0 {
		p.Fill(ui.Rect{X: r.X, Y: y0, W: r.W, H: 1}, theme.Mark, 0)
	}
	if float32(bottom) <= r.H {
		p.Fill(ui.Rect{X: r.X, Y: y1 - 1, W: r.W, H: 1}, theme.Mark, 0)
	}
}

// changeSpan is where the change the caret is in shows in the viewer, its deleted and added lines together, from the
// viewer's top; false outside a change, and in a file that only adds or only deletes lines, which is all one change.
func changeSpan(d *editor.DiffState) (top, bottom float64, ok bool) {
	i, inside := d.CurrentHunk()
	if !inside || oneSided(d) {
		return 0, 0, false
	}
	h := d.Hunks()[i]
	scroll := d.ScrollY()
	top = min(d.LineTop(editor.DiffOld, h.OldStart), d.LineTop(editor.DiffNew, h.NewStart)) - scroll
	bottom = max(d.LineTop(editor.DiffOld, h.OldEnd), d.LineTop(editor.DiffNew, h.NewEnd)) - scroll
	return top, bottom, true
}

package widgets

import (
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/display"
	lobetheme "github.com/lobehub/lobe-mygo/pkg/theme"

	"sheaf/internal/native/theme"
)

// TextInput is a one-line field styled by theme.Theme at the height of a select. Placeholder and Label
// are the caller's.
func TextInput(c *ui.Context, value *string) ui.Element {
	return fieldLook(ui.TextInput(c, value))
}

// SearchField is TextInput with a search icon, which Escape clears.
func SearchField(c *ui.Context, value *string) ui.Element {
	return fieldLook(ui.SearchField(c, value))
}

// Native fields keep the outer focus target, Escape handling and validation border that callers rely on.
func fieldLook(e ui.Element) ui.Element {
	Caret(e).Height(theme.HSelect).PaddingX(8).Radius(theme.RButton).Background(theme.Field).
		Border(1, theme.Line).FontSize(theme.FSBody).FixedLineHeight(theme.HLine).TextColor(theme.Text)
	if e.Hovered() && !e.Focused() {
		e.Background(theme.StripeOn)
	}
	return e
}

// Select is a drop-down with a hairline trigger and a menu panel below it.
// label names it for assistive technology.
func Select[T comparable](c *ui.Context, value *T, options []T, name func(T) string, label string) ui.Element {
	sel := ui.SelectBase(c, value)
	t := fieldLook(sel.Trigger).PaddingEnd(6).Gap(8).Label(label).Cursor(ui.CursorPointer).
		FocusRingColor(theme.Accent)
	if t.Pressed() || sel.Open() {
		t.Background(theme.StripeOn)
	}
	if t.Focused() || sel.Open() {
		t.BorderColor(theme.Accent)
	}
	t.Children(func() {
		ui.Text(c, name(*value)).SingleLine().Shrink(1).MinWidth(0)
		ui.Icon(c, theme.IconChevDown).FontSize(theme.IconS).TextColor(theme.Text3).Shrink(0)
	})
	sel.Popup(func(panel ui.Element) {
		menuPanel(panel).Margin(4, 0, 0, 0).MinWidth(max(180, t.Bounds().W))
		for _, o := range options {
			item := sel.Item(o).Height(theme.HMenuItem).PaddingX(10).Radius(theme.RButton).Gap(8).
				Cursor(ui.CursorPointer).TextColor(theme.Text)
			if item.Highlighted() {
				selectedRow(item)
			}
			item.Children(func() {
				ui.Text(c, name(o)).SingleLine().Grow(1)
				if o == *value {
					ui.Icon(c, theme.IconCheck).FontSize(theme.IconS).TextColor(theme.AccentTxt)
				}
			})
		}
	})
	return t
}

// Panel is the look of every popover (.menu): the panel colour, the menu ring and its shadow. The caller sets its width,
// its margin from the anchor and, where it differs, its padding.
func Panel(panel ui.Element) ui.Element {
	return popoverMotion(panelLook(panel))
}

func panelLook(panel ui.Element) ui.Element {
	return panel.Padding(4).Radius(theme.RMenu).Background(theme.Panel).Border(1, theme.MenuRing).
		Shadow(0, 10, 28, 0, theme.Shadow).FontSize(theme.FSBody).FixedLineHeight(theme.HLine).TextColor(theme.Text)
}

func menuPanel(panel ui.Element) ui.Element { return Panel(panel).MinWidth(180) }

// Switch keeps the native switch semantics with the theme's square track and thumb.
func Switch(c *ui.Context, on *bool) ui.Element {
	sw := ui.SwitchBase(c, on).Size(32, 18).Padding(3).Radius(theme.RButton).
		Border(1, theme.Line).Background(theme.Field).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).FocusRingColor(theme.Accent).Shrink(0)
	thumb := theme.Text3
	if *on {
		sw.Background(theme.Accent).BorderColor(theme.Accent).Justify(ui.End)
		thumb = theme.OnAccent
	}
	sw.Children(func() {
		ui.Box(c).Size(10, 10).Radius(theme.RInner).Background(thumb)
	})
	return sw
}

// Segmented chooses one of labels into value.
func Segmented(c *ui.Context, value *int, label string, labels ...string) ui.Element {
	seg := ui.SegmentedBase(c, value, len(labels))
	seg.Track.Gap(4).Padding(3).Radius(theme.RMenu).Background(theme.FrameBg).Border(1, theme.Line).Label(label).Children(func() {
		for i, l := range labels {
			s := seg.Segment(i).Height(theme.HSelect - 6).PaddingX(10).Radius(theme.RInner).Center().Label(l).
				Cursor(ui.CursorPointer).FocusRingColor(theme.Accent)
			color := theme.Text3
			switch {
			case i == *value:
				selectedRow(s)
				color = theme.Text
			case s.Pressed():
				s.Background(theme.StripeOn)
				color = theme.Text
			case s.Hovered():
				s.Background(theme.Hover)
				color = theme.Text2
			}
			s.Children(func() { ui.Text(c, l).FontSize(theme.FSSmall).FontWeight(500).TextColor(color).SingleLine() })
		}
	})
	return seg.Track
}

// Badge keeps arbitrary words as well as counts, so it uses a neutral Lobe tag rather than an overlaid count.
func Badge(c *ui.Context, text string) ui.Element {
	return display.Tag(c, text, display.TagProps{Variant: display.TagFilled, Size: lobetheme.Small}).
		Height(18).PaddingX(6).Radius(theme.RInner).Background(theme.Field).Border(1, theme.Line).TextColor(theme.Text2).
		FontSize(theme.FSLabel).FontWeight(500).FontFeatures("tnum")
}

// RefTag is a branch or tag name on a commit: kind is "local", "remote" or "tag" (session.RefView.Kind), told by colour
// alone, since a commit's row may hold several and icons would crowd its subject. It gives
// way before the text beside it (Shrink 100) down to a few letters.
func RefTag(c *ui.Context, name, kind string) ui.Element {
	bg, fg := theme.RefLocalBg, theme.RefLocalTxt
	switch kind {
	case "remote":
		bg, fg = theme.RefRemoteBg, theme.RefRemoteTxt
	case "tag":
		bg, fg = theme.RefTagBg, theme.RefTagTxt
	}
	return ui.Row(c).Height(18).PaddingX(6).Radius(theme.RSmall).Background(bg).Border(1, theme.Line).Justify(ui.Center).Shrink(100).MinWidth(56).MaxWidth(200).Tooltip(name).Children(func() {
		ui.Text(c, name).Font(theme.MonoFont).FontSize(theme.FSLabel).TextColor(fg).SingleLine().Shrink(1).MinWidth(0)
	})
}

// EmptyState fills a pane that has nothing to show: what belongs here, then how to get it. action, if any, draws the way
// there (one button).
func EmptyState(c *ui.Context, icon *ui.SVG, title, hint string, action func()) ui.Element {
	return ui.Column(c).Grow(1).Center().Padding(24).Gap(4).Children(func() {
		// A compact glyph keeps the shared toolbar artwork from growing a heavy stroke in empty panes.
		ui.Icon(c, icon).FontSize(theme.IconXL).Scale(0.75, 0.75).TextColor(theme.Text5).Margin(0, 0, 4, 0)
		ui.Text(c, title).FontWeight(500).TextColor(theme.Text).TextAlign(ui.Center).Wrap()
		if hint != "" {
			ui.Text(c, hint).FontSize(theme.FSSmall).TextColor(theme.Text3).TextAlign(ui.Center).Wrap().MaxWidth(340)
		}
		if action != nil {
			ui.Box(c).Height(4)
			action()
		}
	})
}

// Package shell is the window frame of Sheaf's native UI, independent of what it shows:
// the title bar, resizable dock panes, main area and status bar.
package shell

import (
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
)

// Pane is one island: the main area or a dock panel.
type Pane struct {
	ID    string
	Title string
	Icon  *ui.SVG             // its button on a strip (docks only)
	Tip   string              // the strip button's tooltip; Title when empty
	Head  func(c *ui.Context) // optional controls at the right of the header
	Slot  func(c *ui.Context) // optional: replaces the title in the header (a search box, tabs)
	Body  func(c *ui.Context) // fills the island under the header
	Bare  bool                // no header: Body draws the whole island
}

// Side is how the user arranged one dock region: its size and whether it shows, and which of its panes.
type Side struct {
	Size   float32 `json:"size"` // width (left, right) or height (bottom) in DIPs
	Open   bool    `json:"open"`
	Active string  `json:"active,omitempty"` // ID of the pane shown; "" = the first
}

// Dock is the arrangement of the three dock regions. Keep it in the app's state (and save it) — the frame only reads and
// changes it.
type Dock struct {
	Left   Side `json:"left"`
	Right  Side `json:"right"`
	Bottom Side `json:"bottom"`
	// Splits are the sizes of a view's own dividers inside its panes, as the user dragged them, by an index the view
	// chooses; 0 is the view's default. Kept here so they are saved with the rest of the arrangement.
	Splits [4]float32 `json:"splits"`
	// Folded are the view's own panes the user hid, by the same index as Splits.
	Folded [4]bool `json:"folded"`
}

// DefaultDock is the arrangement of a first launch.
func DefaultDock() Dock {
	return Dock{
		Left:   Side{Size: 300, Open: true},
		Right:  Side{Size: 340, Open: true},
		Bottom: Side{Size: 260, Open: false},
	}
}

// Frame is what the window shows; every part but Main may be empty.
type Frame struct {
	Title  func(c *ui.Context) // the title bar, after the window controls
	Left   []Pane
	Right  []Pane
	Bottom []Pane
	Main   Pane
	Status func(c *ui.Context) // the status bar
}

// View builds the frame, filling the window.
func (d *Dock) View(c *ui.Context, f Frame) {
	w, h := c.Size()
	d.clamp(w, h, len(f.Left) > 0, len(f.Right) > 0, len(f.Bottom) > 0)
	ui.Column(c).Fill().Background(theme.FrameBg).FixedLineHeight(theme.HLine).Children(func() {
		titleBar(c, f.Title)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Padding(0, 0, theme.Gap, theme.Gap).Children(func() {
				if p := pick(f.Left, d.Left); p != nil && d.Left.Open {
					dockPane(c.Key("left"), p, nil).Width(d.Left.Size)
					d.handle(c.Key("left-handle"), &d.Left.Size, 1, false)
				}
				ui.Column(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
					paneBody(c.Key("main"), &f.Main, nil).Background(theme.Island).Radius(theme.RIsland).Grow(1).MinHeight(0)
					if p := pick(f.Bottom, d.Bottom); p != nil && d.Bottom.Open {
						d.handle(c.Key("bottom-handle"), &d.Bottom.Size, -1, true)
						dockPane(c.Key("bottom"), p, func() { d.Bottom.Open = false }).Height(d.Bottom.Size)
					}
				})
				if p := pick(f.Right, d.Right); p != nil && d.Right.Open {
					d.handle(c.Key("right-handle"), &d.Right.Size, -1, false)
					dockPane(c.Key("right"), p, func() { d.Right.Open = false }).Width(d.Right.Size)
				}
			})
			d.rightStrip(c, f)
		})
		statusBar(c, f.Status)
	})
}

// LeftToggle is the status bar's button that shows or hides the left pane, at the bottom-left corner below it; ⌘B
// does the same.
func (d *Dock) LeftToggle(c *ui.Context) {
	w := i18n.Of(c)
	label := w.HideLeft
	if !d.Left.Open {
		label = w.ShowLeft
	}
	if PaneToggle(c.Key("left-toggle"), theme.IconSidebar, label+"  ⌘B", d.Left.Open).Clicked() {
		d.Left.Open = !d.Left.Open
	}
	widgets.Shortcuts(c, widgets.Shortcut{Mods: ui.Cmd, Key: ui.KeyB, Do: func() { d.Left.Open = !d.Left.Open }})
}

// PaneToggle is a plain icon button of the status bar that shows or hides a pane, brighter while the pane shows.
func PaneToggle(c *ui.Context, icon *ui.SVG, label string, on bool) ui.Element {
	b := ui.ButtonBase(c).Size(22, 22).Radius(theme.RButton).Center().Shrink(0).Cursor(ui.CursorPointer).Label(label).Tooltip(label)
	color := theme.Text4
	switch {
	case b.Hovered() || b.Pressed():
		b.Background(theme.Hover)
		color = theme.Text
	case on:
		color = theme.Text3
	}
	b.Transition(widgets.FadeColors)
	b.Children(func() { ui.Icon(c, icon).FontSize(theme.IconM).TextColor(color) })
	return b
}

// clamp keeps the docks within the window: each at least theme.MinDock, and the main island at least theme.MinMain beside and
// above them. A side that does not fit gives way without forgetting nothing but the overflow.
func (d *Dock) clamp(w, h float32, left, right, bottom bool) {
	inner := w - theme.Gap - theme.WStrip
	if !right {
		inner = w - 2*theme.Gap
	}
	var used float32
	if left && d.Left.Open {
		used += d.Left.Size + theme.Gap
	}
	if right && d.Right.Open {
		used += d.Right.Size + theme.Gap
	}
	over := used + theme.MinMain - inner
	// The wider dock gives way first, down to theme.MinDock; the other takes what is left over.
	first, second := &d.Right, &d.Left
	if !right || !d.Right.Open || (left && d.Left.Open && d.Left.Size > d.Right.Size) {
		first, second = second, first
	}
	for _, s := range []*Side{first, second} {
		if over <= 0 || !s.Open || (s == &d.Left && !left) || (s == &d.Right && !right) {
			continue
		}
		give := min(over, max(0, s.Size-theme.MinDock))
		s.Size -= give
		over -= give
	}
	d.Left.Size = max(theme.MinDock, d.Left.Size)
	d.Right.Size = max(theme.MinDock, d.Right.Size)
	tall := h - theme.HTitleBar - theme.HStatusBar - theme.Gap
	d.Bottom.Size = min(max(theme.MinDock/2, d.Bottom.Size), max(theme.MinDock/2, tall-theme.MinMain-theme.Gap))
}

// handle is the hairline between panes, dragged to resize the dock whose size it holds; sign is +1 when dragging right
// (or down) grows it.
func (d *Dock) handle(c *ui.Context, size *float32, sign float32, vertical bool) {
	if widgets.Divider(c, size, sign, vertical) {
		*size = max(theme.MinDock/2, *size)
	}
}

// pick is the pane a side shows.
func pick(panes []Pane, s Side) *Pane {
	for i := range panes {
		if panes[i].ID == s.Active {
			return &panes[i]
		}
	}
	if len(panes) > 0 {
		return &panes[0]
	}
	return nil
}

// toggle is a strip button of a dock pane: it shows the pane, or hides its side when the pane already shows.
func toggle(s *Side, id string) {
	if s.Open && s.Active == id {
		s.Open = false
		return
	}
	s.Open, s.Active = true, id
}

// dockPane uses the sidebar surface; the main content shares the window background.
func dockPane(c *ui.Context, p *Pane, hide func()) ui.Element {
	return paneBody(c, p, hide).Background(theme.Rail)
}

func paneBody(c *ui.Context, p *Pane, hide func()) ui.Element {
	return ui.Column(c).Clip().MinWidth(0).AlignItems(ui.Stretch).Children(func() {
		if !p.Bare {
			PaneHead(c, p, hide)
		}
		ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if p.Body != nil {
				p.Body(c)
			}
		})
	})
}

// PaneHead is an island's header: the title (or the pane's own Slot), its controls, and a hide button for docks.
func PaneHead(c *ui.Context, p *Pane, hide func()) {
	ui.Row(c).Height(theme.HPaneHead).PaddingStart(12).PaddingEnd(6).Gap(2).Border(1, theme.Line).BorderWidth(0, 0, 1, 0).Children(func() {
		if p.Slot != nil {
			ui.Row(c).Grow(1).MinWidth(0).Children(func() { p.Slot(c) })
		} else {
			ui.Text(c, p.Title).FontSize(theme.FSLabel).FontWeight(600).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0)
			ui.Spacer(c)
		}
		if p.Head != nil {
			p.Head(c)
		}
		if hide != nil {
			if widgets.IconButton(c, theme.IconHide, i18n.Of(c).Hide).Clicked() {
				hide()
			}
		}
	})
}

func titleBar(c *ui.Context, fill func(*ui.Context)) {
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, theme.HTitleBar)).PaddingStart(max(bar.Left, 74)+6).PaddingEnd(10).Gap(6).Border(1, theme.Line).BorderWidth(0, 0, 1, 0).DragWindow().Children(func() {
		if fill != nil {
			fill(c)
		}
	})
}

func statusBar(c *ui.Context, fill func(*ui.Context)) {
	ui.Row(c).Height(theme.HStatusBar).PaddingStart(8).PaddingEnd(10).Gap(2).ClipX().Border(1, theme.Line).BorderWidth(1, 0, 0, 0).FontSize(theme.FSLabel).TextColor(theme.Text4).Children(func() {
		if fill != nil {
			fill(c)
		}
	})
}

func stripButton(c *ui.Context, icon *ui.SVG, label string, on bool) ui.Element {
	b := ui.ButtonBase(c).Size(30, 30).Radius(theme.RMenu).Center().Label(label).Tooltip(label)
	color := theme.Text3
	switch {
	case on, b.Pressed():
		b.Background(theme.StripeOn)
		color = theme.Text
	case b.Hovered():
		b.Background(theme.Hover)
		color = theme.Text
	}
	if on {
		widgets.RowLook(b, true)
	}
	b.Transition(widgets.FadeColors)
	b.Children(func() { ui.Icon(c, icon).FontSize(theme.IconL).TextColor(color) })
	return b
}

func (d *Dock) rightStrip(c *ui.Context, f Frame) {
	if len(f.Right) == 0 {
		ui.Box(c).Width(theme.Gap) // keep the islands off the window edge
		return
	}
	ui.Column(c).Width(theme.WStrip).Background(theme.Rail).Border(1, theme.Line).BorderWidth(0, 0, 0, 1).AlignItems(ui.Center).Gap(4).Padding(2, 0, 0, 0).Children(func() {
		d.paneButtons(c, f.Right, &d.Right)
	})
}

func (d *Dock) paneButtons(c *ui.Context, panes []Pane, s *Side) {
	shown := pick(panes, *s)
	for i := range panes {
		p := &panes[i]
		tip := p.Tip
		if tip == "" {
			tip = p.Title
		}
		if stripButton(c.Key("pane:"+p.ID), p.Icon, tip, s.Open && shown == p).Clicked() {
			toggle(s, p.ID)
		}
	}
}

// Window is the frame of a window that is not a workspace window (start, settings, diff, file history): the title bar
// with the title and tools (optional, at its right), over one island that body fills, over a status bar when status is
// given.
func Window(c *ui.Context, title string, tools, body, status func(c *ui.Context)) {
	WindowWith(c, func(c *ui.Context) {
		ui.Text(c, title).FontWeight(600).SingleLine().Shrink(1).MinWidth(0).PaddingX(8)
	}, tools, body, status)
}

// WindowWith is Window with a title that draws itself, as one with parts in different weights.
func WindowWith(c *ui.Context, title, tools, body, status func(c *ui.Context)) {
	ui.Column(c).Fill().Background(theme.FrameBg).FixedLineHeight(theme.HLine).Children(func() {
		titleBar(c, func(c *ui.Context) {
			title(c)
			ui.Spacer(c)
			if tools != nil {
				tools(c)
			}
		})
		ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Padding(0, theme.Gap, theme.Gap, theme.Gap).Children(func() {
			ui.Column(c).Grow(1).MinHeight(0).Background(theme.Island).Radius(theme.RIsland).Clip().
				AlignItems(ui.Stretch).Children(func() {
				if body != nil {
					body(c)
				}
			})
		})
		if status != nil {
			statusBar(c, status)
		}
	})
}

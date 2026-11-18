// Package widgets contains the controls and helpers shared by native pages:
// menus, the toast with Undo, confirm and input popovers, and shortcuts.
// Labels come from the caller, already translated (package i18n).
package widgets

import (
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
)

// FadeColors eases a colour change, for the few controls big enough to flash: solid buttons and the icon strips. Rows
// and small buttons change at once: nothing is gained, and each fade keeps the window drawing.
var FadeColors = ui.ElementTransition{Colors: true, Duration: 100 * time.Millisecond}

// Button is a secondary action with a hairline outline.
func Button(c *ui.Context, label string) ui.Element {
	b := ui.ButtonBase(c).Height(theme.HSelect).PaddingX(8).Gap(6).Radius(theme.RButton).
		FontSize(theme.FSBody).FixedLineHeight(theme.HLine).FontWeight(500).
		Background(ui.Transparent).Border(1, theme.Line).TextColor(theme.Text).
		Cursor(ui.CursorPointer).Label(label).Transition(FadeColors)
	face(b)
	b.Children(func() { ui.Text(c, label).SingleLine() })
	return b
}

// face is how a button or a row without a colour of its own answers the pointer. A disabled one reports neither.
func face(b ui.Element) {
	switch {
	case b.Pressed():
		b.Background(theme.StripeOn)
	case b.Hovered():
		b.Background(theme.Hover)
	}
}

// PrimaryButton is the one main action of a popover or a window.
func PrimaryButton(c *ui.Context, label string) ui.Element {
	return tinted(Button(c, label), theme.Accent, theme.AccentHov, theme.AccentDown, theme.OnAccent)
}

// DangerButton marks an action that cannot be undone.
func DangerButton(c *ui.Context, label string) ui.Element {
	return Button(c, label).TextColor(theme.DangerTxt)
}

func tinted(b ui.Element, rest, hover, down, text ui.Color) ui.Element {
	color := rest
	switch {
	case b.Pressed():
		color = down
	case b.Hovered():
		color = hover
	}
	return b.Background(color).Border(1, color).TextColor(text)
}

// FlatButton builds a toolbar action's contents inside its outline; label names it for assistive technology.
func FlatButton(c *ui.Context, label string, content func()) ui.Element {
	b := ui.ButtonBase(c).Height(theme.HSelect).PaddingX(8).Gap(5).Radius(theme.RButton).Border(1, theme.Line).Label(label)
	face(b)
	b.Children(content)
	return b
}

func CopyError(c *ui.Context, text string) {
	if text == "" {
		return
	}
	button := iconButton(c, i18n.Of(c).CopyError).Border(0, ui.Transparent).TextColor(theme.Text3)
	button.Children(func() { ui.Icon(c, theme.IconCopy).FontSize(theme.IconS) })
	if button.Clicked() {
		c.WriteClipboard(text)
	}
}

// IconButton is a 24×24 button showing only an icon (.icon-btn), with label as its tooltip and accessible name.
func IconButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	b := iconButton(c, label)
	color := theme.Text3
	if b.Hovered() || b.Pressed() {
		color = theme.Text
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(theme.IconM).TextColor(color) })
	return b
}

func iconButton(c *ui.Context, label string) ui.Element {
	b := ui.ButtonBase(c).Size(theme.HButton, theme.HButton).Padding(0).Radius(theme.RButton).Border(1, theme.Line).Center().Shrink(0).
		Cursor(ui.CursorPointer).FocusRingColor(theme.Accent).Label(label).Tooltip(label)
	face(b)
	return b
}

// RowLook is the background of a row of a list or a tree on an island: Selection when chosen, else as a flat button.
func RowLook(row ui.Element, selected bool) ui.Element {
	if selected {
		return selectedRow(row)
	}
	face(row)
	return row
}

// PanelRowLook is RowLook on a popover, whose Panel colour hides theme.Hover.
func PanelRowLook(row ui.Element, selected bool) ui.Element {
	switch {
	case selected:
		selectedRow(row)
	case row.Hovered():
		row.Background(theme.PanelHover)
	}
	return row
}

func selectedRow(row ui.Element) ui.Element {
	return row.Background(theme.Selection).DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: theme.WSelection, H: r.H}, theme.TextStrong, 0)
	})
}

// Disable greys e out when off, which then takes neither clicks nor the focus.
func Disable(e ui.Element, off bool) ui.Element {
	if off {
		e.Disabled(true).Opacity(0.45)
	}
	return e
}

package widgets

import (
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

// ContextMenu gives e a right-click menu (Control-click, the menu key, Shift+F10), drawn in the window in the theme's
// look rather than the system's, which looks out of place beside the islands. build runs as the menu opens and again in
// the frame after an item was chosen, where Item(...).Chosen() reports it. In a loop that builds rows, note the choice
// and act after the loop when it changes the rows (see mygo's views.md).
func ContextMenu(c *ui.Context, e ui.Element, build func(m *ui.Menu)) ui.Element {
	return inMenuTheme(c, func() ui.Element { return e.PopoverMenu(build) })
}

// MenuButton gives a button the same menu, below it as it is clicked (a ⋯ button, the title bar's scope button).
func MenuButton(c *ui.Context, e ui.Element, build func(m *ui.Menu)) ui.Element {
	return inMenuTheme(c, func() ui.Element { return e.PopoverMenuButton(build) })
}

// Menu anchors share an elevated surface even when they belong to a list or toolbar.
var menuTheme = func() *ui.Theme {
	t := theme.ListTheme()
	t.Background = theme.Panel
	t.Border = theme.MenuRing
	t.SurfaceHover = theme.PanelHover
	return t
}()

func inMenuTheme(c *ui.Context, attach func() ui.Element) ui.Element {
	previous := c.Theme()
	c.SetTheme(menuTheme)
	defer c.SetTheme(previous)
	return attach()
}

// Confirm is what ConfirmPopover asks.
type Confirm struct {
	Title   string
	Detail  string // optional, under the title
	OK      string // the confirming button's label
	Cancel  string
	Danger  bool // the action cannot be undone: the button is red and Enter does not press it
	OnOK    func()
	OnClose func() // optional: it closed without OK (Cancel, Escape, a press outside)
}

// ConfirmPopover asks q below anchor while *open. An undoable action runs at once and offers Undo instead.
func ConfirmPopover(c *ui.Context, anchor ui.Element, open *bool, q Confirm) {
	was := *open
	ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		menuPanel(panel).Margin(6, 0, 0, 0).Padding(16).Gap(12).MaxWidth(360).Label(q.Title).Description(q.Detail).Children(func() {
			ui.Text(c, q.Title).FontSize(theme.FSTitle).FontWeight(600).Wrap()
			if q.Detail != "" {
				ui.Text(c, q.Detail).FontSize(theme.FSSmall).TextColor(theme.Text2).Wrap()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if Button(c, q.Cancel).Clicked() {
					*open = false
				}
				var ok ui.Element
				if q.Danger {
					ok = DangerButton(c, q.OK)
				} else {
					ok = PrimaryButton(c, q.OK).AutoFocus()
				}
				if ok.Clicked() || (!q.Danger && c.Shortcut(0, ui.KeyEnter)) {
					*open = false
					if q.OnOK != nil {
						q.OnOK()
					}
					was = false // closed by OK: no OnClose
				}
			})
		})
	})
	if was && !*open && q.OnClose != nil {
		q.OnClose()
	}
}

// Prompt is what PromptPopover asks.
type Prompt struct {
	Title       string
	Placeholder string
	OK          string
	Cancel      string
	// Check returns why value cannot be taken ("" when it can), shown under the field; nil takes anything not blank.
	Check func(value string) string
	// OnOK gets the value taken.
	OnOK func(value string)
}

// PromptPopover asks for one line of text below anchor while *open (a filter's name, a new branch). value is the
// field's text, kept by the caller: set it before opening.
func PromptPopover(c *ui.Context, anchor ui.Element, open *bool, value *string, p Prompt) {
	ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		menuPanel(panel).Margin(6, 0, 0, 0).Padding(16).Gap(12).Width(320).Label(p.Title).Children(func() {
			ui.Text(c, p.Title).FontSize(theme.FSTitle).FontWeight(600).Wrap()
			why := ""
			if p.Check != nil {
				why = p.Check(*value)
			}
			blank := len(*value) == 0
			in := TextInput(c.Key("prompt"), value).Placeholder(p.Placeholder).Label(p.Title).AutoFocus()
			if why != "" {
				in.Error(why)
				ui.Text(c, why).FontSize(theme.FSLabel).TextColor(theme.DangerTxt).Wrap()
			}
			submit := in.Submitted()
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if Button(c, p.Cancel).Clicked() {
					*open = false
				}
				ok := Disable(PrimaryButton(c, p.OK), blank || why != "")
				if (ok.Clicked() || submit) && !blank && why == "" {
					*open = false
					if p.OnOK != nil {
						p.OnOK(*value)
					}
				}
			})
		})
	})
}

// Shortcut is one key of a view.
type Shortcut struct {
	Mods ui.Modifiers // ui.Cmd is ⌘ on macOS
	Key  ui.Key
	Do   func()
}

// Shortcuts runs the action of each shortcut pressed in this frame anywhere in the window. Call it while building the
// view that owns them: a view that is hidden builds nothing, so its keys go to the view shown.
// A focused text input keeps its editing keys; use a modifier for keys that would type.
func Shortcuts(c *ui.Context, keys ...Shortcut) {
	for _, k := range keys {
		if c.Shortcut(k.Mods, k.Key) && k.Do != nil {
			k.Do()
		}
	}
}

package widgets

import (
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
)

// UndoTime is how long an undoable action can be reverted through its toast.
const UndoTime = 5 * time.Second

// toastID makes every toast replace the one showing: one toast at a time.
const toastID = "sheaf"

// Toaster queues toasts from anywhere on the main thread (a click, a function given to update after background work)
// and shows them in the window's next frame. The window owns one and calls View once per frame.
type Toaster struct {
	pending []ui.Toast
	clear   bool
	// Details opens the task list; the window that has one sets it, and TaskError offers it.
	Details func()
}

// Show says text for a few seconds.
func (t *Toaster) Show(text string) {
	t.pending = append(t.pending, ui.Toast{ID: toastID, Title: text, Timeout: UndoTime})
}

// Error says what went wrong (a git command's stderr, say) until the user closes it.
func (t *Toaster) Error(text string) {
	t.pending = append(t.pending, ui.Toast{ID: toastID, Title: text, Type: "error", Timeout: -1})
}

// TaskError is Error for a task that failed: its Details button opens the task list, which has every row's outcome.
func (t *Toaster) TaskError(text string) {
	toast := ui.Toast{ID: toastID, Title: text, Type: "error", Timeout: -1}
	if t.Details != nil {
		toast.Action, toast.OnAction = detailsLabel, t.Details
	}
	t.pending = append(t.pending, toast)
}

// detailsLabel marks a toast's Details button until View puts it into words.
const detailsLabel = "\x00details"

// Undo says text with an Undo button for UndoTime: the operation is already done, undo reverts it. label replaces
// "Undo" when the button does something else of the kind ("Restore"); "" keeps Undo.
func (t *Toaster) Undo(text string, undo func(), label string) {
	t.pending = append(t.pending, ui.Toast{ID: toastID, Title: text, Timeout: UndoTime, Action: label, OnAction: undo})
}

// Clear closes the toast showing.
func (t *Toaster) Clear() { t.pending, t.clear = nil, true }

// View shows the queued toasts and draws them: at the bottom middle of the window, in the panel colour (.toast). Call it
// once per frame, last, so the toasts are above the content.
func (t *Toaster) View(c *ui.Context) {
	if t.clear {
		c.CloseToast("")
		t.clear = false
	}
	undo := i18n.Of(c).Undo
	for _, p := range t.pending {
		switch {
		case p.Action == detailsLabel:
			p.Action = i18n.Of(c).Details
		case p.OnAction != nil && p.Action == "":
			p.Action = undo
		}
		c.AddToast(p)
	}
	t.pending = nil
	width, _ := c.Size()
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(20).AlignItems(ui.Center).Gap(8)
		for _, toast := range toasts {
			parts := ui.ToastBase(c, toast)
			fg := theme.Text
			if toast.Type == "error" {
				fg = theme.ErrText
			}
			parts.Root.Row().AlignItems(ui.Center).Gap(12).Padding(12, 16).Radius(theme.RDialog).Background(theme.Panel).Border(1, theme.MenuRing).
				Shadow(0, 8, 24, 0, theme.Shadow).FontSize(theme.FSSmall).MaxWidth(min(640, max(0, width-40)))
			parts.Root.Transition(ui.ElementTransition{Duration: FadeColors.Duration, Colors: true, Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{}})
			parts.Root.Children(func() {
				ui.Text(c, toast.Title).TextColor(fg).Wrap().Grow(1).Shrink(1).MinWidth(0)
				if toast.Action != "" {
					b := parts.ActionButton().Height(theme.HSelect).PaddingX(12).Radius(theme.RButton).FontSize(theme.FSSmall).
						FontWeight(500).Shrink(0).Label(toast.Action).Transition(FadeColors)
					tinted(b, theme.Accent, theme.AccentHov, theme.AccentDown, theme.OnAccent).
						Children(func() { ui.Text(c, toast.Action).SingleLine() })
				}
				if toast.Type == "error" {
					CopyError(c.Key("copy-error"), toast.Title)
					b := parts.CloseButton().Size(20, 20).Center().Radius(theme.RButton).Shrink(0).
						Label(i18n.Of(c).Close).Tooltip(i18n.Of(c).Close).Transition(FadeColors)
					if b.Hovered() {
						b.Background(theme.PanelHover)
					}
					b.Children(func() { ui.Icon(c, theme.IconClose).FontSize(theme.IconS).TextColor(theme.Text3) })
				}
			})
		}
	})
}

// Package view is the contract between the workspace window (package native) and its requirement page:
// what a view gives the frame, what the window gives a view, and the intents views hand each other through Host.Go.
// Views import this package, never each other (archtest); the window registers them (native/views.go).
// A view is made when first shown and kept while hidden.
package view

import (
	"context"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/shell"
	"sheaf/internal/native/widgets"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

// ID names a view; it is also the key of its saved layout.
type ID string

// The views of a workspace window.
const (
	Log   ID = "log"
	Track ID = "track" // the filter page (筛选器)
)

// View is one page of the workspace window. Every method runs on the main thread.
type View interface {
	// Panes fills the workspace frame. The workspace keeps the dock width per workspace.
	Panes(c *ui.Context) Panes
	// Open hands the view an intent of Host.Go (one of the types below, or nil to just show it). It runs after the
	// view was made, before its first Panes.
	Open(intent any)
	// Event tells the view of the session's news, shown or hidden.
	Event(e Event)
	// Close stops the view's work: the window closed. Nothing runs through Host.Update after it.
	Close()
}

// Panes is what a view puts in the frame; every part but Main may be empty.
type Panes struct {
	Title  func(c *ui.Context) // the title bar after the window controls: what is on screen, then the view's actions
	Left   []shell.Pane
	Right  []shell.Pane
	Bottom []shell.Pane
	Main   shell.Pane
	Status func(c *ui.Context) // the status bar
	// StatusEnd is the status bar's far right, after the task corner: the button of a pane at the window's right edge.
	StatusEnd func(c *ui.Context)
}

// Host is what the workspace window gives a view: one per view.
type Host interface {
	Session() *session.Session
	// Info is the workspace as last read; the window reads it again on index changes.
	Info() session.Info
	State() session.State
	// Update runs f on the main thread and draws a frame: background work hands its results over with it. After Close it
	// does nothing.
	Update(f func())
	// Context ends when the window closes; Caller marks the window's git calls (session.Caller), so a newer call of a
	// kind cancels the older. Take it when asking for the call, not on the goroutine that makes it: each one is stamped
	// newer than the last (session.Asked).
	Context() context.Context
	Caller() context.Context
	// Go shows view id and hands it intent (nil: just show it).
	Go(id ID, intent any)
	// Toasts shows messages and the 5-second Undo.
	Toasts() *widgets.Toaster
	// Windows opens the workspace's other windows.
	Windows() Windows
	// Dock is the view's arrangement, saved with the window's layout: a view keeps the sizes of its own dividers in Splits.
	Dock() *shell.Dock
	// Painted tells that the view's content reached the screen once (the screenshot smoke waits for it).
	Painted()
}

// Windows opens the windows that belong to the workspace window; each closes with it. Asking again raises the one open.
type Windows interface {
	OpenDiff(src session.DiffSource) error
	OpenSettings(tab string) error // "" = the first page
}

// The roles of Windows.OpenIn (app.RoleIDE, app.RoleEditor, app.RoleTerminal).
const (
	OpenIDE      = "ide"
	OpenEditor   = "editor"
	OpenTerminal = "terminal"
)

// Event is news of the session for every view made.
type Event interface{ event() }

// StateChanged: the index state changed (progress comes by the dozen; compare Was and Now).
type StateChanged struct{ Was, Now session.State }

// IndexChanged: the index has new content; the window has read Info again.
type IndexChanged struct{ Update session.IndexUpdate }

// WorktreesChanged: the uncommitted state of the worktrees was read again (a refresh of the dirty marks).
type WorktreesChanged struct{ Status session.WorktreeStatus }

// ConfigChanged: the workspace settings were saved (here or in the settings window); the window has read Info again.
type ConfigChanged struct{}

type VisibilityChanged struct{ Visible bool }

func (VisibilityChanged) event() {}
func (WorktreesChanged) event()  {}
func (StateChanged) event()      {}
func (IndexChanged) event()      {}
func (ConfigChanged) event()     {}

// A view ignores intents it does not take.

// LogIntent opens the log on Cond, with Select picked when it is in the result.
type LogIntent struct {
	Cond   query.Conditions
	Select *session.CommitKey
}

// UncommittedIntent opens working directory changes within the requirement page.
type UncommittedIntent struct{}

// RevealIntent shows one commit in the log, whatever the conditions.
type RevealIntent struct{ Reveal session.CommitKey }

// TempIntent opens a branch on the filter page as an unsaved filter.
type TempIntent struct{ Branch string }

// Smoke is the screenshot scenario: a requirement number or branch and a repo row to scroll into view.
type Smoke struct {
	Q   string
	Row int
}

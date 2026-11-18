// Package native is the workspace window drawn with mygo's native UI toolkit. It holds the title bar,
// index state, toast, saved dock layout and registered requirement view.
// Like app it only talks to session: it never runs git itself.
package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/shell"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/view"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

// winWords are the window's own words (the words of its toasts).
type winWords struct {
	// ReposFailed names the repos the last update could not read; ReposFailedN counts them ("{n} 个仓库" | "{n} repository|{n} repositories").
	ReposFailed, ReposFailedN string
	// ConfigBackedUp says a broken workspace.json was moved aside, naming the backup file.
	ConfigBackedUp string
}

var winTr = i18n.New(winWords{
	ReposFailed:    "%s读取失败：%s",
	ReposFailedN:   "{n} 个仓库",
	ConfigBackedUp: "工作区配置无法读取，已备份为 %s",
}, winWords{
	ReposFailed:    "%s failed to read: %s",
	ReposFailedN:   "{n} repository|{n} repositories",
	ConfigBackedUp: "Workspace config could not be read; saved as %s",
})

// Workspace is the state of one workspace window. Every field is read and written on the main thread (the view, and
// the functions given to update); background work hands its results over through update.
type Workspace struct {
	s      *session.Session
	update func(func()) // runs a function on the main thread and draws a frame (mygo.Window.Update)
	wins   view.Windows

	info  session.Info
	state session.State
	tasks []session.Task // as Tasks last read them

	taskStrip taskStrip

	active view.ID
	hidden bool
	views  map[view.ID]*mounted // the views made so far
	layout *layout
	toasts widgets.Toaster

	// toastTexts are the window's toasts waiting for a frame: their language is chosen when one shows.
	// failedRepos names the repos of the last failure toast.
	toastTexts  []func(*winWords) string
	failedRepos []string

	infoSeq int             // the newest Info read; older answers are dropped
	ctx     context.Context // the window's life: cancelled by Close
	cancel  context.CancelFunc
	caller  context.Context // marks this window's git calls: a newer one of a kind cancels the older (session.Caller)
	asked   atomic.Uint64   // stamps each Caller in the order it was asked for (session.Asked)

	smoke   smoke         // the screenshot smoke's scenario (SHEAF_NATIVE_*)
	timing  bool          // SHEAF_TIMING=1: log when the first rows are painted
	painted chan struct{} // closed when a view's content is first painted
}

// mounted is a view that was made, with the Host it was given.
type mounted struct {
	v view.View
	h *host
}

// New makes the window's state; Attach gives it its session once the session is open. wins opens the workspace's other
// windows (diff and settings).
func New(update func(func()), wins view.Windows) *Workspace {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Workspace{ctx: ctx, cancel: cancel, wins: wins,
		update: func(f func()) { // nothing runs for a closed window
			if ctx.Err() == nil {
				update(f)
			}
		}, views: map[view.ID]*mounted{}, smoke: smokeFromEnv(os.Getenv),
		timing: os.Getenv("SHEAF_TIMING") == "1", painted: make(chan struct{})}
	w.caller = session.Caller(ctx, fmt.Sprintf("native-%p", w))
	return w
}

// Close stops the window's work: every view's, the layout's pending save (written now), and every update still to
// come. Call on the main thread.
func (w *Workspace) Close() {
	w.cancel()
	for _, m := range w.views {
		m.v.Close()
	}
	if w.layout != nil {
		w.layout.flush()
	}
}

// Attach starts showing s: the saved layout, then the first view (or the smoke's). Call on the main thread.
func (w *Workspace) Attach(s *session.Session) {
	w.s = s
	w.info = s.Info()
	w.state = w.info.State
	w.failedRepos = failedNames(w.state.Failed)
	if b := w.info.ConfigBackup; b != "" {
		name := filepath.Base(b)
		w.toastTexts = append(w.toastTexts, func(t *winWords) string { return fmt.Sprintf(t.ConfigBackedUp, name) })
	}
	w.layout = openLayout(s.DataDir())
	w.toasts.Details = func() { w.taskStrip.open = true }
	w.tasks = s.Tasks()
	w.Go(registry[0].id, nil)
	if w.smoke.on {
		id := w.smoke.view
		if def(id) == nil {
			id = registry[0].id
		}
		w.Go(id, w.smoke.scenario)
	}
}

// State is session.Options.OnState: the index state changed (any goroutine).
func (w *Workspace) State(st session.State) {
	w.update(func() {
		was := w.state
		w.state = st
		if w.s == nil {
			return
		}
		if names := failedNames(st.Failed); !slices.Equal(names, w.failedRepos) {
			w.failedRepos = names
			if len(names) > 0 { // a toast per new failure, not one on every refresh
				w.toastTexts = append(w.toastTexts, func(t *winWords) string {
					return fmt.Sprintf(t.ReposFailed, i18n.Plural(t.ReposFailedN, len(names)), strings.Join(names, ", "))
				})
			}
		}
		if st.Ready && (!was.Ready || was.Commits == 0 && st.Commits > 0) {
			w.refreshInfo(nil)
		}
		w.notify(view.StateChanged{Was: was, Now: st})
	})
}

// Tasks is session.Options.OnTasks: a task started, moved on or ended (any goroutine).
func (w *Workspace) Tasks() {
	w.update(func() {
		if w.s != nil {
			w.tasks = w.s.Tasks()
		}
	})
}

// Index is session.Options.OnIndex: the index has new content (any goroutine). The views hear of it once Info was read
// again.
func (w *Workspace) Index(u session.IndexUpdate) {
	w.update(func() {
		if w.s != nil {
			w.refreshInfo(view.IndexChanged{Update: u})
		}
	})
}

// Worktrees is session.Options.OnWorktrees: the worktrees' uncommitted state was read again (any goroutine).
func (w *Workspace) Worktrees(ws session.WorktreeStatus) {
	w.update(func() {
		if w.s != nil {
			w.notify(view.WorktreesChanged{Status: ws})
			w.refreshInfo(nil) // the counts in Info; in the background, so the views are not told late
		}
	})
}

// Config is session.Options.OnConfig: the workspace settings were saved (any goroutine).
func (w *Workspace) Config() {
	w.update(func() {
		if w.s != nil {
			w.refreshInfo(view.ConfigChanged{})
		}
	})
}

// refreshInfo reads Info in the background (it waits for the index lock), then tells the views e (nil: nothing).
func (w *Workspace) refreshInfo(e view.Event) {
	w.infoSeq++
	seq, s := w.infoSeq, w.s
	go func() {
		info := s.Info()
		w.update(func() {
			if seq == w.infoSeq {
				w.info = info
			}
			if e != nil {
				w.notify(e)
			}
		})
	}()
}

func (w *Workspace) notify(e view.Event) {
	for _, m := range w.views {
		m.v.Event(e)
	}
}

func (w *Workspace) SetVisible(visible bool) {
	if w.hidden == !visible {
		return
	}
	w.hidden = !visible
	for id, m := range w.views {
		m.v.Event(view.VisibilityChanged{Visible: visible && id == w.active})
	}
}

// Go shows view id, making it the first time, and hands it intent.
func (w *Workspace) Go(id view.ID, intent any) {
	d := def(id)
	if d == nil || w.s == nil {
		return
	}
	m := w.views[id]
	if m == nil {
		h := &host{w: w, dock: w.layout.dock(id, d.dock, d.dockVer)}
		m = &mounted{v: d.make(h), h: h}
		w.views[id] = m
	}
	if w.active != id {
		if old := w.views[w.active]; old != nil {
			old.v.Event(view.VisibilityChanged{Visible: false})
		}
		w.active = id
		m.v.Event(view.VisibilityChanged{Visible: !w.hidden})
	}
	if intent != nil {
		m.v.Open(intent)
	}
}

// Painted is closed once a view's content is first on screen (the smoke capture waits for it).
func (w *Workspace) Painted() <-chan struct{} { return w.painted }

// Smoke is the screenshot smoke's window to capture instead of this one ("" = this one): SHEAF_NATIVE_WIN.
func (w *Workspace) Smoke() string { return w.smoke.win }

// markPainted notes the first content reaching the screen; with SHEAF_TIMING=1 it logs the time, to compare with the launch.
func (w *Workspace) markPainted() {
	select {
	case <-w.painted:
		return
	default:
	}
	close(w.painted)
	if w.timing {
		fmt.Printf("[timing] first rows painted %d\n", time.Now().UnixMilli())
	}
	if w.smoke.win == "settings" {
		w.openSettings("")
	}
}

func (w *Workspace) openSettings(tab string) {
	if w.wins == nil {
		return
	}
	go func() { // opening a window waits for the main thread
		if err := w.wins.OpenSettings(tab); err != nil {
			w.update(func() { w.toasts.Error(err.Error()) })
		}
	}()
}

// View builds the window: the frame around the view shown.
func (w *Workspace) View(c *ui.Context) {
	c.SetTheme(theme.Theme())
	if w.s == nil || w.views[w.active] == nil {
		ui.Column(c).Fill().Background(theme.FrameBg).Center().Children(func() {
			ui.Text(c, i18n.Of(c).Opening).TextColor(theme.Text4)
		})
		return
	}
	m := w.views[w.active]
	p := m.v.Panes(c)
	m.h.dock.View(c, shell.Frame{
		Title:  func(c *ui.Context) { w.titleBar(c, p.Title) },
		Left:   p.Left,
		Right:  p.Right,
		Bottom: p.Bottom,
		Main:   p.Main,
		Status: func(c *ui.Context) {
			var left func(*ui.Context)
			if len(p.Left) > 0 {
				left = m.h.dock.LeftToggle
			}
			w.statusBar(c, left, p.Status, p.StatusEnd)
		},
	})
	widgets.Shortcuts(c, widgets.Shortcut{Mods: ui.Cmd, Key: ui.KeyComma, Do: func() { w.openSettings("") }})
	if len(w.toastTexts) > 0 {
		t := winTr.For(c.Locale())
		for _, text := range w.toastTexts {
			w.toasts.Error(text(t))
		}
		w.toastTexts = nil
	}
	w.toasts.View(c)
	w.layout.changed()
}

// failedNames lists the repos of a State's failures.
func failedNames(fs []session.RepoFailure) []string {
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = f.Repo
	}
	return names
}

// titleBar is the view's own title, which names what is on screen; the workspace name when the view has none.
func (w *Workspace) titleBar(c *ui.Context, slot func(*ui.Context)) {
	if slot == nil {
		ui.Text(c, w.info.Name).FontWeight(600).TextColor(theme.Text).PaddingX(8)
		ui.Spacer(c)
		return
	}
	ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() { slot(c) })
}

// statusBar has the left pane's button (left, when the view has one) at the window's bottom-left corner, then the view's
// own status, the task corner, and the view's end (a right pane's button) at the bottom-right corner. Settings are in
// the app menu (⌘,).
func (w *Workspace) statusBar(c *ui.Context, left, slot, end func(*ui.Context)) {
	if left != nil {
		left(c)
	}
	if slot != nil {
		slot(c)
	}
	w.taskStrip.View(c, w.s, w.tasks, w.state.Phase == "indexing")
	if end != nil {
		end(c)
	}
}

// host is the view.Host of one view.
type host struct {
	w    *Workspace
	dock *shell.Dock
}

func (h *host) Session() *session.Session { return h.w.s }
func (h *host) Info() session.Info        { return h.w.info }
func (h *host) State() session.State      { return h.w.state }
func (h *host) Update(f func())           { h.w.update(f) }
func (h *host) Context() context.Context  { return h.w.ctx }
func (h *host) Caller() context.Context   { return session.Asked(h.w.caller, h.w.asked.Add(1)) }
func (h *host) Go(id view.ID, intent any) { h.w.Go(id, intent) }
func (h *host) Toasts() *widgets.Toaster  { return &h.w.toasts }
func (h *host) Windows() view.Windows     { return windows{h.w} }
func (h *host) Painted()                  { h.w.markPainted() }
func (h *host) Dock() *shell.Dock         { return h.dock }

// windows forwards requests to the workspace's window owner; tests may omit it.
type windows struct{ w *Workspace }

func (o windows) OpenDiff(src session.DiffSource) error {
	if o.w.wins == nil {
		return nil
	}
	return o.w.wins.OpenDiff(src)
}

func (o windows) OpenSettings(tab string) error {
	if o.w.wins == nil {
		return nil
	}
	return o.w.wins.OpenSettings(tab)
}

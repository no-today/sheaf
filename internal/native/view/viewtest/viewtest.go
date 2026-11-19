// Package viewtest runs a view of the workspace window in a test, as the window would but without one: a session on
// fixture repos, a view.Host whose Update queues, and a ui.Tester drawing the frame. Only tests import it.
package viewtest

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/shell"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/view"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

// Fixture makes a workspace of two repos: repo-a with "first in a" and "second in a" (which adds dir/new.go), repo-b with
// "only in b". It returns the workspace's root.
func Fixture(t *testing.T) string {
	t.Helper()
	testutil.Isolate(t)
	root := filepath.Join(t.TempDir(), "ws")
	a := testutil.NewRepo(t, filepath.Join(root, "repo-a"))
	a.Commit("first in a", map[string]string{"a.txt": "1\n"})
	a.Commit("second in a", map[string]string{"a.txt": "2\n", "dir/new.go": "package x\n"})
	testutil.NewRepo(t, filepath.Join(root, "repo-b")).Commit("only in b", map[string]string{"b.txt": "1\n"})
	return root
}

// Host is a view.Host for tests. Update queues; Drain runs what was queued.
type Host struct {
	S       *session.Session
	Inf     session.Info
	St      session.State
	DockOf  shell.Dock
	Toaster widgets.Toaster
	Wins    Windows
	V       view.View // gets the session's events once set
	Went    []Went    // the Go calls
	Shown   bool      // Painted was called

	ctx    context.Context
	cancel context.CancelFunc
	asked  atomic.Uint64
	mu     sync.Mutex
	queue  []func()
}

// Went is a Go call.
type Went struct {
	ID     view.ID
	Intent any
}

// Windows records the windows a view opened. A view may open them from a goroutine: read them with the methods.
type Windows struct {
	mu       sync.Mutex
	diffs    []session.DiffSource
	settings []string
}

func (w *Windows) OpenDiff(src session.DiffSource) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.diffs = append(w.diffs, src)
	return nil
}
func (w *Windows) OpenSettings(tab string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.settings = append(w.settings, tab)
	return nil
}
func (w *Windows) Diffs() []session.DiffSource {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.diffs)
}
func (w *Windows) Settings() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.settings)
}

// Open opens root's session (refreshed) with a Host listening to it.
func Open(t *testing.T, root string) *Host {
	t.Helper()
	data := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	h := &Host{DockOf: shell.DefaultDock(), ctx: ctx, cancel: cancel}
	s, err := session.Open(context.Background(), root, session.Options{DataDir: data, CacheDir: t.TempDir(), Now: time.Now,
		OnState: func(st session.State) {
			h.Update(func() {
				was := h.St
				h.St = st
				if h.V != nil {
					h.V.Event(view.StateChanged{Was: was, Now: st})
				}
			})
		},
		OnWorktrees: func(ws session.WorktreeStatus) {
			h.Update(func() {
				if h.V != nil {
					h.V.Event(view.WorktreesChanged{Status: ws})
				}
			})
		},
		OnIndex: func(u session.IndexUpdate) {
			h.Update(func() {
				h.Inf = h.S.Info()
				if h.V != nil {
					h.V.Event(view.IndexChanged{Update: u})
				}
			})
		},
		OnConfig: func() {
			h.Update(func() {
				h.Inf = h.S.Info()
				if h.V != nil {
					h.V.Event(view.ConfigChanged{})
				}
			})
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	t.Cleanup(cancel)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.S = s
	h.Drain()
	h.Inf = s.Info()
	h.St = h.Inf.State
	return h
}

func (h *Host) Session() *session.Session { return h.S }
func (h *Host) Info() session.Info        { return h.Inf }
func (h *Host) State() session.State      { return h.St }
func (h *Host) Context() context.Context  { return h.ctx }
func (h *Host) Caller() context.Context   { return session.Asked(h.ctx, h.asked.Add(1)) }
func (h *Host) Toasts() *widgets.Toaster  { return &h.Toaster }
func (h *Host) Windows() view.Windows     { return &h.Wins }
func (h *Host) Painted()                  { h.Shown = true }
func (h *Host) Dock() *shell.Dock         { return &h.DockOf }
func (h *Host) Go(id view.ID, intent any) { h.Went = append(h.Went, Went{id, intent}) }

// Update queues f, unless the host was closed.
func (h *Host) Update(f func()) {
	if h.ctx.Err() != nil {
		return
	}
	h.mu.Lock()
	h.queue = append(h.queue, f)
	h.mu.Unlock()
}

// Close stops the updates, as the window's closing does.
func (h *Host) Close() { h.cancel() }

// Take removes the queued updates without running them, so a test can run them in an order of its choosing.
func (h *Host) Take() []func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	fs := h.queue
	h.queue = nil
	return fs
}

// Drain runs the queued updates.
func (h *Host) Drain() {
	h.mu.Lock()
	fs := h.queue
	h.queue = nil
	h.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

// Harness is a view mounted in a Host, drawn by a Tester in the window's frame.
type Harness struct {
	T  *testing.T
	H  *Host
	V  view.View
	TT *ui.Tester
}

// Mount makes the view with make and draws it in a 1400×900 frame. The Tester's language is locale ("" is English).
func Mount(t *testing.T, h *Host, locale string, make func(view.Host) view.View) *Harness {
	t.Helper()
	v := make(h)
	h.V = v
	frame := func(c *ui.Context) {
		c.SetTheme(theme.Theme())
		p := v.Panes(c)
		h.DockOf.View(c, shell.Frame{Title: p.Title, Left: p.Left, Right: p.Right, Bottom: p.Bottom, Main: p.Main, Status: p.Status})
		h.Toaster.View(c)
	}
	tt := ui.NewTester(frame, 1400, 900)
	t.Cleanup(v.Close)
	if locale != "" {
		tt.SetLocale(locale)
	}
	return &Harness{T: t, H: h, V: v, TT: tt}
}

// Shot writes the frame to $SHEAF_SHOTS/name.png at 2x, for states a smoke capture cannot reach (hover, press, menus,
// popovers). Without the variable it does nothing.
func (hr *Harness) Shot(name string) {
	hr.T.Helper()
	dir := os.Getenv("SHEAF_SHOTS")
	if dir == "" {
		return
	}
	hr.TT.SetScale(2)
	hr.H.Drain()
	hr.TT.Frame()
	hr.TT.Advance(300 * time.Millisecond) // past any panel's entrance, or it is drawn see-through
	hr.TT.Frame()
	var buf bytes.Buffer
	if err := png.Encode(&buf, hr.TT.Image()); err != nil {
		hr.T.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), buf.Bytes(), 0o644); err != nil {
		hr.T.Fatal(err)
	}
}

// Until runs queued updates and frames until ok holds, for up to 2 seconds.
func (hr *Harness) Until(what string, ok func() bool) {
	hr.T.Helper()
	for range 400 {
		hr.H.Drain()
		hr.TT.Frame()
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	hr.T.Fatalf("timed out waiting for %s; texts %q", what, hr.TT.Texts())
}

// TrackAll tracks every requirement number the workspace shows candidates for, as a user who typed them all would; the
// requirement pattern is read now, so set it first.
func TrackAll(t *testing.T, s *session.Session) {
	t.Helper()
	cands, err := s.RequirementCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if err := s.Track(c.ID); err != nil {
			t.Fatal(err)
		}
	}
}

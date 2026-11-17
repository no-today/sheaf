package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native"
	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

// Errors shown by the folder picker and workspace menu.
var (
	errNoRepos = errors.New("no_repos")  // the folder has no git repository in it
	errNotDir  = errors.New("not_a_dir") // the path is not a folder
	// errWorkspaceBusy: another process kept the workspace lock past the wait.
	errWorkspaceBusy = errors.New("workspace_busy")
)

// Windows owns one window per workspace.
// A workspace is open in at most one window; opening it again focuses that one.
type Windows struct {
	ws       *Workspace
	settings *SettingsStore
	recents  *RecentStore

	mu      sync.Mutex
	byRoot  map[string]*mygo.Window
	opening map[string]bool // roots being opened now (Scan and session.Open take seconds)
}

func newWindows(ws *Workspace, settings *SettingsStore, recents *RecentStore) *Windows {
	return &Windows{ws: ws, settings: settings, recents: recents, byRoot: map[string]*mygo.Window{}, opening: map[string]bool{}}
}

// Start opens explicit folders, or restores the last open workspaces or the newest recent workspace.
// With no workspace to open, it asks for a folder.
func (m *Windows) Start(ctx context.Context, explicit []string) {
	m.start(explicit, func(root string) error {
		err := m.open(ctx, root, false)
		if err != nil {
			m.report(root, err)
		}
		return err
	}, func() {
		if err := m.OpenFolderDialog(ctx, nil); err != nil {
			m.report("", err)
		}
	})
}

func (m *Windows) start(explicit []string, open func(string) error, choose func()) {
	roots := explicit
	if len(roots) == 0 {
		roots = m.recents.OpenRoots()
	}
	opened := false
	for _, root := range roots {
		if err := open(root); err == nil {
			opened = true
		}
	}
	if opened {
		return
	}
	if len(explicit) == 0 {
		for _, r := range m.recents.List() {
			if !r.Missing && !slices.Contains(roots, r.Root) && open(r.Root) == nil {
				return
			}
		}
	}
	choose()
}

// BeforeQuit keeps workspaces that close with the app in the open list.
func (m *Windows) BeforeQuit() {
	m.recents.Freeze()
}

// OpenWorkspace opens root in its own window, or focuses the window it is already open in.
// New folders are checked for repositories first.
func (m *Windows) OpenWorkspace(ctx context.Context, root string) error {
	return m.open(ctx, root, true)
}

func (m *Windows) open(ctx context.Context, root string, check bool) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("open %s: %w", root, err)
	}
	root = filepath.Clean(root)
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return errNotDir
	}
	win, others, ok := m.claim(root)
	if win != nil {
		win.Show()
		return nil
	}
	if !ok {
		return nil // already being opened (a double click, or menu and drop at once)
	}
	defer m.release(root)
	// A workspace seen for the first time opens a little off the newest window (a known one has its own saved position).
	var cx, cy int
	for _, w := range others {
		if b := w.Bounds(); b.X+30 > cx {
			cx, cy = b.X+30, b.Y+30
		}
	}
	if check && !m.known(root) {
		if has, err := session.HasRepos(ctx, root); err != nil {
			return fmt.Errorf("open %s: %w", root, err)
		} else if !has {
			return errNoRepos
		}
	}

	win, err = m.openNative(ctx, root, cx, cy)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.byRoot[root] = win
	m.mu.Unlock()
	win.OnFileDrop(func(e *mygo.FileDropEvent) { go m.dropped(e) })
	win.OnClosed(func() { m.workspaceClosed(root, win) })
	win.Show()
	m.RebuildMenu() // the recent list has this workspace first now
	return nil
}

// openNative opens root in a window drawn by the native UI toolkit.
func (m *Windows) openNative(ctx context.Context, root string, cx, cy int) (*mygo.Window, error) {
	var win *mygo.Window
	nw := native.New(func(f func()) { win.Update(f) }, nativeWindows{ws: m.ws, wins: m, win: &win})
	win = mygo.NewWindow(mygo.WindowOptions{
		Title:           filepath.Base(root),
		Width:           1440,
		Height:          900,
		MinWidth:        1180,
		MinHeight:       640,
		BackgroundColor: theme.FrameHex,
		TitleBarStyle:   mygo.TitleBarHidden,
		X:               cx,
		Y:               cy,
		StateKey:        "native:" + session.WorkspaceID(root),
		Content:         ui.View(nw.View),
		Hidden:          true,
	})
	followWindowVisibility(win, func(visible bool) {
		win.Update(func() { nw.SetVisible(visible) })
	})
	s, err := m.ws.open(ctx, win, root, nw)
	if err != nil {
		win.Destroy()
		return nil, err
	}
	win.OnClosed(nw.Close)
	win.Update(func() { nw.Attach(s) })
	if nw.Smoke() == "" { // else the smoke captures the window it opens (openNativeWindow)
		go captureNative(win, nw.Painted())
	}
	return win, nil
}

// claim returns the window root is open in, or reserves root for opening (ok false: it is being opened
// already). others are the open workspace windows.
func (m *Windows) claim(root string) (win *mygo.Window, others []*mygo.Window, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w := m.byRoot[root]; w != nil {
		return w, nil, false
	}
	if m.opening[root] {
		return nil, nil, false
	}
	m.opening[root] = true
	for _, w := range m.byRoot {
		others = append(others, w)
	}
	return nil, others, true
}

func (m *Windows) release(root string) {
	m.mu.Lock()
	delete(m.opening, root)
	m.mu.Unlock()
}

// known: the folder is in the recent list, so it was opened (and had repositories) before.
func (m *Windows) known(root string) bool {
	for _, r := range m.recents.List() {
		if r.Root == root {
			return true
		}
	}
	return false
}

func (m *Windows) workspaceClosed(root string, win *mygo.Window) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byRoot[root] == win {
		delete(m.byRoot, root)
	}
}

// dropped opens the folders among the dropped paths, each in its own window. It runs on a goroutine:
// opening takes seconds and listeners run on the main thread.
func (m *Windows) dropped(e *mygo.FileDropEvent) {
	for _, p := range e.Paths {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			if err := m.OpenWorkspace(context.Background(), p); err != nil {
				m.report(p, err)
			}
		}
	}
}

// OpenFolderDialog asks for a folder (as a sheet on parent) and opens it.
func (m *Windows) OpenFolderDialog(ctx context.Context, parent *mygo.Window) error {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Directory: true})
	if err != nil || len(paths) == 0 {
		return err
	}
	return m.OpenWorkspace(ctx, paths[0])
}

// Package app connects native windows, dialogs and application settings to workspace sessions.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sheaf/internal/native/theme"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"sheaf/internal/appdir"
	"sheaf/internal/session"
	"sheaf/internal/watch"
)

// Run opens the first windows and blocks until the app quits.
func Run() error {
	if os.Getenv("GOGC") == "" {
		// The index is most of the heap and lives as long as the window: collecting at +50% instead of +100% keeps about
		// 50 MB off the process on a large workspace (35 repos, 230k commits), with no measurable cost to queries or the index build.
		debug.SetGCPercent(50)
	}
	data, _ := dirs()
	settings := openSettings(filepath.Join(data, "settings.json"))
	recents := openRecents(filepath.Join(data, "recent.json"), time.Now)
	ws := &Workspace{sessions: map[int]*session.Session{}, diffs: map[int]*mygo.Window{}, diffKeys: map[int]string{}, parent: map[int]*mygo.Window{}, recents: recents, gitLimit: session.NewGitLimit()}
	wins := newWindows(ws, settings, recents)

	// Keep the app and its Open Folder / Open Recent menus available after the last window closes.
	mygo.App.OnWindowAllClosed(func() {})
	// A click on the Dock icon with no window open brings back what a launch would.
	mygo.App.OnActivate(func(visible bool) {
		if !visible {
			go wins.Start(context.Background(), nil)
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { wins.BeforeQuit() })

	mygo.App.WhenReady(func() {
		setNativeLanguage(settings.Get().Lang)
		theme.SetCodeFont(settings.Get().CodeFont)
		wins.RebuildMenu()
		go wins.Start(context.Background(), explicitRoots()) // opening takes seconds: not on the main thread
	})
	return mygo.App.Run()
}

// explicitRoots: folders passed on the command line, else SHEAF_WORKSPACE (a ':' separated list).
func explicitRoots() []string {
	var out []string
	for _, a := range os.Args[1:] {
		if st, err := os.Stat(a); err == nil && st.IsDir() {
			out = append(out, a)
		}
	}
	if len(out) == 0 && os.Getenv("SHEAF_WORKSPACE") != "" {
		out = filepath.SplitList(os.Getenv("SHEAF_WORKSPACE"))
	}
	return out
}

func dirs() (data, cache string) {
	root := appdir.Root(appdir.Dev(mygo.App.Name()))
	return appdir.Data(root), appdir.Cache(root)
}

// Workspace owns the sessions and auxiliary windows of workspace windows.
type Workspace struct {
	mu       sync.Mutex
	sessions map[int]*session.Session // window id -> session (a diff window shares its workspace window's)
	diffs    map[int]*mygo.Window     // the diff windows (diffwin.go), by id
	diffKeys map[int]string           // what each diff window was opened with (parent id + source), to raise it instead of opening a twin
	parent   map[int]*mygo.Window     // diff window id -> its workspace window
	recents  *RecentStore
	gitLimit *session.GitLimit // shared by every window's session
	closes   map[int]func()    // window id -> close session and release its lock once
	lockMu   sync.Mutex
	locks    map[string]*workspaceLock // absolute workspace root -> process-owned lock
}

type workspaceLock struct {
	refs   int
	unlock func()
	ready  chan struct{} // closes when acquisition finishes; pending entries have no unlock
}

func (w *Workspace) lockWorkspace(ctx context.Context, data, root string) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lockError := func(err error) error {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %w", errWorkspaceBusy, err)
		}
		return fmt.Errorf("lock workspace: %w", err)
	}
	w.lockMu.Lock()
	if w.locks == nil {
		w.locks = map[string]*workspaceLock{}
	}
	var entry *workspaceLock
	for {
		if err := ctx.Err(); err != nil {
			w.lockMu.Unlock()
			return nil, lockError(err)
		}
		entry = w.locks[root]
		if entry == nil {
			entry = &workspaceLock{ready: make(chan struct{})}
			w.locks[root] = entry
			w.lockMu.Unlock()
			unlock, err := appdir.Lock(ctx, data, session.WorkspaceID(root))
			w.lockMu.Lock()
			if err != nil {
				delete(w.locks, root)
				close(entry.ready)
				w.lockMu.Unlock()
				return nil, lockError(err)
			}
			entry.unlock = unlock
			close(entry.ready)
		} else if entry.unlock == nil {
			ready := entry.ready
			w.lockMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, lockError(ctx.Err())
			case <-ready:
			}
			w.lockMu.Lock()
			continue
		}
		entry.refs++
		w.lockMu.Unlock()
		break
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			w.lockMu.Lock()
			defer w.lockMu.Unlock()
			entry.refs--
			if entry.refs == 0 {
				entry.unlock()
				delete(w.locks, root)
			}
		})
	}, nil
}

var errNoWorkspace = errors.New("no workspace open in this window")

// listener receives changes to a workspace session.
type listener interface {
	State(session.State)
	Index(session.IndexUpdate)
	Worktrees(session.WorktreeStatus)
	Config()
	Tasks()
}

// open opens root's session for win and sends its events to l.
func (w *Workspace) open(ctx context.Context, win *mygo.Window, root string, l listener) (*session.Session, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("locate workspace: %w", err)
	}
	data, cache := dirs()
	root = session.CanonicalRoot(data, root)
	unlock, err := w.lockWorkspace(ctx, data, root)
	if err != nil {
		return nil, err
	}
	var settled sync.Once
	s, err := session.Open(ctx, root, session.Options{
		DataDir: data, CacheDir: cache, GitLimit: w.gitLimit,
		OnState: func(st session.State) {
			l.State(st)
			if st.Ready && !st.Indexing {
				// Building or loading the index peaks at about twice the heap it leaves; Go hands the difference back to
				// the system only slowly, and it counts against the memory budget meanwhile.
				settled.Do(func() { go debug.FreeOSMemory() })
			}
		},
		OnWorktrees: l.Worktrees,
		OnTasks:     l.Tasks,
		OnIndex:     l.Index,
		OnConfig:    l.Config,
		Watch:       watch.Files,
	})
	if err != nil {
		unlock()
		return nil, err
	}
	var closed sync.Once
	closeSession := func() {
		closed.Do(func() {
			s.Close()
			unlock()
		})
	}
	w.mu.Lock()
	if w.closes == nil {
		w.closes = map[int]func(){}
	}
	if closeOld := w.closes[win.ID()]; closeOld != nil {
		go closeOld()
	}
	w.sessions[win.ID()] = s
	w.closes[win.ID()] = closeSession
	w.mu.Unlock()
	win.SetTitle(s.Name)
	win.OnClosed(func() {
		w.mu.Lock()
		if w.sessions[win.ID()] == s {
			delete(w.sessions, win.ID())
			delete(w.closes, win.ID())
		}
		w.mu.Unlock()
		closeSession()
		w.recents.MarkClosed(s.Root)
	})
	w.recents.Touch(s.Root, s.Name)
	w.recents.MarkOpen(s.Root)
	return s, nil
}

func (w *Workspace) sessionFor(win *mygo.Window) (*session.Session, error) {
	if win == nil {
		return nil, errNoWorkspace
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if s := w.sessions[win.ID()]; s != nil {
		return s, nil
	}
	return nil, errNoWorkspace
}

package native

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sheaf/internal/native/shell"
	"sheaf/internal/native/view"
)

// layoutFile holds the dock arrangement of each view in session.DataDir so layout changes never write to the user's repos.
const layoutFile = "native-layout.json"

// saveDelay lets a drag of a divider end before the layout is written.
const saveDelay = 500 * time.Millisecond

// layout is the dock arrangement of the window's views as the user left them. The views' docks are read and changed on
// the main thread; writing the file happens on a timer.
type layout struct {
	path  string
	docks map[view.ID]*shell.Dock // the views made, live
	saved map[view.ID]shell.Dock  // as last written (or read), the views not made too
	vers  map[view.ID]int         // the version of the defaults each saved dock was made from (viewDef.dockVer)
	stale bool                    // a saved dock of older defaults was dropped: write the file again

	mu       sync.Mutex // one write at a time
	timer    *time.Timer
	next     map[view.ID]shell.Dock // what the timer writes
	nextVers map[view.ID]int
}

type layoutJSON struct {
	Docks    map[view.ID]shell.Dock `json:"docks"`
	Versions map[view.ID]int        `json:"versions,omitempty"` // missing: 0
}

// openLayout reads dir's layout; a missing or unreadable file is the defaults. dir "" keeps nothing.
func openLayout(dir string) *layout {
	l := &layout{docks: map[view.ID]*shell.Dock{}, saved: map[view.ID]shell.Dock{}, vers: map[view.ID]int{}}
	if dir == "" {
		return l
	}
	l.path = filepath.Join(dir, layoutFile)
	if b, err := os.ReadFile(l.path); err == nil {
		var j layoutJSON
		if json.Unmarshal(b, &j) == nil && j.Docks != nil {
			l.saved = j.Docks
			if j.Versions != nil {
				l.vers = j.Versions
			}
		}
	}
	return l
}

// dock is view id's arrangement, live: the saved one, else def's. A saved dock made from older defaults (ver is the
// view's viewDef.dockVer) is dropped: a saved layout records the defaults too, not only what the user moved, so a
// default that changed would never show.
func (l *layout) dock(id view.ID, def func() shell.Dock, ver int) *shell.Dock {
	if d := l.docks[id]; d != nil {
		return d
	}
	d, ok := l.saved[id]
	if !ok || l.vers[id] != ver {
		d = def()
		l.stale = l.stale || ok
	}
	l.vers[id] = ver
	l.docks[id] = &d
	return &d
}

// changed writes the layout soon when a dock changed since it was last written. Call it after every frame.
func (l *layout) changed() {
	diff := l.stale
	l.stale = false
	for id, d := range l.docks {
		if s, ok := l.saved[id]; !ok || s != *d {
			l.saved[id] = *d
			diff = true
		}
	}
	if !diff || l.path == "" {
		return
	}
	next, vers := maps.Clone(l.saved), maps.Clone(l.vers)
	l.mu.Lock()
	l.next, l.nextVers = next, vers
	if l.timer == nil {
		l.timer = time.AfterFunc(saveDelay, l.writeNext)
	} else {
		l.timer.Reset(saveDelay)
	}
	l.mu.Unlock()
}

// flush writes a pending change now (the window closes).
func (l *layout) flush() {
	l.mu.Lock()
	pending := l.timer != nil && l.timer.Stop()
	l.mu.Unlock()
	if pending {
		l.writeNext()
	}
}

func (l *layout) writeNext() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.next == nil {
		return
	}
	if err := writeJSON(l.path, layoutJSON{Docks: l.next, Versions: l.nextVers}); err != nil {
		fmt.Println("[layout]", err) // the layout is a convenience: the next launch starts from the defaults
	}
	l.next = nil
}

// writeJSON replaces path with v atomically (a temporary file renamed over it).
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

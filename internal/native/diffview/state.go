// Package diffview is Preview Diff for the native UI: a list of files, one shown at a time as editor.DiffView, with
// IDEA's keys to step through the changes and the files. Requirement and uncommitted changes give it files and a Loader.
package diffview

import (
	"context"
	"fmt"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// File is one file of the list.
type File struct {
	Path    string
	OldPath string // source of a rename or copy
	Status  string // as session.FileChange.Status: A M D R C T
	// Texts, when set, are the file's two sides, and the Loader is not asked for them.
	Texts *Texts
	// Source is the caller's own record of the file (a session.FileChange, a worktree entry), handed back to the Loader.
	Source any
}

// Texts are the whole file before and after; the diff is computed from them, so unchanged lines can be expanded.
type Texts struct {
	Old, New string
	Binary   bool
	Cut      bool // only the start of a very large file (session.FileText.Cut)
	// Parts, when set, are shown instead of Old and New: only the hunks of each patch, numbered as in the file, a part's
	// first hunk under its title (a filter's file commit by commit). More counts the commits left out.
	Parts []Part
	More  int
	// Resolved: a part is a merge edited while merging, on top of a range diff.
	Resolved bool
}

// Loader reads the texts of a file. It runs on its own goroutine; ctx is cancelled when the list is replaced.
type Loader func(ctx context.Context, f File) (Texts, error)

// State is Preview Diff's state, kept by the page across frames.
type State struct {
	// Layout is side by side (editor.DiffSplit) or one column (editor.DiffUnified); the header toggles it.
	Layout editor.DiffLayout
	// ForceUnified keeps a preview in one column and hides the layout switch.
	ForceUnified bool
	// ToolsOutside leaves the navigation and layout switch out of the header, for a host that draws Tools elsewhere.
	ToolsOutside bool
	// Theme uses the dock background when nil.
	Theme *Theme
	// OnSelect is called when the shown file changes, by keys, buttons or Select, so a file list can follow.
	OnSelect func(i int)

	files   []File
	load    Loader
	cur     int
	entries map[int]*entry
	ctx     context.Context
	cancel  context.CancelFunc
	// hadFocus: the keyboard was in Preview Diff in the last frame.
	hadFocus bool
	// viewed: View ran since BeginFrame.
	viewed bool

	mu      sync.Mutex
	gen     int // SetFiles bumps it, so loads of the previous list are dropped
	arrived []arrival
}

type target int

const (
	goNone target = iota
	goFirst
	goLast
)

type entry struct {
	started bool
	ready   bool
	err     error
	binary  bool
	cut     bool
	more    int      // Texts.More
	titles  []string // of the parts
	bins    []string // the titles of the binary parts
	cancel  context.CancelFunc
	diff    *editor.DiffState
	goTo    target // the change to go to once the diff is built
	// picked is the layout chosen while this one-sided file was shown in one column on its own; it lasts while the entry
	// does, and State.Layout stays as it was.
	picked *editor.DiffLayout
}

type arrival struct {
	gen, i int
	texts  Texts
	err    error
}

// keep is how many files on each side of the current one keep their diff (buffers, expanded lines, scroll):
// stepping back and forth stays instant without holding every file of a large commit.
const keep = 4

// New makes the state of a list of files, showing the first.
func New(files []File, load Loader) *State {
	s := &State{}
	s.SetFiles(files, load, 0)
	return s
}

// SetFiles replaces the list and shows file current. Loads still running for the previous list are cancelled.
func (s *State) SetFiles(files []File, load Loader, current int) {
	if s.cancel != nil {
		s.cancel()
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.mu.Lock()
	s.gen++
	s.arrived = nil
	s.mu.Unlock()
	s.hadFocus = false // the keyboard is where the host put it, not here
	s.files, s.load = files, load
	s.entries = map[int]*entry{}
	s.cur = -1
	s.Select(current)
}

// BeginFrame is for a host that does not always show the view (a collapsed pane, a placeholder): call it at the start of
// every frame, so a frame without View forgets that the view had the keyboard and cannot take it back later.
func (s *State) BeginFrame() {
	if !s.viewed {
		s.hadFocus = false
	}
	s.viewed = false
}

// Close cancels the loads still running.
func (s *State) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// Current is the index of the file shown, -1 when the list is empty.
func (s *State) Current() int { return s.cur }

// Select shows file i at its first change.
func (s *State) Select(i int) { s.show(i, goFirst) }

// NextFile and PrevFile show the next and previous file at its first change (Right and Left).
func (s *State) NextFile() { s.show(s.cur+1, goFirst) }
func (s *State) PrevFile() { s.show(s.cur-1, goFirst) }

// NextChange goes to the next change of the file, or from its last change to the first change of the next file
// (Down, F7), as IDEA's Next Difference does. A file still loading takes it once it is shown.
func (s *State) NextChange() {
	e := s.entries[s.cur]
	if e == nil || !e.ready {
		return
	}
	if e.diff != nil {
		i, _ := e.diff.CurrentHunk()
		if i+1 < len(e.diff.Hunks()) {
			e.diff.GoToHunk(i + 1)
			return
		}
	}
	s.show(s.cur+1, goFirst)
}

// PrevChange goes to the previous change, or from the first to the last change of the previous file (Up, Shift+F7).
func (s *State) PrevChange() {
	e := s.entries[s.cur]
	if e == nil || !e.ready {
		return
	}
	if e.diff != nil {
		i, inside := e.diff.CurrentHunk()
		if inside { // in a change: the one before it; below one: that one
			i--
		}
		if i >= 0 {
			e.diff.GoToHunk(i)
			return
		}
	}
	s.show(s.cur-1, goLast)
}

// layout is the file shown's: one column for a file that only adds or only deletes lines, where the other side would
// be empty, unless one picked a layout for it; State.Layout otherwise.
func (s *State) layout() editor.DiffLayout {
	if s.ForceUnified {
		return editor.DiffUnified
	}
	e := s.entries[s.cur]
	switch {
	case e == nil:
	case e.picked != nil:
		return *e.picked
	case e.diff != nil && oneSided(e.diff):
		return editor.DiffUnified
	}
	return s.Layout
}

// pick sets the layout from the switch: for the file shown alone when it is one-sided, for every file otherwise.
func (s *State) pick(l editor.DiffLayout) {
	if e := s.entries[s.cur]; e != nil && e.diff != nil && oneSided(e.diff) {
		e.picked = &l
		return
	}
	s.Layout = l
}

func oneSided(d *editor.DiffState) bool {
	add, del := d.Stats()
	return add == 0 || del == 0
}

// Diff is the viewer's state of the file shown, nil while it loads or when it has no text diff (binary, failed).
func (s *State) Diff() *editor.DiffState {
	if e := s.entries[s.cur]; e != nil {
		return e.diff
	}
	return nil
}

func (s *State) show(i int, at target) {
	if i < 0 || i >= len(s.files) {
		if len(s.files) == 0 {
			s.cur = -1
		}
		return
	}
	changed := i != s.cur
	s.cur = i
	e := s.entries[i]
	if e == nil {
		e = &entry{}
		s.entries[i] = e
	}
	e.goTo = at
	for j, o := range s.entries {
		switch {
		case o.started && !o.ready && j != i && j != i+1:
			// Stepped past while loading: stop it (git runs per file); shown again, it loads again.
			if o.cancel != nil {
				o.cancel()
			}
			delete(s.entries, j)
		case (j < i-keep || j > i+keep) && (o.ready || !o.started):
			delete(s.entries, j)
		}
	}
	if changed && s.OnSelect != nil {
		s.OnSelect(i)
	}
}

// fetch starts loading file i unless it is known or loading. Results are applied on the UI thread by settle.
func (s *State) fetch(i int, svc ui.Services) {
	if i < 0 || i >= len(s.files) {
		return
	}
	e := s.entries[i]
	if e == nil {
		e = &entry{}
		s.entries[i] = e
	}
	if e.started {
		return
	}
	e.started = true
	f := s.files[i]
	if f.Texts != nil {
		s.apply(i, e, *f.Texts, nil)
		return
	}
	if s.load == nil {
		e.ready = true
		return
	}
	s.mu.Lock()
	gen := s.gen
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.WithValue(s.ctx, ownerKey{}, fmt.Sprintf("%p", s)))
	e.cancel = cancel
	load := s.load
	go func() {
		defer cancel()
		t, err := load(ctx, f)
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		if gen == s.gen {
			s.arrived = append(s.arrived, arrival{gen, i, t, err})
		}
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// settle applies the loads that finished since the last frame.
func (s *State) settle() {
	s.mu.Lock()
	got, gen := s.arrived, s.gen
	s.arrived = nil
	s.mu.Unlock()
	for _, a := range got {
		if e := s.entries[a.i]; e != nil && a.gen == gen && !e.ready {
			s.apply(a.i, e, a.texts, a.err)
		}
	}
}

func (s *State) apply(i int, e *entry, t Texts, err error) {
	e.ready, e.err, e.binary, e.cut, e.more = true, err, t.Binary, t.Cut, t.More
	if err != nil || t.Binary {
		return
	}
	f := s.files[i]
	if len(t.Parts) > 0 {
		e.diff = &editor.DiffState{Bindings: navBindings()}
		var p editor.FilePatch
		p, e.titles = patchOf(f.Path, t.Parts)
		e.diff.SetPatch(p)
		e.bins = binaryParts(t.Parts)
		return
	}
	oldPath := f.Path
	if f.OldPath != "" {
		oldPath = f.OldPath
	}
	old, nw := editor.NewBuffer(t.Old), editor.NewBuffer(t.New)
	old.SetLanguage(editor.LanguageFor(oldPath))
	nw.SetLanguage(editor.LanguageFor(f.Path))
	e.diff = &editor.DiffState{Old: old, New: nw, Bindings: navBindings()}
}

// navBindings take the plain arrows from the sides' editors: the diff is read only, so moving a caret line by line
// or letter by letter is of little use, while stepping through changes (Up/Down) and files (Left/Right) is the work
// of a review, as in IDEA's diff. With modifiers the arrows still move and select (Shift+Up selects a line,
// Alt/Cmd move by words and to the ends), and clicking, dragging, Cmd+A, Cmd+C and Cmd+F work as in any editor.
func navBindings() []editor.Binding {
	return []editor.Binding{
		{Key: ui.KeyUp, Command: editor.NoCommand},
		{Key: ui.KeyDown, Command: editor.NoCommand},
		{Key: ui.KeyLeft, Command: editor.NoCommand},
		{Key: ui.KeyRight, Command: editor.NoCommand},
	}
}

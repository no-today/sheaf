package diffview

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// changedAt is a 300-line file with one changed line at each of lines.
func changedAt(lines ...int) *Texts {
	var old, nw strings.Builder
	for i := range 300 {
		old.WriteString("same\n")
		changed := false
		for _, l := range lines {
			changed = changed || l == i
		}
		if changed {
			nw.WriteString("changed\n")
		} else {
			nw.WriteString("same\n")
		}
	}
	return &Texts{Old: old.String(), New: nw.String()}
}

func tester(s *State) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) { View(c, s) }, 900, 400)
	tt.Frame()
	return tt
}

// where is the file shown and the change the caret is in.
func where(s *State) string {
	d := s.Diff()
	if d == nil {
		return s.files[s.cur].Path
	}
	i, inside := d.CurrentHunk()
	if !inside {
		return fmt.Sprintf("%s after %d", s.files[s.cur].Path, i)
	}
	return fmt.Sprintf("%s %d", s.files[s.cur].Path, i)
}

func TestKeysStepThroughChangesAndFiles(t *testing.T) {
	files := []File{
		{Path: "a.go", Status: "M", Texts: changedAt(50, 150, 250)},
		{Path: "logo.png", Status: "M", Texts: &Texts{Binary: true}},
		{Path: "b.go", Status: "M", Texts: changedAt(20, 200)},
	}
	type step struct {
		mods ui.Modifiers
		key  ui.Key
		want string
	}
	steps := []step{
		{0, ui.KeyDown, "a.go 1"},
		{0, ui.KeyF7, "a.go 2"},
		{0, ui.KeyDown, "logo.png"}, // from the last change to the next file
		{0, ui.KeyDown, "b.go 0"},   // a file without a text diff passes on at once
		{0, ui.KeyDown, "b.go 1"},
		{0, ui.KeyDown, "b.go 1"}, // the last change of the last file stays
		{0, ui.KeyUp, "b.go 0"},
		{0, ui.KeyUp, "logo.png"},
		{ui.Shift, ui.KeyF7, "a.go 2"}, // back into a file at its last change
		{0, ui.KeyUp, "a.go 1"},
		{0, ui.KeyRight, "logo.png"},
		{0, ui.KeyRight, "b.go 0"}, // a file is shown at its first change
		{0, ui.KeyRight, "b.go 0"},
		{0, ui.KeyLeft, "logo.png"},
		{0, ui.KeyLeft, "a.go 0"},
		{0, ui.KeyLeft, "a.go 0"},
	}
	// The keys work with the focus on Preview Diff itself (a page focuses it, or a click on the header)
	// and in the text, where the editor would otherwise take the arrows.
	for _, focus := range []string{"header", "text"} {
		s := New(files, nil)
		tt := tester(s)
		if got := where(s); got != "a.go 0" {
			t.Fatalf("shown at %q, want the first change", got)
		}
		if focus == "header" {
			tt.Click("a.go")
		} else {
			tt.ClickAt(600, 180) // below the first change (a separator, 3 lines, the change, 3 lines), in the new side
			if got := where(s); got != "a.go after 0" {
				t.Fatalf("clicked at %q", got)
			}
		}
		for i, st := range steps {
			tt.Key(st.mods, st.key)
			if got := where(s); got != st.want {
				t.Fatalf("%s focus, step %d (%v %v): at %q, want %q", focus, i, st.mods, st.key, got, st.want)
			}
		}
	}
}

// Up from below a change goes to that change, as IDEA's Previous Difference.
func TestUpFromBetweenChanges(t *testing.T) {
	s := New([]File{{Path: "a.go", Texts: changedAt(50, 150, 250)}}, nil)
	tt := tester(s)
	tt.ClickAt(600, 180)
	if got := where(s); got != "a.go after 0" {
		t.Fatalf("clicked at %q", got)
	}
	tt.Key(0, ui.KeyUp)
	if got := where(s); got != "a.go 0" {
		t.Fatalf("up: %q", got)
	}
}

// A selection over several lines, made with the editor's own keys (Cmd+Left, Shift+Down, which Preview Diff leaves
// to it), copies the code alone: no line numbers, no + or − markers, no rows the other side shows. In one column the deleted lines are not text of the editor, so they are not copied; split shows them.
func TestCopyIsCodeOnly(t *testing.T) {
	texts := &Texts{Old: "alpha\nbeta\ngamma\ndelta\n", New: "alpha\nBETA\ngamma\ndelta\nepsilon\n"}
	for _, c := range []struct {
		layout editor.DiffLayout
		x      float32
		want   string
	}{
		{editor.DiffUnified, 600, "alpha\nBETA\ngamma\ndelta\nepsilon\n"},
		{editor.DiffSplit, 200, "alpha\nbeta\ngamma\ndelta\n"},
		{editor.DiffSplit, 650, "alpha\nBETA\ngamma\ndelta\nepsilon\n"},
	} {
		s := New([]File{{Path: "notes.txt", Status: "M", Texts: texts}}, nil)
		s.Layout = c.layout
		tt := tester(s)
		tt.ClickAt(c.x, 42) // the first row, under the header
		tt.Key(ui.Cmd, ui.KeyLeft)
		for range 6 {
			tt.Key(ui.Shift, ui.KeyDown)
		}
		tt.Command("copy")
		if got := tt.Clipboard(); got != c.want {
			t.Errorf("layout %v at x %v: copied %q, want %q", c.layout, c.x, got, c.want)
		}
	}
}

func TestLayoutToggle(t *testing.T) {
	s := New([]File{{Path: "a.go", Texts: changedAt(5)}}, nil)
	tt := tester(s)
	if err := tt.Click("Unified"); err != nil {
		t.Fatal(err)
	}
	if s.Layout != editor.DiffUnified || s.Diff().Layout != editor.DiffUnified {
		t.Fatalf("layout %v, viewer's %v", s.Layout, s.Diff().Layout)
	}
}

func TestToolbarStepsThroughChangesAcrossFiles(t *testing.T) {
	for _, layout := range []editor.DiffLayout{editor.DiffSplit, editor.DiffUnified} {
		t.Run(fmt.Sprint(layout), func(t *testing.T) {
			s := New([]File{{Path: "a.go", Texts: changedAt(50, 150, 250)}, {Path: "b.go", Texts: changedAt(10)}}, nil)
			s.Layout = layout
			tt := tester(s)
			places := map[string]ui.Rect{}
			steps := []struct {
				button, at, counter string
			}{
				{"Previous change", "a.go 0", "change 1 of 3"}, // greyed out: nothing before
				{"Next change", "a.go 1", "change 2 of 3"},
				{"Next change", "a.go 2", "change 3 of 3"},
				{"Next change", "b.go 0", "1 change"}, // past the last change: the next file
				{"Next change", "b.go 0", "1 change"}, // greyed out: nothing after
				{"Previous change", "a.go 2", "change 3 of 3"},
			}
			for _, st := range steps {
				for _, label := range []string{"Previous change", "Next change"} {
					r, shown := tt.Find(label)
					if !shown {
						t.Fatalf("%s is hidden at %q; it should grey out in place", label, where(s))
					}
					if p, ok := places[label]; ok && p != r {
						t.Fatalf("%s moved from %v to %v", label, p, r)
					}
					places[label] = r
				}
				_ = tt.Click(st.button)
				tt.Frame()
				if got := where(s); got != st.at {
					t.Fatalf("%s: at %q, want %q", st.button, got, st.at)
				}
				if !tt.HasText(st.counter) {
					t.Fatalf("the counter did not follow, want %q: %q", st.counter, tt.Texts())
				}
			}
		})
	}
}

func TestToolbarPreviousChangeFromBetweenChanges(t *testing.T) {
	s := New([]File{{Path: "a.go", Texts: changedAt(50, 150, 250)}}, nil)
	tt := tester(s)
	tt.ClickAt(600, 180)
	if got := where(s); got != "a.go after 0" {
		t.Fatalf("clicked at %q", got)
	}
	if err := tt.Click("Previous change"); err != nil {
		t.Fatal(err)
	}
	if got := where(s); got != "a.go 0" {
		t.Fatalf("previous: %q", got)
	}
}

func TestForcedUnifiedLayoutHidesTheSwitch(t *testing.T) {
	s := New([]File{{Path: "a.go", Texts: changedAt(5)}}, nil)
	s.ForceUnified = true
	tt := tester(s)
	if s.Diff().Layout != editor.DiffUnified {
		t.Fatalf("forced layout %v", s.Diff().Layout)
	}
	for _, label := range []string{"Layout", "Split", "Unified"} {
		if _, shown := tt.Find(label); shown {
			t.Errorf("forced layout still shows %s", label)
		}
	}
	// A reusable state keeps the user's choice when its host stops forcing a layout.
	s.ForceUnified = false
	tt.Frame()
	if s.Diff().Layout != editor.DiffSplit {
		t.Fatalf("remembered layout %v", s.Diff().Layout)
	}
	if err := tt.Click("Unified"); err != nil {
		t.Fatal(err)
	}
	if s.Layout != editor.DiffUnified {
		t.Fatalf("layout switch did not return: %v", s.Layout)
	}
}

// settle runs frames until cond holds: loads finish on their own goroutines.
func settle(t *testing.T, tt *ui.Tester, cond func() bool) {
	t.Helper()
	for range 500 {
		tt.Frame()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestLoaderLoadsShownAndNextFile(t *testing.T) {
	var asked atomic.Int32
	release := make(chan struct{})
	load := func(ctx context.Context, f File) (Texts, error) {
		asked.Add(1)
		<-release
		if f.Path == "bad.go" {
			return Texts{}, fmt.Errorf("no such file")
		}
		return *changedAt(10), nil
	}
	files := []File{{Path: "a.go"}, {Path: "b.go"}, {Path: "bad.go"}, {Path: "d.go"}}
	s := New(files, load)
	tt := tester(s)
	if !tt.HasText("Loading…") {
		t.Fatalf("texts %q", tt.Texts())
	}
	settle(t, tt, func() bool { return asked.Load() == 2 }) // the file shown and the next
	close(release)
	settle(t, tt, func() bool { return s.Diff() != nil })
	if got := where(s); got != "a.go 0" {
		t.Fatalf("loaded at %q", got)
	}
	s.NextFile()
	tt.Frame()
	if s.Diff() == nil || where(s) != "b.go 0" {
		t.Fatalf("the next file was not ready: %q", where(s))
	}
	s.NextFile()
	settle(t, tt, func() bool { return tt.HasText("Could not read the file: no such file") })

	// A new list drops what the old one still loads.
	stale := make(chan struct{})
	s.SetFiles([]File{{Path: "x.go"}}, func(ctx context.Context, f File) (Texts, error) {
		<-stale
		return *changedAt(1), ctx.Err()
	}, 0)
	tt.Frame()
	s.SetFiles([]File{{Path: "y.go", Texts: changedAt(3)}}, nil, 0)
	close(stale)
	tt.Frame()
	if where(s) != "y.go 0" {
		t.Fatalf("after a new list: %q", where(s))
	}
}

func TestOnSelectAndEmptyList(t *testing.T) {
	var got []int
	s := New([]File{{Path: "a.go", Texts: changedAt(1)}, {Path: "b.go", Texts: changedAt(1)}}, nil)
	s.OnSelect = func(i int) { got = append(got, i) }
	tt := tester(s)
	if err := tt.Click("Next file (→)"); err != nil {
		t.Fatal(err)
	}
	_ = tt.Click("Next file (→)") // greyed out at the last file
	tt.Frame()
	s.Select(0)
	if fmt.Sprint(got) != "[1 0]" {
		t.Fatalf("OnSelect %v", got)
	}
	s.SetFiles(nil, nil, 0)
	tt.Frame()
	if s.Current() != -1 || !tt.HasText("Select a file") || !tt.HasText("Its changes appear here") {
		t.Fatalf("empty: current %d, texts %q", s.Current(), tt.Texts())
	}
	tt.Key(0, ui.KeyDown) // nothing to step through
}

// The root takes the keyboard only from the diff's own editor in the frame before: not after a new list, and not after
// frames in which the host did not show the view.
func TestDoesNotStealFocus(t *testing.T) {
	files := []File{{Path: "a.go", Texts: changedAt(1)}}
	s := New(files, nil)
	tt := ui.NewTester(func(c *ui.Context) { s.BeginFrame(); View(c, s) }, 900, 400)
	tt.Frame()
	s.hadFocus = true // the editor had the keyboard
	s.SetFiles([]File{{Path: "b.go"}}, func(ctx context.Context, f File) (Texts, error) {
		<-ctx.Done()
		return Texts{}, ctx.Err()
	}, 0)
	if s.hadFocus {
		t.Fatal("SetFiles kept hadFocus")
	}
	tt.Frame()
	if s.hadFocus {
		t.Error("a loading file took the keyboard")
	}
	s.hadFocus = true
	tt2 := ui.NewTester(func(c *ui.Context) { s.BeginFrame() }, 100, 100)
	tt2.Frame() // a frame without View
	tt2.Frame()
	if s.hadFocus {
		t.Error("hadFocus survived a frame without View")
	}
}

func TestOneSidedFileShowsOneColumnUntilPicked(t *testing.T) {
	s := New([]File{
		{Path: "new.go", Status: "A", Texts: &Texts{New: "a\nb\n"}},
		{Path: "gone.go", Status: "D", Texts: &Texts{Old: "a\nb\n"}},
		{Path: "both.go", Texts: changedAt(5)},
	}, nil)
	s.Layout = editor.DiffSplit
	tt := tester(s)
	for _, want := range []struct {
		file   string
		layout editor.DiffLayout
	}{{"new.go", editor.DiffUnified}, {"gone.go", editor.DiffUnified}, {"both.go", editor.DiffSplit}} {
		tt.Frame()
		if s.files[s.cur].Path != want.file || s.Diff().Layout != want.layout {
			t.Fatalf("%s shown %v, want %s %v", s.files[s.cur].Path, s.Diff().Layout, want.file, want.layout)
		}
		s.NextFile()
	}
	s.Select(0)
	tt.Frame()
	if err := tt.Click("Split"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.Diff().Layout != editor.DiffSplit {
		t.Fatal("picking Split for an added file did not show it side by side")
	}
	s.Select(1)
	tt.Frame()
	if s.Diff().Layout != editor.DiffUnified || s.Layout != editor.DiffSplit {
		t.Fatalf("the pick spread: deleted file %v, setting %v", s.Diff().Layout, s.Layout)
	}
}

func TestTheChangeReachedIsFramed(t *testing.T) {
	s := New([]File{
		{Path: "a.go", Texts: changedAt(10, 13, 16)},
		{Path: "gone.go", Texts: &Texts{Old: "a\nb\nc\n", New: "a\n"}}, // only deletes lines
	}, nil)
	tt := tester(s)
	framed := func(side editor.DiffSide, from, to int) {
		t.Helper()
		tt.Frame()
		d := s.Diff()
		top, bottom, ok := changeSpan(d)
		want := [2]float64{d.LineTop(side, from) - d.ScrollY(), d.LineTop(side, to) - d.ScrollY()}
		if !ok || [2]float64{top, bottom} != want {
			t.Fatalf("at %q framed %v..%v (%v), want %v", where(s), top, bottom, ok, want)
		}
		if _, tinted := d.SelectedLines(); tinted {
			t.Fatal("the frame alone marks the change; its lines are not tinted")
		}
	}
	for _, line := range []int{10, 13, 16} {
		framed(editor.DiffNew, line, line+1)
		s.NextChange()
	}
	tt.Frame()
	if _, _, ok := changeSpan(s.Diff()); ok {
		t.Fatal("a file that only deletes lines is framed")
	}
}

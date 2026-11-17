package diffview

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/view/viewtest"
)

// streamOf is a stream of n files of ten changed lines each, file i in group i/5; lines overrides a file's line count.
func streamOf(n int, lines map[int]int) []StreamFile {
	files := make([]StreamFile, n)
	for i := range files {
		l := 20
		if v, ok := lines[i]; ok {
			l = v
		}
		files[i] = StreamFile{File: File{Path: fmt.Sprintf("f%02d.go", i), Status: "M"}, Key: fmt.Sprint(i), Rev: "1", Group: fmt.Sprint("g", i/5), Lines: l}
	}
	return files
}

func tenLines(prefix string) string {
	var b strings.Builder
	for i := range 10 {
		fmt.Fprintf(&b, "%s %d\n", prefix, i)
	}
	return b.String()
}

// streamTester shows s; loads answer at once with ten lines changed, counting how many run at the same time.
func streamTester(t *testing.T, s *Stream, files []StreamFile, block chan struct{}) (*ui.Tester, func() int) {
	t.Helper()
	var mu sync.Mutex
	running, most := 0, 0
	s.SetFiles(files, func(ctx context.Context, f File) (Texts, error) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		defer func() {
			mu.Lock()
			running--
			mu.Unlock()
		}()
		if block != nil {
			select {
			case <-block:
			case <-ctx.Done():
				return Texts{}, ctx.Err()
			}
		}
		return Texts{Old: tenLines("old"), New: tenLines("new")}, nil
	})
	t.Cleanup(s.Close)
	s.Header = func(c *ui.Context, group string) { ui.Text(c, "header "+group) }
	s.FileHead = func(c *ui.Context, i int) { ui.Text(c, "head "+s.Files()[i].Path) }
	s.Tail = func(c *ui.Context) { ui.Text(c, "the end") }
	tt := ui.NewTester(func(c *ui.Context) { s.View(c).Grow(1) }, 900, 600)
	tt.SetLocale("zh-Hans")
	return tt, func() int {
		mu.Lock()
		defer mu.Unlock()
		return most
	}
}

func until(t *testing.T, tt *ui.Tester, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		tt.Frame()
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s; texts %q", what, tt.Texts())
}

// The stream reads only the files in view, at most MaxLoads at once, and holds at most Keep diffs as it scrolls.
func TestStreamReadsWhatIsInView(t *testing.T) {
	s := NewStream()
	block := make(chan struct{})
	tt, most := streamTester(t, s, streamOf(120, nil), block)
	tt.Frame()
	tt.Frame()
	if s.Requested > MaxLoads {
		t.Errorf("%d loads started at once", s.Requested)
	}
	close(block)
	until(t, tt, "the first screen", func() bool { return s.Loaded() > 0 && !tt.FramePending() })
	first, last := s.List.Visible()
	if s.Requested > last-first+1+2 {
		t.Errorf("the first screen read %d diffs for rows %d…%d", s.Requested, first, last)
	}
	for i := 0; i < len(s.Files()); i += 3 {
		s.ScrollToFile(i)
		tt.Frame()
		tt.Frame()
	}
	until(t, tt, "the end", func() bool { return tt.HasText("head f119.go") })
	if n := s.Loaded(); n > Keep {
		t.Errorf("%d diffs held", n)
	}
	if most() > MaxLoads {
		t.Errorf("%d loads ran at once", most())
	}
}

// A file over FoldLines lines starts folded and is read only once opened; a binary file is a note and never read.
func TestStreamFoldsLargeFilesAndNotesBinaries(t *testing.T) {
	s := NewStream()
	files := streamOf(3, map[int]int{0: 500})
	files[1].Binary = true
	tt, _ := streamTester(t, s, files, nil)
	w := tr.For("zh-Hans")
	until(t, tt, "the third diff", func() bool { return s.Loaded() == 1 })
	if !tt.HasText(w.StreamFolded(500)) || !tt.HasText(w.Binary) {
		t.Fatalf("texts %q", tt.Texts())
	}
	if s.Requested != 1 {
		t.Errorf("%d read; only the small text file should be", s.Requested)
	}
	if err := tt.Click(w.StreamFolded(500)); err != nil {
		t.Fatal(err)
	}
	until(t, tt, "the opened diff", func() bool { return s.Loaded() == 2 && !tt.HasText(w.StreamFolded(500)) })
}

// Next and Prev step file by file, below the pinned header.
func TestStreamSteps(t *testing.T) {
	s := NewStream()
	tt, _ := streamTester(t, s, streamOf(12, nil), nil)
	until(t, tt, "the stream", func() bool { return s.Loaded() > 0 && s.Current() == 0 })
	for want := 1; want <= 7; want++ {
		s.Next()
		until(t, tt, fmt.Sprint("file ", want), func() bool { return s.Current() == want })
	}
	s.Prev()
	until(t, tt, "back", func() bool { return s.Current() == 6 })
	if !tt.HasText("header g1") {
		t.Errorf("no pinned header over file 6: %q", tt.Texts())
	}
}

// longStream shows n files of sixty changed lines each, read at once, three to a group.
func longStream(t *testing.T, n int) (*Stream, *ui.Tester) {
	t.Helper()
	var old, nw strings.Builder
	for i := range 30 {
		fmt.Fprintf(&old, "old %d\n", i)
		fmt.Fprintf(&nw, "new %d\n", i)
	}
	files := make([]StreamFile, n)
	for i := range files {
		files[i] = StreamFile{File: File{Path: fmt.Sprintf("f%02d.go", i), Status: "M", Texts: &Texts{Old: old.String(), New: nw.String()}},
			Key: fmt.Sprint(i), Rev: "1", Group: fmt.Sprint("g", i/3), Lines: 60}
	}
	s := NewStream()
	s.SetFiles(files, nil)
	t.Cleanup(s.Close)
	s.Header = func(c *ui.Context, group string) {
		ui.Text(c, "header "+group).Padding(10).Background(theme.FrameBg).Border(1, theme.Line).BorderWidth(0, 0, 1, 0)
	}
	s.FileHead = func(c *ui.Context, i int) { ui.Text(c, "head "+s.Files()[i].Path).Padding(8) }
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme.Theme())
		s.View(c).Grow(1)
	}, 900, 600)
	return s, tt
}

// heads are the boxes of the file heads showing.
func heads(s *Stream, tt *ui.Tester) map[int]ui.Rect {
	got := map[int]ui.Rect{}
	for i, f := range s.Files() {
		if r, ok := tt.Find("head " + f.Path); ok && r.Y+r.H > 0 && r.Y < 600 {
			got[i] = r
		}
	}
	return got
}

// Scrolled into the middle of a long file, its head stays under the pinned group header.
func TestStreamHoldsTheFileHead(t *testing.T) {
	s, tt := longStream(t, 9)
	s.ScrollToFile(4)
	until(t, tt, "file 4", func() bool { return s.Current() == 4 && !tt.FramePending() })
	tt.Scroll(450, 300, 0, 500)
	until(t, tt, "the middle of file 4", func() bool {
		b := s.boxes[4]
		return !tt.FramePending() && b.Y < s.top-200 && b.Y+b.H > s.top+300
	})
	if s.Current() != 4 {
		t.Fatalf("current %d, want 4", s.Current())
	}
	group, ok := tt.Find("header g1")
	if !ok {
		t.Fatalf("no pinned group header: %q", tt.Texts())
	}
	head, ok := heads(s, tt)[4]
	if !ok {
		t.Fatalf("no head of file 4: %q", tt.Texts())
	}
	if top := group.Y + group.H; head.Y < top || head.Y > top+40 {
		t.Errorf("head of file 4 at %v, group header ends at %v", head.Y, top)
	}
	(&viewtest.Harness{T: t, H: &viewtest.Host{}, TT: tt}).Shot("stream-held-head")
}

// Where one file gives way to the next, the next head pushes the held one out: the two never overlap.
func TestStreamPushesTheHeldHeadOut(t *testing.T) {
	s, tt := longStream(t, 9)
	s.ScrollToFile(4)
	until(t, tt, "file 4", func() bool { return s.Current() == 4 && !tt.FramePending() })
	under := s.top + s.headH
	tt.Scroll(450, 300, 0, s.boxes[4].H-s.fileH/2-(s.boxes[4].Y-under))
	until(t, tt, "file 5 halfway up", func() bool {
		b := s.boxes[5]
		return !tt.FramePending() && b.Y > under && b.Y < under+s.fileH
	})
	hs := heads(s, tt)
	held, next := hs[4], hs[5]
	if next.H == 0 {
		t.Fatalf("no head of file 5: %v", hs)
	}
	group, _ := tt.Find("header g1")
	if held.H > 0 && held.Y < group.Y+group.H-held.H {
		t.Errorf("head of file 4 at %v shows over the group header ending at %v", held.Y, group.Y+group.H)
	}
	if held.H > 0 && held.Y+held.H > next.Y {
		t.Errorf("heads overlap: file 4's ends at %v, file 5's starts at %v", held.Y+held.H, next.Y)
	}
	for i, r := range hs {
		if i != 4 && i != 5 && r.Y < next.Y {
			t.Errorf("head of file %d at %v above file 5's", i, r.Y)
		}
	}
	if s.Current() != 5 {
		t.Errorf("current %d, want 5: only file 5 is in view below its pushed head", s.Current())
	}
	(&viewtest.Harness{T: t, H: &viewtest.Host{}, TT: tt}).Shot("stream-head-pushed")
}

// A diff already on screen redraws in the code font chosen after it loaded.
func TestStreamFollowsTheCodeFont(t *testing.T) {
	t.Cleanup(func() { theme.SetCodeFont("") })
	s := NewStream()
	tt, _ := streamTester(t, s, streamOf(1, nil), nil)
	until(t, tt, "the diff", func() bool { it := s.item(0); return it != nil && it.diff != nil })
	theme.SetCodeFont("Menlo")
	tt.Frame()
	if got := s.item(0).diff.Font.Family; got != theme.CodeFont() {
		t.Errorf("loaded diff draws in %q, want %q", got, theme.CodeFont())
	}
}

// A file folded by hand shows only its head and is not read again; opened, it comes back. The choice beats FoldLines
// both ways and stays while the file is listed.
func TestStreamFoldsByHand(t *testing.T) {
	s := NewStream()
	files := streamOf(2, map[int]int{1: 500})
	tt, _ := streamTester(t, s, files, nil)
	w := tr.For("zh-Hans")
	until(t, tt, "the small diff", func() bool { return s.Loaded() == 1 && tt.HasText(w.StreamDiff(files[0].Path)) })
	if s.Folded(0) || !s.Folded(1) {
		t.Fatalf("folded %v %v at the start", s.Folded(0), s.Folded(1))
	}
	s.SetFolded(0, true)
	s.SetFolded(1, false)
	until(t, tt, "the swap", func() bool {
		return !tt.HasText(w.StreamDiff(files[0].Path)) && tt.HasText(w.StreamDiff(files[1].Path)) && !tt.HasText(w.StreamFolded(500))
	})
	s.SetFiles(files, s.load)
	tt.Frame()
	if !s.Folded(0) || s.Folded(1) {
		t.Errorf("folded %v %v after the same list came again", s.Folded(0), s.Folded(1))
	}
	s.SetFiles(files[1:], s.load)
	s.SetFiles(files, s.load)
	if s.Folded(0) || s.Folded(1) {
		t.Errorf("folded %v %v after the first file left and came back", s.Folded(0), s.Folded(1))
	}
}

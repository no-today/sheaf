package diffview

import (
	"context"
	"fmt"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

// StreamFile is one file of a Stream.
type StreamFile struct {
	File
	// Key names the file across lists (its group and path): what was loaded for it stays while Key and Rev do.
	Key string
	// Rev changes when the file's diff may have: a new compare target, new line counts.
	Rev string
	// Group is the heading the file goes under; the files of a group come together.
	Group string
	// Lines is how many lines it adds and deletes, -1 while unknown: over FoldLines the file starts folded, and it gives a
	// file not read yet about its height.
	Lines int
	// Binary is known without reading the file.
	Binary bool
}

// The limits of a Stream: files over FoldLines changed lines start folded; at most MaxLoads files read at once, and the
// diffs of at most Keep files are held.
const (
	FoldLines = 400
	MaxLoads  = 4
	Keep      = 40
)

// Stream is the diffs of many files, of several groups (repos), in one scrolling list: a heading per group that stays at
// the top while its files scroll under it, and every file's diff as high as its lines. Only the files in view are read,
// a few at a time; diffs far from the view are let go and read again when scrolled back to.
type Stream struct {
	List ui.ListState
	// Header builds a group heading, with a background when pinned.
	Header func(c *ui.Context, group string)
	// Sticky selects the group headings that pin; nil pins every heading.
	Sticky func(group string) bool
	// FileHead builds the head of file i, above its diff.
	FileHead func(c *ui.Context, i int)
	// Tail, when set, builds a row after the last file.
	Tail func(c *ui.Context)
	// Requested counts the loads started, for tests and benchmarks.
	Requested int

	files   []StreamFile
	rows    []streamRow
	fileRow []int
	// Where the last frame drew the list, its pinned header, a file's head and each file, for the file at the top: the
	// rows Visible reports include the one under the pinned header.
	top, headH  float32
	fileH       float32
	boxes       map[int]ui.Rect
	jump        int // the row ScrollToFile or ScrollToGroup went to, -1 once its neighbours are read
	settling    int // frames the jump has been held since the diffs in view were read
	load        Loader
	nextRequest uint64
	items       map[string]*streamItem
	paused      bool
	fold        map[string]bool // by Key, what the user chose: folded or open, over the FoldLines default
	ctx         context.Context
	cancel      context.CancelFunc
	theme       *Theme

	mu      sync.Mutex
	arrived []streamArrival
}

type streamRow struct {
	group string
	file  int // -1 a heading, -2 the tail
}

type streamItem struct {
	rev      string
	request  uint64 // unique to this load, including when the item is reused
	pending  bool
	cancel   context.CancelFunc
	ready    bool
	err      error
	texts    Texts // without Parts' patches once the diff is built
	diff     *editor.DiffState
	titles   []string
	bins     []string
	height   float32 // of its diff, once known: kept while the diff is let go
	resolved bool
}

type streamArrival struct {
	request uint64
	key     string
	texts   Texts
	err     error
}

func (it *streamItem) stopLoad() {
	if it.cancel != nil {
		it.cancel()
	}
	it.request, it.pending, it.cancel = 0, false, nil
}

// NewStream makes an empty stream.
func NewStream() *Stream {
	s := &Stream{items: map[string]*streamItem{}, fold: map[string]bool{}, theme: streamTheme(), jump: -1}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.List.Key = func(i int) any {
		if i >= len(s.rows) {
			return i
		}
		r := s.rows[i]
		switch r.file {
		case -1:
			return "group\x00" + r.group
		case -2:
			return "tail"
		}
		return "file\x00" + s.files[r.file].Key
	}
	s.List.Header = func(i int) bool {
		return i < len(s.rows) && s.rows[i].file == -1 && (s.Sticky == nil || s.Sticky(s.rows[i].group))
	}
	return s
}

// streamTheme is DefaultTheme over the window's background.
func streamTheme() *Theme {
	t := DefaultTheme()
	t.Background, t.Part = theme.FrameBg, theme.FrameBg
	t.Editor.Background = theme.FrameBg
	return t
}

// SetFiles shows files, read with load. What was read for a file whose Key and Rev stay is kept; loads of files that left
// are stopped.
func (s *Stream) SetFiles(files []StreamFile, load Loader) {
	same := len(files) == len(s.files)
	for i := 0; same && i < len(files); i++ {
		same = files[i].Key == s.files[i].Key
	}
	if !same {
		s.jump = -1
	}
	s.files, s.load = files, load
	s.rows, s.fileRow = s.rows[:0], make([]int, len(files))
	keep, shown := map[string]bool{}, map[string]bool{}
	group := "\x00none"
	for i, f := range files {
		shown[f.Key] = true
		if f.Group != group {
			group = f.Group
			s.rows = append(s.rows, streamRow{group: group, file: -1})
		}
		s.fileRow[i] = len(s.rows)
		s.rows = append(s.rows, streamRow{group: group, file: i})
		if it := s.items[f.Key]; it != nil && it.rev == f.Rev {
			keep[f.Key] = true
		}
	}
	if s.Tail != nil && len(files) > 0 {
		s.rows = append(s.rows, streamRow{file: -2})
	}
	for k := range s.fold {
		if !shown[k] {
			delete(s.fold, k)
		}
	}
	for k, it := range s.items {
		if !keep[k] {
			it.stopLoad()
			delete(s.items, k)
		}
	}
}

// Close stops the loads still running.
func (s *Stream) Close() {
	s.cancel()
	for _, it := range s.items {
		it.stopLoad()
	}
}

// Files is the list shown.
func (s *Stream) Files() []StreamFile { return s.files }

// Loaded counts the files whose diff is held.
func (s *Stream) Loaded() int {
	n := 0
	for _, it := range s.items {
		if it.diff != nil {
			n++
		}
	}
	return n
}

func (s *Stream) SetPaused(paused bool) {
	if s.paused == paused {
		return
	}
	s.paused = paused
	if paused {
		for _, it := range s.items {
			if it.pending {
				it.stopLoad()
			}
		}
	}
}

// Stats are the lines file i adds and deletes, once its diff was read.
func (s *Stream) Stats(i int) (add, del int, ok bool) {
	if it := s.item(i); it != nil && it.diff != nil {
		add, del = it.diff.Stats()
		return add, del, true
	}
	return 0, 0, false
}

// Resolved tells that file i's diff, once read, has a part of a merge edited while merging (range diffs only).
func (s *Stream) Resolved(i int) bool {
	it := s.item(i)
	return it != nil && it.resolved
}

// Folded tells that file i shows only its head: the user folded it, or it is over FoldLines and not opened.
func (s *Stream) Folded(i int) bool {
	return i >= 0 && i < len(s.files) && s.folded(s.files[i], s.item(i))
}

// SetFolded folds or opens file i; the choice stays while the file is listed.
func (s *Stream) SetFolded(i int, folded bool) {
	if i >= 0 && i < len(s.files) {
		s.fold[s.files[i].Key] = folded
	}
}

// SetAllFolded folds or opens every file listed, and keeps file i at the top so the view stays where the user looks.
func (s *Stream) SetAllFolded(folded bool, i int) {
	for _, f := range s.files {
		s.fold[f.Key] = folded
	}
	s.ScrollToFile(i)
}

func (s *Stream) item(i int) *streamItem {
	if i < 0 || i >= len(s.files) {
		return nil
	}
	return s.items[s.files[i].Key]
}

// ScrollToFile shows file i at the top; ScrollToGroup the heading of group.
func (s *Stream) ScrollToFile(i int) {
	if i >= 0 && i < len(s.fileRow) {
		s.jumpTo(s.fileRow[i])
	}
}

// ScrollToTop shows the start of the stream.
func (s *Stream) ScrollToTop() {
	s.jump, s.settling = -1, 0
	s.List.ScrollTo(0, ui.Start)
}

// jumpTo scrolls to row, and keeps it at the top while the diffs around it arrive and change their heights.
func (s *Stream) jumpTo(row int) {
	s.jump = row
	s.List.ScrollTo(row, ui.Start)
}

// holdJump scrolls to the row jumped to again while a diff in view is still being read.
func (s *Stream) holdJump(svc ui.Services, first, last int) {
	if s.jump < 0 {
		return
	}
	if s.jump >= len(s.rows) {
		s.jump = -1
		return
	}
	s.List.ScrollTo(s.jump, ui.Start)
	for r := max(first, 0); r <= last && r < len(s.rows); r++ {
		if f := s.rows[r].file; f >= 0 {
			if it := s.items[s.files[f].Key]; it != nil && it.pending {
				s.settling = 0
				return
			}
		}
	}
	// A diff takes its height a frame or two after it arrives (the viewer measures what it fits): hold on as long.
	if s.settling++; s.settling < 3 {
		svc.Invalidate()
		return
	}
	s.jump, s.settling = -1, 0
}

func (s *Stream) ScrollToGroup(group string) {
	for i, r := range s.rows {
		if r.file == -1 && r.group == group {
			s.jumpTo(i)
			return
		}
	}
}

// Current is the file at the top of the view, below the pinned header: the first with more in view than its held
// head, -1 when there is none.
func (s *Stream) Current() int {
	first, last := s.List.Visible()
	for i := max(first, 0); i <= last && i < len(s.rows); i++ {
		f := s.rows[i].file
		if f < 0 {
			continue
		}
		if b, ok := s.boxes[f]; !ok || b.H == 0 || b.Y+b.H > s.top+s.headH+max(s.fileH, 8) {
			return f
		}
	}
	return -1
}

// Next and Prev scroll to the file after or before the one at the top.
func (s *Stream) Next() { s.ScrollToFile(s.Current() + 1) }

func (s *Stream) Prev() {
	if cur := s.Current(); cur > 0 {
		s.ScrollToFile(cur - 1)
	}
}

// View builds the stream.
func (s *Stream) View(c *ui.Context) ui.Element {
	s.theme.Font.Family = theme.CodeFont() // the setting may have changed since the stream was made
	s.settle()
	first, last := s.List.Visible()
	s.schedule(c.Services(), first, last)
	s.holdJump(c.Services(), first, last)
	w := tr.Of(c)
	stuck := s.stuck()
	boxes := make(map[int]ui.Rect, last-first+3)
	l := ui.List(c.Key("stream"), &s.List, len(s.rows), func(i int) {
		r := s.rows[i]
		switch r.file {
		case -1:
			if s.Header != nil {
				hd := ui.Column(c).FillWidth().AlignItems(ui.Stretch).Children(func() { s.Header(c, r.group) })
				if b := hd.Bounds(); b.H > 0 && (s.Sticky == nil || s.Sticky(r.group)) {
					s.headH = b.H
				}
			}
		case -2:
			s.Tail(c)
		default:
			col := ui.Column(c).FillWidth().AlignItems(ui.Stretch).Children(func() {
				if s.FileHead != nil {
					s.fileHead(c, r.file, stuck == r.file)
				}
				s.body(c, w, r.file)
			})
			boxes[r.file] = col.Bounds()
		}
	})
	s.top, s.boxes = l.Bounds().Y, boxes
	return l
}

// fileHead builds the head of file i, held under the pinned group heading while the rest of its file is in view: its
// file's box bounds it, so the next file pushes it out. The line under it shows only while it is held.
func (s *Stream) fileHead(c *ui.Context, i int, stuck bool) {
	line := theme.FrameBg
	if stuck {
		line = theme.Line
	}
	hd := ui.Column(c).FillWidth().AlignItems(ui.Stretch).Background(theme.FrameBg).Border(1, line).BorderWidth(0, 0, 1, 0).
		Sticky().Top(s.headH).Children(func() { s.FileHead(c, i) })
	if b := hd.Bounds(); b.H > 0 {
		s.fileH = b.H
	}
}

// stuck is the file whose head the last frame held under the pinned group heading, -1 for none.
func (s *Stream) stuck() int {
	under := s.top + s.headH
	for f, b := range s.boxes {
		if b.H > 0 && b.Y < under-0.5 && b.Y+b.H > under {
			return f
		}
	}
	return -1
}

// streamInset is where the code of a diff starts: the hunk heads and notes line up with it.
const streamInset = 104

func (s *Stream) note(c *ui.Context, text string, color ui.Color) ui.Element {
	return ui.Text(c, text).FontSize(theme.FSSmall).TextColor(color).SingleLine().Padding(6, 20, 6, streamInset)
}

func (s *Stream) body(c *ui.Context, w *words, i int) {
	f := s.files[i]
	it := s.items[f.Key]
	switch {
	case f.Binary || it != nil && it.ready && it.texts.Binary:
		s.note(c, w.Binary, theme.Text4)
		return
	case s.fold[f.Key]:
		return
	case s.folded(f, it):
		lines := f.Lines
		if it != nil && it.diff != nil {
			a, d := it.diff.Stats()
			lines = a + d
		}
		label := w.StreamFolded(lines)
		b := ui.ButtonBase(c.Key("fold\x00" + f.Key)).Justify(ui.Start).Label(label).Cursor(ui.CursorPointer)
		b.Children(func() {
			color := theme.Text4
			if b.Hovered() {
				color = theme.Text
			}
			s.note(c, label, color)
		})
		if b.Clicked() {
			s.SetFolded(i, false)
			c.Invalidate()
		}
		return
	case it != nil && it.err != nil:
		s.note(c, w.Failed(it.err), theme.Del)
		return
	case it == nil || it.diff == nil:
		h := s.estimate(f, it)
		ui.Column(c).Height(h).FillWidth().Children(func() {
			label := w.Loading
			if s.paused {
				label = w.RefreshToLoad
			}
			s.note(c, label, theme.Text4)
		})
		return
	}
	if it.texts.Cut {
		s.note(c, w.Cut, theme.Text4)
	}
	if it.texts.More > 0 {
		s.note(c, w.More(it.texts.More), theme.Text4)
	}
	if len(it.bins) > 0 {
		s.note(c, w.BinaryParts(it.bins), theme.Text4)
	}
	d := it.diff
	if len(d.Hunks()) == 0 {
		s.note(c, w.Unchanged, theme.Text4)
		return
	}
	d.RenderSeparator = s.separator(it.titles)
	d.Font = s.theme.Font // loaded diffs keep up with a changed code font
	editor.DiffView(c, d).FillWidth().Label(w.StreamDiff(f.Path))
}

// folded: as the user chose, else over FoldLines changed lines, known or read.
func (s *Stream) folded(f StreamFile, it *streamItem) bool {
	if v, ok := s.fold[f.Key]; ok {
		return v
	}
	if f.Lines > FoldLines {
		return true
	}
	if it != nil && it.diff != nil {
		a, d := it.diff.Stats()
		return a+d > FoldLines
	}
	return false
}

// estimate is the height a diff not read yet takes: as before when it was read once, else from its line count, so
// the list moves little as diffs arrive.
func (s *Stream) estimate(f StreamFile, it *streamItem) float32 {
	if it != nil && it.height > 0 {
		return it.height
	}
	lines := 6
	if f.Lines >= 0 {
		lines = f.Lines + 6
	}
	return float32(lines)*theme.HLine + 32
}

// heightOf is the height a diff of patch p takes: its lines, and a 32-DIP separator over each hunk.
func heightOf(p editor.FilePatch) float32 {
	var h float32
	for _, hk := range p.Hunks {
		h += 32 + float32(len(hk.Lines))*theme.HLine
	}
	return h
}

// separator draws a hunk's head in the faint small type of the stream, lined up with the code, and a part's title above
// its first hunk.
func (s *Stream) separator(titles []string) func(c *ui.Context, sep ui.Element, x *editor.DiffSeparator) {
	return func(c *ui.Context, sep ui.Element, x *editor.DiffSeparator) {
		if x.Lines > 0 {
			foldSeparator(c, sep, x)
			return
		}
		title, rest, ok := partOf(x.Header, titles)
		sep.Children(func() {
			ui.Row(c).Grow(1).AlignSelf(ui.Stretch).AlignItems(ui.Center).Gap(12).PaddingStart(streamInset).PaddingEnd(20).Children(func() {
				if ok {
					ui.Text(c, title).FontSize(theme.FSSmall).TextColor(theme.Text).SingleLine().Shrink(1).MinWidth(0)
				} else {
					rest = x.Header
				}
				ui.Text(c, rest).FontSize(theme.FSSmall).TextColor(theme.Faint).SingleLine().Shrink(2).MinWidth(0)
			})
		})
	}
}

// schedule starts the loads of the files in view (rows first to last of the last layout, and one beyond each end), at
// most MaxLoads at once; stops those far out of view, and lets go of diffs past Keep, the farthest first.
func (s *Stream) schedule(svc ui.Services, first, last int) {
	if s.paused || len(s.rows) == 0 {
		return
	}
	first, last = max(first-1, 0), min(last+1, len(s.rows)-1)
	running := 0
	for i := range s.files {
		it := s.items[s.files[i].Key]
		if it == nil || !it.pending {
			continue
		}
		if r := s.fileRow[i]; r < first-8 || r > last+8 {
			it.stopLoad()
			delete(s.items, s.files[i].Key)
			continue
		}
		running++
	}
	for r := first; r <= last && running < MaxLoads; r++ {
		i := s.rows[r].file
		if i < 0 {
			continue
		}
		f := s.files[i]
		it := s.items[f.Key]
		if f.Binary || it != nil && (it.pending || it.ready) || s.folded(f, it) {
			continue
		}
		s.fetch(svc, i, it)
		running++
	}
	s.evict(first, last)
}

func (s *Stream) fetch(svc ui.Services, i int, it *streamItem) {
	f := s.files[i]
	if it == nil {
		it = &streamItem{}
		s.items[f.Key] = it
	}
	it.stopLoad()
	s.nextRequest++
	it.rev, it.request, it.pending = f.Rev, s.nextRequest, true
	s.Requested++
	if f.Texts != nil {
		s.apply(f.Key, it, *f.Texts, nil)
		return
	}
	request := it.request
	// Each file is a caller of its own: the stream reads several at once, and a session call cancels the same caller's
	// previous call of its kind. The stream stops its own loads.
	owner := fmt.Sprintf("%p", s)
	ctx, cancel := context.WithCancel(session.Caller(context.WithValue(s.ctx, ownerKey{}, owner), "stream "+owner+"\x00"+f.Key))
	it.cancel = cancel
	load, key := s.load, f.Key
	go func() {
		defer cancel()
		t, err := load(ctx, f.File)
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		s.arrived = append(s.arrived, streamArrival{request, key, t, err})
		s.mu.Unlock()
		svc.Invalidate()
	}()
}

// evict lets go of the diffs farthest from rows first to last while more than Keep are held.
func (s *Stream) evict(first, last int) {
	held := 0
	for _, it := range s.items {
		if it.diff != nil {
			held++
		}
	}
	for held > Keep {
		far, farKey := -1, ""
		for i, f := range s.files {
			it := s.items[f.Key]
			if it == nil || it.diff == nil {
				continue
			}
			r := s.fileRow[i]
			d := max(first-r, r-last)
			if d > far {
				far, farKey = d, f.Key
			}
		}
		if far <= 0 {
			return
		}
		it := s.items[farKey]
		it.stopLoad()
		it.diff, it.ready, it.titles, it.bins = nil, false, nil, nil
		held--
	}
}

func (s *Stream) settle() {
	s.mu.Lock()
	got := s.arrived
	s.arrived = nil
	s.mu.Unlock()
	for _, a := range got {
		if it := s.items[a.key]; it != nil && it.pending && it.request == a.request {
			s.apply(a.key, it, a.texts, a.err)
		}
	}
}

func (s *Stream) apply(key string, it *streamItem, t Texts, err error) {
	it.stopLoad()
	it.ready, it.err = true, err
	it.texts = Texts{Binary: t.Binary, Cut: t.Cut, More: t.More}
	if err != nil || t.Binary {
		return
	}
	var path string
	for _, f := range s.files {
		if f.Key == key {
			path = f.Path
			break
		}
	}
	d := &editor.DiffState{
		Layout: editor.DiffUnified, FitContent: true, LineSelection: true, NoScrollbars: true,
		Inline: editor.DiffInlineNone, Indicators: editor.DiffClassic,
		Font: s.theme.Font, LineHeight: s.theme.LineHeight, Theme: s.theme.Editor,
		Bindings: navBindings(),
	}
	if len(t.Parts) > 0 {
		var p editor.FilePatch
		p, it.titles = patchOf(path, t.Parts)
		d.SetPatch(p)
		it.bins = binaryParts(t.Parts)
		it.height = heightOf(p)
		it.resolved = t.Resolved
	} else {
		old, nw := editor.NewBuffer(t.Old), editor.NewBuffer(t.New)
		old.SetLanguage(editor.LanguageFor(path))
		nw.SetLanguage(editor.LanguageFor(path))
		d.Old, d.New = old, nw
	}
	it.diff = d
}

package diffview

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

// Grouper puts a file under a heading of a Tree: key tells the headings apart, "" for none (the
// file's directory, or the file, sits at the top); name is what the heading shows.
type Grouper func(f File) (key, name string)

// ByRepo groups the files of FilesOf by repo (FileChange.Repo), as the log's commit tree.
func ByRepo(f File) (string, string) {
	fc, _ := f.Source.(session.FileChange)
	return fc.Repo, fc.Repo
}

// Tree is a file tree's state, kept by the page across frames: files under their group (a repo, a filter's row), the
// files of a group under their directory, one row with the whole path (a chain of single subdirectories is one row
// anyway), the files at a group's root right under it. Up and Down move through the rows, Right opens a heading or goes
// into it, Left closes it or goes to the heading above (with Option: all inside).
type Tree struct {
	// Selected is the file chosen: an index into the files given to FileTree, -1 for none. When it changes (Preview
	// Diff stepped to another file), the tree opens the headings over that file and moves its choice there.
	Selected *int
	// OnSelect is called as the user chooses file i (a click, the keys); choosing a heading leaves the file as it is.
	OnSelect func(i int)
	// Group puts the files under headings; nil: none.
	Group Grouper
	// Note, when set, is small grey text at the end of a file's row (the area of an uncommitted change), "" for none.
	Note func(f File) string
	// Row, when set, is called inside each file's row after what the tree shows, with the row: add to its end (a badge),
	// give it a context menu (widgets.ContextMenu), act on its double click (RowDoubleClicked).
	Row func(c *ui.Context, row ui.Element, i int)
	// Flat shows the full paths under their group, without directories.
	Flat bool
	// KeepClosed keeps headings closed across file lists on the filter page; without it another list opens every
	// heading, as the log does when a different commit is selected.
	KeepClosed bool

	outline ui.OutlineState[node]
	row     int // the outline's choice
	own     int // Selected when the page gives none
	shape   shape
	files   []File // the last frame's, which make shape
	flat    bool
	built   bool
	file    int           // *Selected as the tree last showed it
	item    node          // the item chosen as the tree last saw it
	closed  map[node]bool // headings the user closed, kept from list to list
	known   int           // the row of item as the tree last set or saw it; another is the user moving the choice
	shown   [2]string     // the group and path of the file chosen, to tell it in another list
}

type kind uint8

const (
	groupNode kind = iota + 1
	dirNode
	fileNode
)

// node is a row of a Tree: a group's heading, a directory of a group, or file i.
type node struct {
	kind  kind
	group string
	dir   string
	file  int
}

// shape is the tree of a list of files.
type shape struct {
	roots []node
	kids  map[node][]node // the headings' rows; files have none
	up    map[node]node   // the heading over a row, none for those at the top
	names map[string]string
	count map[string]int // files of a group
	heads []node
	leaf  []node // file i's row
}

// shapeOf builds groups, directories and files in order of first appearance, preserving TreeOrder.
func shapeOf(files []File, group Grouper, flat bool) shape {
	s := shape{kids: map[node][]node{}, up: map[node]node{}, names: map[string]string{}, count: map[string]int{}, leaf: make([]node, len(files))}
	var groups []string
	dirs := map[string][]string{}
	in := map[[2]string][]node{}
	for i, f := range files {
		g, name := "", ""
		if group != nil {
			g, name = group(f)
			s.names[g] = name
		}
		if _, seen := dirs[g]; !seen {
			groups = append(groups, g)
			dirs[g] = []string{}
		}
		s.count[g]++
		d := ""
		if !flat {
			d, _ = split(f.Path)
		}
		k := [2]string{g, d}
		if _, seen := in[k]; !seen {
			dirs[g] = append(dirs[g], d)
		}
		s.leaf[i] = node{kind: fileNode, group: g, dir: d, file: i}
		in[k] = append(in[k], s.leaf[i])
	}
	for _, g := range groups {
		head := node{kind: groupNode, group: g}
		var rows []node
		for _, d := range dirs[g] {
			fs := in[[2]string{g, d}]
			over := head
			if d != "" {
				dn := node{kind: dirNode, group: g, dir: d}
				s.kids[dn] = fs
				s.heads = append(s.heads, dn)
				rows = append(rows, dn)
				over = dn
			} else {
				rows = append(rows, fs...)
			}
			for _, f := range fs {
				s.up[f] = over
			}
		}
		if g == "" {
			for _, r := range rows {
				if up := s.up[r]; up.kind == groupNode {
					delete(s.up, r)
				}
			}
			s.roots = append(s.roots, rows...)
			continue
		}
		for _, r := range rows {
			if r.kind == dirNode {
				s.up[r] = head
			}
		}
		s.kids[head] = rows
		s.heads = append(s.heads, head)
		s.roots = append(s.roots, head)
	}
	return s
}

// children is the outline's: a heading's rows, nil for a file.
func (s *shape) children(n node) []node { return s.kids[n] }

// rows lists the rows the outline shows when open tells which headings are open, as ui.Outline flattens them.
func (s *shape) rows(open func(node) bool) []node {
	var out []node
	var walk func([]node)
	walk = func(ns []node) {
		for _, n := range ns {
			out = append(out, n)
			if kids := s.kids[n]; kids != nil && open(n) {
				walk(kids)
			}
		}
	}
	walk(s.roots)
	return out
}

func split(p string) (dir, name string) {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "", p
	}
	return p[:i], p[i+1:]
}

// TreeOrder is files in the order a Tree grouped by group shows them: by group, then directory, each in order of first
// appearance. Give Preview Diff and the Tree the files in this order, so stepping through the files follows the tree.
func TreeOrder(files []File, group Grouper) []File {
	s := shapeOf(files, group, false)
	out := make([]File, 0, len(files))
	for _, n := range s.rows(func(node) bool { return true }) {
		if n.kind == fileNode {
			out = append(out, files[n.file])
		}
	}
	return out
}

// ExpandAll opens every heading, also those of other lists closed before.
func (t *Tree) ExpandAll() {
	clear(t.closed)
	for _, h := range t.shape.heads {
		t.outline.Open.Add(h)
	}
}

// CollapseAll closes every heading of the list, and only those; the choice moves to the heading at the top over it.
func (t *Tree) CollapseAll() {
	t.outline.Open.Clear()
	t.closed = map[node]bool{}
	for _, h := range t.shape.heads {
		t.closed[h] = true
	}
	if t.row >= 0 && t.row < t.outline.Rows() {
		t.moveTo(t.outline.Item(t.row), false)
	}
}

// moveTo makes n the outline's choice. With reveal, the headings over it open; else
// the choice is the outermost closed heading over it, when one hides it.
func (t *Tree) moveTo(n node, reveal bool) {
	at := n
	for up, ok := t.shape.up[n]; ok; up, ok = t.shape.up[up] {
		switch {
		case reveal:
			t.outline.Open.Add(up)
		case !t.outline.Open.Has(up):
			at = up
		}
	}
	n = at
	t.row = -1
	for i, r := range t.shape.rows(t.outline.Open.Has) {
		if r == n {
			t.row = i
			break
		}
	}
	t.item, t.known = n, t.row
}

func (t *Tree) selected() *int {
	if t.Selected == nil {
		return &t.own
	}
	return t.Selected
}

// sync rebuilds the shape for another list of files, every heading open but, with KeepClosed, those the user closed;
// and it follows a file chosen elsewhere, opening the headings over it and scrolling to it. In another list it
// scrolls only when the chosen file or its row changed, and opens nothing.
func (t *Tree) sync(files []File) {
	if !t.built {
		t.built, t.own = true, -1
		t.outline.List.Selected = &t.row
		// Typing a row's first letters chooses it; screen readers read it.
		t.outline.List.Label = func(i int) string { return t.label(t.outline.Item(i)) }
		t.flat = !t.Flat // so the shape is built below
	}
	sel := t.selected()
	rebuild := t.flat != t.Flat || !t.fits(files)
	t.files = files // what the rows show, also of a list rebuilt every frame
	reveal := true
	if rebuild {
		if t.closed == nil || !t.KeepClosed {
			t.closed = map[node]bool{}
		}
		for _, h := range t.shape.heads {
			switch {
			case !t.KeepClosed:
			case t.outline.Open.Has(h):
				delete(t.closed, h)
			default:
				t.closed[h] = true
			}
		}
		t.flat = t.Flat
		t.shape = shapeOf(files, t.Group, t.Flat)
		t.outline.Open.Clear()
		for _, h := range t.shape.heads {
			if !t.closed[h] {
				t.outline.Open.Add(h)
			}
		}
		t.file, reveal = -2, false // follow *sel below, into what stays closed
	}
	if *sel == t.file {
		return
	}
	t.file = *sel
	if *sel >= 0 && *sel < len(files) {
		leaf := t.shape.leaf[*sel]
		was, row := t.shown, t.row
		t.shown = [2]string{leaf.group, files[*sel].Path}
		t.moveTo(leaf, reveal)
		if t.row >= 0 && (reveal || t.shown != was || t.row != row) {
			t.outline.List.ScrollIntoView(t.row)
		}
	} else {
		t.row, t.item, t.known, t.shown = -1, node{}, -1, [2]string{}
	}
}

// picked notes the outline's choice after the user moved it: a file becomes the file chosen. The rows are the last
// built: the keys move the choice among them before the frame builds, a click as the outline builds.
func (t *Tree) picked() {
	t.known = t.row
	if t.row < 0 || t.row >= t.outline.Rows() {
		return
	}
	n := t.outline.Item(t.row)
	if n == t.item {
		return
	}
	t.item = n
	if sel := t.selected(); n.kind == fileNode && n.file != *sel {
		*sel, t.file = n.file, n.file
		t.shown = [2]string{n.group, t.files[n.file].Path}
		if t.OnSelect != nil {
			t.OnSelect(n.file)
		}
	}
}

// fits tells whether files make the tree t shows: the same paths under the same headings, in the same order.
func (t *Tree) fits(files []File) bool {
	if len(files) != len(t.files) {
		return false
	}
	for i, f := range files {
		g := ""
		if t.Group != nil {
			g, _ = t.Group(f)
		}
		if f.Path != t.files[i].Path || g != t.shape.leaf[i].group {
			return false
		}
	}
	return true
}

// The outline (mygo ui/outline.go) pads each row by the theme's Space(1) above and below and Space(2) at the sides,
// indents it Space(4) a level, gives it a Space(4) arrow and leaves Space(1) between these; treeRowH is the height left for
// a row's content. Its chosen row has the list theme's corners.
var (
	rowPadY   = theme.Theme().Space(1)
	rowPadX   = theme.Theme().Space(2)
	rowIndent = theme.Theme().Space(4)
	rowArrow  = theme.Theme().Space(4)
	rowGap    = theme.Theme().Space(1)
	rowRadius = theme.ListTheme().Radius
	treeRowH  = theme.HTree - 2*rowPadY
)

// FileTree builds the tree of files. Another list (other paths or headings) opens every heading again; the same list
// built anew each frame, as a filter of a page's files, keeps what is open.
func FileTree(c *ui.Context, t *Tree, files []File) ui.Element {
	t.sync(files)
	if t.row != t.known {
		t.picked() // the keys moved it since the last frame: the rows show the file it chose
	}
	w := tr.Of(c)
	return ui.Themed(c, theme.ListTheme()).Background(ui.Transparent).Children(func() {
		ui.Outline(c, &t.outline, t.shape.roots, t.shape.children, func(n node) {
			switch n.kind {
			case groupNode:
				name := t.shape.names[n.group]
				ui.Row(c).Height(treeRowH).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Tooltip(name).Children(func() {
					ui.Text(c, name).FontSize(theme.FSBody).FontWeight(600).TextColor(theme.TextStrong).SingleLine().Shrink(1).MinWidth(0)
					ui.Spacer(c)
					ui.Text(c, w.NFiles(t.shape.count[n.group])).FontSize(theme.FSLabel).FontFeatures("tnum").TextColor(theme.RowText(n == t.item, theme.Text4)).SingleLine().Shrink(0)
				})
			case dirNode:
				ui.Row(c).Height(treeRowH).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Tooltip(n.dir).Children(func() {
					ui.Icon(c, theme.IconFolder).FontSize(theme.IconS).TextColor(theme.Text5).Shrink(0)
					tailText(c, n.dir, theme.FSBody, theme.Text4).Height(treeRowH)
				})
			default:
				t.fileRow(c, w, n)
			}
		}).Grow(1).Padding(0, 6).Label(w.Tree).Changed() // applies this pass's input to the choice
		t.picked()
	})
}

// look is how file row n shows: its name in the status colour, the file shown too, so what happened to it stays visible;
// the file shown keeps a dim background while the choice is on another row (the heading above it, after Left or
// Collapse all).
func (t *Tree) look(n node) (name, background ui.Color) {
	name = theme.Status(t.files[n.file].Status)
	if st := t.files[n.file].Status; st == "M" || st == "" {
		name = theme.Text // most files are modified: only the unusual ones take a colour
	}
	if n.file != *t.selected() || n == t.item {
		return name, ui.Transparent // n == t.item: on the outline's chosen row, which draws its own background
	}
	return name, theme.SelDim
}

// fileRow is a file's row: only its name takes the status colour (none for a modified file), then ×N for a file several picked commits changed,
// its lines added and removed, and the note. It reaches over the outline row's padding, indent and (empty) arrow, so the
// background of the file shown covers the row as the outline's choice does.
func (t *Tree) fileRow(c *ui.Context, w *words, n node) {
	f := t.files[n.file]
	add, del, binary, stat := statOf(f)
	color, bg := t.look(n)
	selected := n == t.item || n.file == *t.selected()
	lead, depth := rowPadX+rowArrow+rowGap, float32(0)
	for up, ok := t.shape.up[n]; ok; up, ok = t.shape.up[up] {
		depth++
	}
	if depth > 0 { // the indent, and the gap after it
		lead += depth*rowIndent + rowGap
	}
	row := ui.Row(c).Height(treeRowH+2*rowPadY).Margin(-rowPadY, -rowPadX, -rowPadY, -lead).Padding(0, rowPadX, 0, lead).
		Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Radius(rowRadius).Background(bg).Tooltip(w.status(f.Status) + " · " + f.Path)
	if n.file == *t.selected() { // the bar the workspace's lists mark their chosen row with
		row.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: theme.WSelection, H: r.H}, theme.TextStrong, 0)
		})
	}
	row.Children(func() {
		ui.Text(c, t.label(n)).FontSize(theme.FSBody).TextColor(color).SingleLine().Shrink(1).MinWidth(0)
		ui.Spacer(c)
		if stat {
			var spans []ui.Span
			if binary {
				spans = []ui.Span{{Text: w.Bin, Color: theme.RowText(selected, theme.Text3)}}
			} else {
				if add != 0 {
					spans = append(spans, ui.Span{Text: fmt.Sprintf("+%d", add), Color: theme.Add})
				}
				if del != 0 {
					text := fmt.Sprintf("−%d", del)
					if add != 0 {
						text = " " + text
					}
					spans = append(spans, ui.Span{Text: text, Color: theme.Del})
				}
			}
			ui.RichText(c, spans...).FontSize(theme.FSLabel).FontFeatures("tnum").SingleLine().Shrink(0)
		}
		if t.Note != nil {
			if s := t.Note(f); s != "" {
				ui.Text(c, s).FontSize(theme.FSLabel).TextColor(theme.RowText(selected, theme.Text5)).SingleLine().Shrink(0)
			}
		}
		if t.Row != nil {
			t.Row(c, row, n.file)
		}
	})
}

// label is what a row says: a heading's name or path, a file's name (its path when flat) with ×N.
func (t *Tree) label(n node) string {
	switch n.kind {
	case groupNode:
		return t.shape.names[n.group]
	case dirNode:
		return n.dir
	}
	f := t.files[n.file]
	name := f.Path
	if !t.Flat {
		_, name = split(f.Path)
	}
	if n := commitsOf(f); n > 1 {
		name += fmt.Sprintf(" ×%d", n)
	}
	return name
}

// commitsOf is how many of the commits shown changed f (FileChange, AggFile), 0 when its record does not tell.
func commitsOf(f File) int {
	switch s := f.Source.(type) {
	case session.FileChange:
		return s.Commits
	case session.AggFile:
		return s.Commits
	}
	return 0
}

// tailText drops the start of a path that does not fit ("…/pharmacy/dto"), since the end tells it apart.
func tailText(c *ui.Context, s string, size float32, color ui.Color) ui.Element {
	return ui.Box(c).Grow(1).Shrink(1).MinWidth(0).Label(s).Draw(func(p *ui.Painter, r ui.Rect) {
		span := ui.Span{Text: s, Size: size, Color: color}
		w, h := p.MeasureText(0, span)
		if w > r.W {
			span.Text = fitTail(s, r.W, func(t string) float32 {
				w, _ := p.MeasureText(0, ui.Span{Text: t, Size: size})
				return w
			})
		}
		p.RichText(r.X, r.Y+(r.H-h)/2, 0, span)
	})
}

// fitTail is "…" and the longest end of s that fits in width as measure measures it.
func fitTail(s string, width float32, measure func(string) float32) string {
	rs := []rune(s)
	lo, hi := 1, len(rs) // the start of the shortest end known to fit is hi (none but the ellipsis)
	for lo < hi {
		mid := (lo + hi) / 2
		if measure("…"+string(rs[mid:])) <= width {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return "…" + string(rs[hi:])
}

// statOf is the lines a file adds and removes, when its record tells them.
func statOf(f File) (add, del int, binary, ok bool) {
	switch s := f.Source.(type) {
	case session.FileChange:
		return s.Add, s.Del, s.Binary, true
	case session.FileStatus:
		return s.Add, s.Del, s.Binary, true
	}
	return 0, 0, false, false
}

// status is the name of a file status, as the tooltip says it.
func (w *words) status(s string) string {
	switch {
	case strings.HasPrefix(s, "A"):
		return w.Added
	case strings.HasPrefix(s, "D"):
		return w.Deleted
	case strings.HasPrefix(s, "R"):
		return w.Renamed
	}
	return w.Modified
}

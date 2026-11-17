package track

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

// The changes tab: every file of every row, its merged diff as high as its lines, in one stream under the row's heading;
// beside it the stream's outline.

const (
	railWidth    = 264
	outlineWidth = 236
	outlineMin   = 160 // the outline's width as dragged stays within these
	outlineMax   = 480
	// outlineFrom is the narrowest window that keeps the outline beside the stream.
	outlineFrom = 1280
	// statsParallel bounds the line count reads of the rows.
	statsParallel = 4
)

const worktreeGroupSuffix = "\x00worktree"

// fileKey distinguishes a row's committed file from the same path in its working directory.
func fileKey(f session.AggFile) string {
	group := f.Key
	if f.Worktree {
		group += worktreeGroupSuffix
	}
	return group + "\x00" + f.Path
}

// treeFilesOf is AggFiles as a tree shows them: under their heading (a working directory), the files of a directory
// together, each in order of first appearance.
func treeFilesOf(files []session.AggFile, group diffview.Grouper) []diffview.File {
	out := make([]diffview.File, len(files))
	for i, f := range files {
		out[i] = diffview.File{Path: f.Path, OldPath: f.OldPath, Status: f.Status, Source: f}
	}
	return diffview.TreeOrder(out, group)
}

func (p *Page) initStream() {
	p.stream.Header = func(c *ui.Context, group string) {
		p.repoHead(c, tr.Of(c), group)
	}
	p.stream.FileHead = func(c *ui.Context, i int) { p.fileHead(c, tr.Of(c), i) }
	p.stream.Tail = func(c *ui.Context) { p.streamTail(c, tr.Of(c)) }
}

// syncStream gives the stream the files shown, again only when they, their line counts or what they are read against
// changed. A file read before keeps its diff on screen until it is read again.
func (p *Page) syncStream() {
	for _, r := range p.rows {
		if _, ok := p.stats[r.Key]; !ok {
			// Line counts keep initial loads inside the files that will remain visible.
			return
		}
	}
	key := fmt.Sprint(p.filesVer)
	if key == p.streamKey {
		return
	}
	p.streamKey = key
	// What a diff shows: the input its files were read with, a forced read that list came from, the file's commits
	// (merge classification adds parts only to files whose commits changed) and its line counts.
	in, _ := json.Marshal(p.filesIn)
	rev := fmt.Sprint(string(in), "\x00", p.filesReread)
	files := p.shownFiles()
	list := make([]diffview.StreamFile, len(files))
	for i, f := range files {
		sf := diffview.StreamFile{File: diffview.File{Path: f.Path, OldPath: f.OldPath, Status: f.Status, Source: f}, Key: fileKey(f), Rev: fmt.Sprint(rev, "\x00", f.Commits), Group: f.Key, Lines: -1}
		if f.Worktree {
			sf.Group += worktreeGroupSuffix
		}
		if st, ok := p.statOf(f); ok {
			sf.Lines, sf.Binary = st.Add+st.Del, st.Binary
			// Counts that change mean the content changed (an edited working file keeps its commits).
			sf.Rev += fmt.Sprint("\x00", st.Add, " ", st.Del)
		}
		list[i] = sf
	}
	p.stream.SetFiles(list, p.diffLoader())
}

// diffLoader reads the merged diff of a file; with 仅未提交, a row checked out in a worktree shows that worktree against
// HEAD instead.
func (p *Page) diffLoader() diffview.Loader {
	return diffview.AggLoader(p.s, p.filesIn)
}

// loadStats counts, in the background, the lines each file of each row adds and deletes (one git call a row, a few at a
// time). The places that show them stay empty until they come.
func (p *Page) loadStats(in session.TagAggInput) {
	if p.stats == nil || p.statsFor != p.loaded {
		p.stats, p.statsFor = map[string]map[string]session.FileStat{}, p.loaded
		p.streamKey = ""
	}
	p.statsSeq++
	seq, s, ctx, rows := p.statsSeq, p.s, p.h.Caller(), append([]session.AggRepo(nil), p.rows...)
	go func() {
		sem := make(chan struct{}, statsParallel)
		var wg sync.WaitGroup
		for _, r := range rows {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				// A row whose lines cannot be counted counts none: its files then show the counts of their diffs.
				l, _ := s.TagAggStats(ctx, in, r.Key)
				<-sem
				p.h.Update(func() {
					if seq != p.statsSeq {
						return
					}
					m := make(map[string]session.FileStat, len(l))
					for _, st := range l {
						m[st.Path] = st
					}
					p.stats[r.Key] = m
					p.streamKey = ""
				})
			}()
		}
		wg.Wait()
		p.h.Update(func() {
			if seq == p.statsSeq {
				p.prefetch()
			}
		})
	}()
}

// prefetch reads, once the requirement shown is in, the files and line counts of the other tracked ones in the
// background, so switching to one is answered from the caches instead of waiting for git. Archived ones are left out:
// they are folded away and rarely opened.
func (p *Page) prefetch() {
	if p.stopPrefetch != nil {
		p.stopPrefetch()
		p.stopPrefetch = nil
	}
	shown := p.shownReq()
	var ins []session.TagAggInput
	for _, r := range p.reqs {
		if r.ID == shown || r.Stage == session.StageArchived {
			continue
		}
		c := query.Default()
		c.Q = r.ID
		ins = append(ins, session.TagAggInput{Base: p.master, DiffMode: p.diffMode, DiffStage: p.diffStage, Branches: map[string]string{}, Cond: &c, Requirement: r.ID})
	}
	if len(ins) == 0 {
		return
	}
	ctx, stop := context.WithCancel(p.h.Caller())
	p.stopPrefetch = stop
	s := p.s
	go s.PrefetchTagAgg(ctx, ins)
}

func (p *Page) statOf(f session.AggFile) (session.FileStat, bool) {
	if f.Worktree {
		return session.FileStat{}, false
	}
	st, ok := p.stats[f.Key][f.Path]
	return st, ok
}

// fileStats are the lines file i of the stream adds and deletes: counted for its row, else from its diff once read.
func (p *Page) fileStats(i int) (add, del int, ok bool) {
	f := p.stream.Files()[i].Source.(session.AggFile)
	if st, ok := p.statOf(f); ok && !st.Binary {
		return st.Add, st.Del, true
	}
	return p.stream.Stats(i)
}

// totals are the lines all the files add and delete, once every row is counted.
func (p *Page) totals() (add, del int, ok bool) {
	if p.diffPending() {
		return 0, 0, false
	}
	for _, r := range p.rows {
		m, ok := p.stats[r.Key]
		if !ok {
			return 0, 0, false
		}
		for _, st := range m {
			add += st.Add
			del += st.Del
		}
	}
	return add, del, len(p.rows) > 0
}

func (p *Page) worktreeInfo(w *words, r session.AggRepo) (string, int) {
	n := 0
	for _, f := range p.shownFiles() {
		if f.Key == r.Key && (f.Worktree || p.filesIn.DiffMode == session.DiffWorktree) {
			n++
		}
	}
	dir := session.DirtyDir{Path: r.Worktree}
	for _, repo := range p.h.Info().Repos {
		if repo.Name == r.Repo {
			for _, wt := range repo.Worktrees {
				if wt.Path == r.Worktree {
					dir.Main = wt.Main
				}
			}
		}
	}
	return dirtyLocation(dir, w), n
}

// repoHead is a row's heading in the stream: its repo, its branch (a click picks another), how far it got and how far
// it is from the compare target (a click on ↓ lists what the target has beyond it).
func (p *Page) repoHead(c *ui.Context, w *words, group string) {
	key, worktree := strings.CutSuffix(group, worktreeGroupSuffix)
	r, ok := p.rowOf(key)
	if !ok {
		return
	}
	ui.Row(c).FillWidth().Background(theme.FrameBg).Padding(18, 20, 8, 20).Gap(10).AlignItems(ui.Center).
		Border(1, theme.Line).BorderWidth(0, 0, 1, 0).Label("stream repo " + group).Children(func() {
		name := rowLabel(r.Key, r.Repo)
		ui.Text(c, name).FontSize(18).FixedLineHeight(24).FontWeight(600).TextColor(theme.TextStrong).SingleLine().Shrink(1).MinWidth(48).MaxWidth(240).Tooltip(name)
		if worktree || p.filesIn.DiffMode == session.DiffWorktree {
			location, n := p.worktreeInfo(w, r)
			label := fill(w.WorktreeSection, "location", location, "n", n)
			ui.Text(c, w.DiffWorktree+" · "+fill(w.WcFileCount, "n", n)).FontSize(theme.FSSmall).TextColor(theme.Ticket).SingleLine().Shrink(0).Label(label).Tooltip(label)
			ui.Text(c, location).FontSize(theme.FSSmall).TextColor(theme.Ticket).SingleLine().Shrink(1).MinWidth(0).MaxWidth(160).Tooltip(r.Worktree)
		}
		p.branchButton(c, w, r)
		if mr, ok := p.mrResults()[r.Key]; ok {
			p.mrResult(c, w, r, mr)
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
			p.stageSteps(c, r.Stage)
			ui.Text(c, p.stageName(w, r.Stage)).SingleLine()
		})
		switch ab := abCell(r); ab.Kind {
		case abNone:
			ui.Text(c, fill(w.SkipNoBase, "base", valueOr(p.master, r.Base))).TextColor(theme.Text4).SingleLine().Shrink(0)
		case abUnset:
			ui.Text(c, w.SkipBaseUnset).TextColor(theme.Text4).SingleLine().Shrink(0)
		case abCount:
			ui.Row(c).Gap(8).Shrink(0).Children(func() {
				ui.Text(c, "↑"+itoa(ab.Up)).FontFeatures("tnum").TextColor(theme.Text4)
				down := "↓" + itoa(ab.Down)
				if ab.Down == 0 {
					ui.Text(c, down).FontFeatures("tnum").TextColor(theme.Text4)
					return
				}
				b := ui.ButtonBase(c.Key("down" + r.Key)).Label(down).Cursor(ui.CursorPointer).Tooltip(fill(w.DownHint, "b", valueOr(p.master, r.Base), "n", ab.Down))
				b.Children(func() {
					t := ui.Text(c, down).FontFeatures("tnum").TextColor(theme.Text4)
					if ab.Down > farBehind {
						t.TextColor(theme.Del)
					}
					if b.Hovered() {
						t.Underline()
					}
				})
				if b.Clicked() {
					p.showDown(r.Key)
				}
			})
		}
	})
}

func (p *Page) scrollToRepo(key string) {
	for index, file := range p.stream.Files() {
		if file.Source.(session.AggFile).Key == key {
			p.stream.ScrollToFile(index)
			return
		}
	}
}

// mrResult is a row's 创建 MR result. A failure says why in a line, and its whole stderr is on hover.
func (p *Page) mrResult(c *ui.Context, w *words, r session.AggRepo, mr session.MRRow) {
	cell := mrCell(w, mr)
	color := theme.Text3
	switch cell.Tone {
	case toneOK:
		color = theme.Add
	case toneBad:
		color = theme.DangerTxt
	}
	ui.Column(c).Shrink(1).MinWidth(0).FontSize(theme.FSSmall).FontFeatures("tnum").Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0).Children(func() {
			if cell.Tone == toneOK {
				ui.Icon(c, theme.IconCheck).FontSize(theme.IconS).TextColor(color).Shrink(0)
			}
			if cell.URL != "" {
				b := ui.ButtonBase(c.Key("res" + r.Key)).Label(cell.Text).Cursor(ui.CursorPointer).Tooltip(cell.Title)
				b.Children(func() { ui.Text(c, cell.Text).TextColor(color).SingleLine().Underline() })
				if b.Clicked() {
					c.OpenURL(cell.URL)
				}
				return
			}
			t := ui.Text(c, cell.Text).TextColor(color).SingleLine()
			if cell.Title != "" {
				t.Tooltip(cell.Title)
			}
		})
		if cell.Reason != "" {
			ui.Text(c, cell.Reason).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).Tooltip(cell.Title)
		}
	})
}

// showDown opens the commits tab on what the compare target has beyond row key.
func (p *Page) showDown(key string) {
	p.setFocus(key)
	p.tab, p.side = 1, 1
}

// fileHead is a file's head in the stream: its name, its directory, what it holds, and its lines. Its menu and a double
// click open it elsewhere.
func (p *Page) fileHead(c *ui.Context, w *words, i int) {
	streamFileHead(c, w, p.stream, i, true, p.fileStats, p.review, nil)
}

// more, when set, adds to the file's menu and may anchor what its items open to the header.
func streamFileHead(c *ui.Context, w *words, stream *diffview.Stream, i int, showUncommitted bool, stats func(int) (int, int, bool), open func(session.AggFile), more fileActions) {
	f := stream.Files()[i].Source.(session.AggFile)
	dir, name := path.Split(f.Path)
	var row ui.Element
	ui.Row(c).FillWidth().AlignItems(ui.Stretch).Children(func() {
		foldChevron(c, w, stream, i, fileKey(f))
		row = ui.ButtonBase(c.Key("head "+fileKey(f))).Justify(ui.Start).Grow(1).MinWidth(0).Padding(14, 20, 4, 6).Gap(10).Label(f.Repo + "/" + f.Path)
	})
	if row.DoubleClicked() {
		open(f)
	}
	widgets.ContextMenu(c, row, func(m *ui.Menu) {
		if m.Item(w.OpenDiff).Chosen() {
			open(f)
		}
		if m.Item(w.CopyPath).Chosen() {
			c.WriteClipboard(f.Path)
		}
		m.Separator()
		if m.Item(w.FoldAll).Chosen() {
			stream.SetAllFolded(true, i)
		}
		if m.Item(w.UnfoldAll).Chosen() {
			stream.SetAllFolded(false, i)
		}
		if more != nil {
			more.menu(m, w, f, "stream")
		}
	})
	if more != nil {
		more.anchor(c, w, row, f, "stream")
	}
	row.Children(func() {
		ui.Text(c, name).FontWeight(500).TextColor(theme.TextStrong).SingleLine().Shrink(0)
		ui.Text(c, dir).TextColor(theme.Text5).SingleLine().Shrink(1).MinWidth(0)
		if showUncommitted && f.Uncommitted && !f.Worktree {
			fileTag(c, w.WcTag, theme.Ticket)
		}
		if stream.Resolved(i) {
			fileTag(c, w.Resolved, theme.Resolved)
		}
		if f.Status == "A" || f.Untracked {
			ui.Text(c, w.NewFile).TextColor(theme.Text4).SingleLine().Shrink(0)
		}
		ui.Spacer(c)
		if add, del, ok := stats(i); ok {
			ui.Text(c, "+"+itoa(add)).FontFeatures("tnum").TextColor(theme.Add).Shrink(0)
			if del > 0 {
				ui.Text(c, "−"+itoa(del)).FontFeatures("tnum").TextColor(theme.Del).Shrink(0)
			}
		}
	})
}

// foldChevron folds or opens file i of the stream, with Option every file as it does file i; it points down while the
// diff shows.
func foldChevron(c *ui.Context, w *words, stream *diffview.Stream, i int, key string) {
	folded := stream.Folded(i)
	icon, label := theme.IconChevDown, w.FoldFile
	if folded {
		icon, label = theme.IconChevRight, w.UnfoldFile
	}
	b := ui.ButtonBase(c.Key("fold "+key)).Padding(14, 0, 4, 16).Center().Shrink(0).Cursor(ui.CursorPointer).Label(label).Tooltip(label + w.FoldAllHint)
	if b.Clicked() {
		if c.Modifiers()&ui.Alt != 0 {
			stream.SetAllFolded(!folded, i)
		} else {
			stream.SetFolded(i, !folded)
		}
		c.Invalidate()
	}
	color := theme.Text4
	if b.Hovered() {
		color = theme.Text
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(theme.IconS).TextColor(color) })
}

// fileActions are the actions a page adds to a file's menu beyond opening and copying it: the items, and what they open
// below the file (where tells the file list from the stream, which show the same file).
type fileActions interface {
	menu(m *ui.Menu, w *words, f session.AggFile, where string)
	anchor(c *ui.Context, w *words, row ui.Element, f session.AggFile, where string)
}

func fileTag(c *ui.Context, text string, color ui.Color) {
	ui.Text(c, text).FontSize(theme.FSSmall).FixedLineHeight(18).TextColor(color).Border(1, color).PaddingX(6).SingleLine().Shrink(0)
}

// streamTail closes the stream: how many files, how many with uncommitted changes, how many edited while merging (of
// those read so far: only a file's diff tells).
func (p *Page) streamTail(c *ui.Context, w *words) {
	files := p.stream.Files()
	wc, res := 0, 0
	for i, f := range files {
		if f.Source.(session.AggFile).Uncommitted {
			wc++
		}
		if p.stream.Resolved(i) {
			res++
		}
	}
	text := fill(w.StreamEnd, "n", len(files))
	if wc > 0 {
		text += fill(w.StreamEndWc, "n", wc)
	}
	if res > 0 {
		text += fill(w.StreamEndResolved, "n", res)
	}
	ui.Text(c, text+w.StreamEndStop).TextColor(theme.Text4).Wrap().Padding(28, 20, 60, 20)
}

// outline is the stream's table of contents: the files under their repo, each with a bar of its added and deleted lines.
// The file at the top of the stream is lit; a click scrolls the stream to a file.
func (p *Page) outlineList(c *ui.Context) {
	streamOutline(c, p.stream, &p.outline, func(key string) string {
		if strings.HasSuffix(key, worktreeGroupSuffix) {
			return tr.Of(c).DiffWorktree
		}
		if r, ok := p.rowOf(key); ok {
			return rowLabel(r.Key, r.Repo)
		}
		return key
	}, p.fileStats, p.outlineW())
}

// outlineW is the outline's width: as the user dragged it, else outlineWidth.
func (p *Page) outlineW() float32 {
	if w := p.h.Dock().Splits[splitOutline]; w > 0 {
		return w
	}
	return outlineWidth
}

// The page's own dividers in shell.Dock.Splits: the rail's (the requirements pane's height) and the outline's width.
const (
	splitRail = iota
	splitOutline
)

func streamOutline(c *ui.Context, stream *diffview.Stream, state *ui.ListState, label func(string) string, stats func(int) (int, int, bool), width float32) {
	files := stream.Files()
	type entry struct {
		repo string
		file int
	}
	var rows []entry
	last := "\x00"
	for i, f := range files {
		if f.Group != last {
			if key, ok := strings.CutSuffix(f.Group, worktreeGroupSuffix); ok && last != key {
				rows = append(rows, entry{repo: key, file: -1})
			}
			last = f.Group
			rows = append(rows, entry{repo: f.Group, file: -1})
		}
		rows = append(rows, entry{repo: f.Group, file: i})
	}
	cur := stream.Current()
	state.Key = func(i int) any {
		if i >= len(rows) {
			return i
		}
		if rows[i].file < 0 {
			return "repo\x00" + rows[i].repo
		}
		return files[rows[i].file].Key
	}
	ui.List(c.Key("outline"), state, len(rows), func(i int) {
		e := rows[i]
		if e.file < 0 {
			color := theme.Text4
			if strings.HasSuffix(e.repo, worktreeGroupSuffix) {
				color = theme.Ticket
			}
			ui.Text(c, label(e.repo)).FontSize(theme.FSGroup).FontWeight(600).TextColor(color).SingleLine().Padding(10, 16, 2, 16)
			return
		}
		f := files[e.file]
		b := ui.ButtonBase(c.Key("o "+f.Key)).Justify(ui.Start).FillWidth().Padding(1, 16, 1, 16).Gap(8).Label("outline " + f.Group + "/" + f.Path)
		if b.Hovered() {
			b.Background(theme.Hover)
		}
		if b.Clicked() {
			stream.ScrollToFile(e.file)
		}
		b.Children(func() {
			color := theme.Text4
			if e.file == cur {
				color = theme.TextStrong
			}
			ui.Text(c, path.Base(f.Path)).FontSize(theme.FSSmall).TextColor(color).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			ui.Row(c).Width(46).Shrink(0).Children(func() {
				if add, del, ok := stats(e.file); ok && add+del > 0 {
					ratioBar(c, add, del)
				}
			})
		})
	}).Width(width).Shrink(0).FillHeight().Background(theme.Rail).Padding(12, 0, 12, 0)
}

// ratioBar is as long as the change is big (square root, at most 46), split into its added and deleted share.
func ratioBar(c *ui.Context, add, del int) {
	t := float64(add + del)
	width := float32(min(46, 8+math.Round(math.Sqrt(t)*3)))
	ui.Row(c).Width(width).Height(6).Background(theme.BarTrack).Children(func() {
		if add > 0 {
			ui.Box(c).Width(width * float32(add) / float32(t)).FillHeight().Background(theme.BarAdd)
		}
		if del > 0 {
			ui.Box(c).Width(width * float32(del) / float32(t)).FillHeight().Background(theme.BarDel)
		}
	})
}

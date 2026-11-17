package track

import (
	"context"
	"path"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

// justNow is how many seconds a change counts as "刚刚".
const justNow = 120

const (
	dirRow      = 34 // a directory's row in the list above the changes
	dirPad      = 4
	agoWidth    = 88 // fits "01-02 15:04"
	statusWidth = 72
	linesWidth  = 88
)

// dirtyPage follows all directories with uncommitted changes and shows their tracked requirements.
type dirtyPage struct {
	view         *session.DirtyView
	err          string
	seq          int
	busy         bool
	focus        string
	tab          int
	stream       *diffview.Stream
	streamView   *session.DirtyView
	streamFor    string
	fileList     ui.ListState
	fileSel      int
	area         ui.Handle
	topHeight    float32
	resized      bool
	statsView    *session.DirtyView
	stats        map[string]*dirtyFileStat
	statsCancel  context.CancelFunc
	statsContext context.Context

	page      *Page
	discard   string // fileKey of the file whose discard is being confirmed; "" none
	discardAt string // where it was asked: "list" or "stream"
}

func (d *dirtyPage) init(p *Page) {
	d.page = p
	d.fileSel = -1
	d.fileList.Selected = &d.fileSel
	d.stream = diffview.NewStream()
	d.stream.Header = func(c *ui.Context, key string) { d.dirHead(c, tr.Of(c), key) }
	d.stream.FileHead = func(c *ui.Context, i int) {
		streamFileHead(c, tr.Of(c), d.stream, i, false, d.stream.Stats, func(f session.AggFile) { d.open(p, f) }, d)
	}
	d.stream.Tail = func(c *ui.Context) {
		w := tr.Of(c)
		ui.Text(c, fill(w.StreamEnd, "n", len(d.stream.Files()))+w.StreamEndStop).TextColor(theme.Text4).Wrap().Padding(28, 20, 60, 20)
	}
}

func (d *dirtyPage) load(p *Page) {
	d.seq++
	d.busy = true
	seq, s, ctx := d.seq, p.s, p.h.Caller()
	go func() {
		v, err := s.Dirty(ctx)
		p.h.Update(func() {
			if seq != d.seq {
				return
			}
			d.busy = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.view, d.err = &v, ""
			if _, ok := dirOf(d.dirs(), d.focus); !ok {
				d.focus = ""
			}
			p.h.Painted()
		})
	}()
}

func (d *dirtyPage) dirs() []session.DirtyDir {
	var out []session.DirtyDir
	if d.view != nil {
		for _, g := range d.view.Groups {
			out = append(out, g.Dirs...)
		}
	}
	return out
}

func (d *dirtyPage) body(c *ui.Context, p *Page) {
	w := tr.Of(c)
	if d.view == nil {
		s := w.Loading
		if d.err != "" {
			s = fill(w.Error, "reason", d.err)
		}
		ui.Column(c).Grow(1).Center().Gap(10).Padding(16).Children(func() {
			ui.Text(c, s).TextColor(theme.Text4).TextAlign(ui.Center)
			if d.err != "" {
				p.refreshStatus(c, w)
			}
		})
		return
	}
	dirs := d.dirs()
	d.syncStream(p, dirs)
	if len(dirs) == 0 {
		emptyNote(c, w.NoDirty)
		return
	}
	if d.focus != "" {
		widgets.Shortcuts(c, widgets.Shortcut{Key: ui.KeyEscape, Do: func() { d.focus = "" }})
	}
	ui.Column(c).Bind(&d.area).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		rowHeight := float32(dirRow)
		headerHeight := float32(38 + 2*dirPad)
		minimum := headerHeight + 2*rowHeight
		if !d.resized {
			height := d.area.Bounds(c).H
			d.topHeight = max(minimum, min(headerHeight+float32(len(dirs))*rowHeight, height*0.4))
		}
		d.topHeight = max(minimum, d.topHeight)
		if ui.SplitVertical(c, &d.topHeight, func() {
			ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
				d.heading(c, w, dirs)
				d.table(c, p, w, dirs)
			})
		}, func() {
			ui.Column(c).Fill().MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				d.syncStream(p, dirs)
				d.tabs(c, w, dirs)
				if d.tab == 1 {
					d.files(c, p, w, dirs)
				} else {
					d.stream.View(c).Grow(1).MinHeight(0).FillWidth()
				}
			})
		}).Grow(1).MinHeight(0).Changed() {
			d.resized = true
		}
	})
	if d.tab == 0 {
		widgets.Shortcuts(c,
			widgets.Shortcut{Mods: ui.Alt, Key: ui.KeyDown, Do: d.stream.Next},
			widgets.Shortcut{Mods: ui.Alt, Key: ui.KeyUp, Do: d.stream.Prev},
		)
	}
}

func (d *dirtyPage) heading(c *ui.Context, w *words, dirs []session.DirtyDir) {
	ui.Row(c).Height(38).Shrink(0).PaddingX(20).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, w.WcDirs).FontWeight(600).SingleLine().Shrink(0) // the title bar already says uncommitted
		ui.Text(c, itoa(len(dirs))).FontSize(theme.FSSmall).FontFeatures("tnum").TextColor(theme.Text4)
		ui.Spacer(c)
		ui.Row(c).Gap(8).Shrink(1).MinWidth(0).AlignItems(ui.Center).FontSize(theme.FSSmall).Children(func() {
			meta := fill(w.WcMeta, "repos", len(d.view.Groups), "dirs", len(dirs), "files", len(d.view.Files))
			ui.Text(c, meta).TextColor(theme.Text4).FontFeatures("tnum").SingleLine().Shrink(1).MinWidth(0).Tooltip(meta)
			ui.Text(c, "·").TextColor(theme.Text4)
			ui.Text(c, w.WcBase).TextColor(theme.Text4).SingleLine().Tooltip(w.WcCond + " " + w.WcCondValue)
		})
	})
}

func (d *dirtyPage) tabs(c *ui.Context, w *words, dirs []session.DirtyDir) {
	ui.Row(c).Height(38).Shrink(0).PaddingX(20).Gap(22).AlignItems(ui.Center).Border(1, theme.Line).BorderWidth(0, 0, 1, 0).Children(func() {
		for _, tab := range []struct {
			index int
			label string
		}{{1, w.TabFiles}, {0, w.TabChanges}} {
			if pageTab(c, "wc-tab"+itoa(tab.index), tab.label+" "+itoa(len(d.stream.Files())), d.tab == tab.index).Clicked() {
				d.tab = tab.index
				d.page.refreshIfHidden()
				d.fileSel = -1
			}
		}
		d.page.refreshStatus(c, w)
		ui.Spacer(c)
		if dir, ok := dirOf(dirs, d.focus); ok {
			ui.Row(c).Gap(8).Shrink(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fill(w.OnlyDir, "where", dirtyLocation(dir, w))).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).Tooltip(dir.Path)
				if widgets.IconButton(c.Key("wcunfocus"), theme.IconClose, w.Unfocus).Clicked() {
					d.focus = ""
				}
			})
		}
	})
}

func (d *dirtyPage) syncStream(p *Page, dirs []session.DirtyDir) {
	if d.streamView == d.view && d.streamFor == d.focus {
		return
	}
	if d.statsView != d.view {
		if d.statsCancel != nil {
			d.statsCancel()
		}
		d.statsView = d.view
		d.stats = make(map[string]*dirtyFileStat)
		d.statsContext, d.statsCancel = context.WithCancel(p.h.Caller())
	}
	d.streamView, d.streamFor = d.view, d.focus
	grouped := map[string][]session.AggFile{}
	for _, file := range d.view.Files {
		grouped[file.Key] = append(grouped[file.Key], file)
	}
	var files []diffview.StreamFile
	for _, dir := range dirs {
		if d.focus != "" && dir.Path != d.focus {
			continue
		}
		for _, file := range grouped[dir.Path] {
			files = append(files, diffview.StreamFile{
				Path: file.Path, OldPath: file.OldPath, Status: file.Status, Source: file,
				Key: fileKey(file), Rev: itoa(d.seq), Group: dir.Path, Lines: -1,
			})
		}
	}
	d.stream.SetFiles(files, func(ctx context.Context, file diffview.File) (diffview.Texts, error) {
		f := file.Source.(session.AggFile)
		patch, err := p.s.UncommittedPatch(ctx, f.Key, f.Path, f.OldPath, dirtyArea(f), false)
		if err != nil {
			return diffview.Texts{}, err
		}
		return diffview.Texts{Binary: patch.Binary, Parts: []diffview.Part{{Patch: patch}}}, nil
	})
}

func (d *dirtyPage) dirHead(c *ui.Context, w *words, key string) {
	dir, ok := dirOf(d.dirs(), key)
	if !ok {
		return
	}
	ui.Row(c).FillWidth().Background(theme.FrameBg).Padding(18, 20, 8, 20).Gap(14).AlignItems(ui.Center).
		Border(1, theme.Line).BorderWidth(0, 0, 1, 0).Label("stream directory " + key).Children(func() {
		ui.Text(c, dir.Repo).FontSize(18).FixedLineHeight(24).FontWeight(600).TextColor(theme.TextStrong).SingleLine().Shrink(0)
		ui.Text(c, valueOr(dir.Branch, w.NoBranch)).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).Tooltip(dir.Branch)
		if dir.Requirement != "" {
			ui.Text(c, dir.Requirement).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text3).SingleLine().Shrink(0)
		}
		if dir.OnMain {
			fileTag(c, w.OnMain, theme.Ticket)
		}
		if dir.Merging {
			fileTag(c, fill(w.Merging, "n", dir.Conflicts), theme.DangerTxt)
		}
		ui.Spacer(c)
		ui.Text(c, dirtyLocation(dir, w)).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).Tooltip(dir.Path)
		ui.Text(c, fill(w.WcFileCount, "n", dir.Dirty)).FontSize(theme.FSSmall).FontFeatures("tnum").TextColor(theme.Text4).SingleLine().Shrink(0)
		ago(c, w, dir.LastChange, time.Now().Unix(), false)
	})
}

// table lists the directories in the stream headings' grammar: repo, branch and requirement on the left, where and how
// much on the right. A click scopes the panes below to the directory; a second click on it shows everything again.
func (d *dirtyPage) table(c *ui.Context, p *Page, w *words, dirs []session.DirtyDir) {
	now := time.Now().Unix()
	ui.Scroll(c.Key("dirs")).Grow(1).MinHeight(0).PaddingY(dirPad).Children(func() {
		for _, dir := range dirs {
			selected := dir.Path == d.focus
			where := dirtyLocation(dir, w)
			tip := dir.Path
			if title := requirementTitle(p, dir.Requirement); title != "" {
				tip = dir.Requirement + " " + title + "\n" + tip
			}
			row := widgets.RowLook(ui.ButtonBase(c.Key("dir "+dir.Path)).Justify(ui.Start).FillWidth().Height(dirRow).PaddingX(20).Gap(12).
				Label(dir.Repo+" · "+where).Tooltip(tip), selected)
			row.Children(func() {
				ui.Text(c, dir.Repo).FontWeight(600).TextColor(theme.TextStrong).SingleLine().Shrink(0).Tooltip(dir.Repo)
				ui.Text(c, valueOr(dir.Branch, w.NoBranch)).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).Tooltip(dir.Branch)
				if dir.Requirement != "" {
					ui.Text(c, dir.Requirement).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.RowText(selected, theme.Text2)).SingleLine().Shrink(0)
				}
				if dir.OnMain {
					ui.Text(c, w.OnMain).FontSize(theme.FSLabel).TextColor(theme.Ticket).SingleLine().Shrink(0).Tooltip(w.OnMainHint)
				}
				if dir.Merging {
					ui.Text(c, fill(w.Merging, "n", dir.Conflicts)).FontSize(theme.FSLabel).FontFeatures("tnum").TextColor(theme.DangerTxt).SingleLine().Shrink(0).Tooltip(w.MergingHint)
				}
				ui.Spacer(c)
				ui.Text(c, where).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.RowText(selected, theme.Text4)).SingleLine().Shrink(1).MinWidth(0).Tooltip(dir.Path)
				ui.Text(c, fill(w.WcFileCount, "n", dir.Dirty)).FontSize(theme.FSSmall).FontFeatures("tnum").TextColor(theme.RowText(selected, theme.Text3)).SingleLine().Shrink(0)
				ui.Row(c).Width(agoWidth).Shrink(0).Justify(ui.End).FontSize(theme.FSSmall).Children(func() { ago(c, w, dir.LastChange, now, selected) })
			})
			if row.Clicked() {
				if selected {
					d.focus = ""
				} else {
					d.focus = dir.Path
				}
			}
		}
	})
}

// requirementTitle is the title of tracked requirement id, "" when it has none or is not tracked.
func requirementTitle(p *Page, id string) string {
	if id == "" || p == nil {
		return ""
	}
	for _, req := range p.reqs {
		if req.ID == id {
			return req.Title
		}
	}
	return ""
}

func (d *dirtyPage) files(c *ui.Context, p *Page, w *words, dirs []session.DirtyDir) {
	files := d.stream.Files()
	d.fileList.Key = func(i int) any { return files[i].Key }
	d.fileList.Label = func(i int) string { return "file " + files[i].Group + "/" + files[i].Path }
	ui.Themed(c, tableTheme(d.fileList.Handle.Focused(c))).Grow(1).MinHeight(0).Children(func() {
		l := ui.List(c.Key("files"), &d.fileList, len(files), func(i int) {
			f := files[i].Source.(session.AggFile)
			dir, name := path.Split(f.Path)
			row := ui.Row(c).Height(dirRow).PaddingX(20).Gap(12).AlignItems(ui.Center)
			widgets.ContextMenu(c, row, func(m *ui.Menu) {
				if m.Item(w.OpenDiff).Chosen() {
					d.open(p, f)
				}
				if m.Item(w.CopyPath).Chosen() {
					c.WriteClipboard(f.Path)
				}
				d.menu(m, w, f, "list")
			})
			d.anchor(c, w, row, f, "list")
			row.Children(func() {
				ui.Text(c, name).FontWeight(500).TextColor(theme.TextStrong).SingleLine().Shrink(0).Tooltip(f.Path)
				ui.Text(c, dir).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).Tooltip(f.Path)
				ui.Spacer(c)
				wd, _ := dirOf(dirs, f.Key)
				ui.Text(c, f.Repo+" · "+dirtyLocation(wd, w)).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).Tooltip(f.Key)
				label, color := w.ModifiedFile, theme.Text3
				switch {
				case f.Status == "U":
					label, color = w.ConflictFile, theme.DangerTxt
				case f.Status == "A" || f.Untracked:
					label, color = w.AddedFile, theme.Add
				case f.Status == "D":
					label, color = w.DeletedFile, theme.Del
				}
				ui.Text(c, label).FontSize(theme.FSSmall).TextColor(color).SingleLine().Width(statusWidth).Shrink(0)
				stat := d.fileStat(p, i, f)
				ui.Row(c).Width(linesWidth).Shrink(0).Gap(8).Justify(ui.End).AlignItems(ui.Center).FontSize(theme.FSSmall).Children(func() {
					if !stat.ready || stat.binary {
						ui.Text(c, "—").TextColor(theme.Text4).Tooltip(stat.err)
						return
					}
					if stat.add > 0 || stat.del == 0 {
						ui.Text(c, "+"+itoa(stat.add)).FontFeatures("tnum").TextColor(theme.Add).SingleLine()
					}
					if stat.del > 0 {
						ui.Text(c, "−"+itoa(stat.del)).FontFeatures("tnum").TextColor(theme.Del).SingleLine()
					}
				})
			})
		}).Grow(1).MinHeight(0).PaddingY(dirPad)
		if (l.Changed() || l.Submitted()) && d.fileSel >= 0 && d.fileSel < len(files) {
			d.tab = 0
			d.stream.ScrollToFile(d.fileSel)
		}
	})
}

type dirtyFileStat struct {
	add, del      int
	ready, binary bool
	err           string
}

func (d *dirtyPage) fileStat(p *Page, i int, f session.AggFile) dirtyFileStat {
	if add, del, ok := d.stream.Stats(i); ok {
		return dirtyFileStat{add: add, del: del, ready: true}
	}
	key := fileKey(f)
	if stat, ok := d.stats[key]; ok {
		return *stat
	}
	d.stats[key] = &dirtyFileStat{}
	ctx, v := d.statsContext, d.statsView
	go func() {
		patch, err := p.s.UncommittedPatch(ctx, f.Key, f.Path, f.OldPath, dirtyArea(f), false)
		stat := dirtyFileStat{ready: err == nil, binary: patch.Binary}
		if err != nil {
			stat.err = err.Error()
		}
		for _, hunk := range patch.Hunks {
			for _, line := range hunk.Lines {
				switch line.Kind {
				case "+":
					stat.add++
				case "-":
					stat.del++
				}
			}
		}
		p.h.Update(func() {
			if d.statsView == v {
				d.stats[key] = &stat
			}
		})
	}()
	return dirtyFileStat{}
}

func dirtyLocation(dir session.DirtyDir, w *words) string {
	if dir.Main {
		return w.MainDir
	}
	return path.Base(dir.Path)
}

func ago(c *ui.Context, w *words, at, now int64, selected bool) {
	switch {
	case at == 0:
		ui.Text(c, "—").TextColor(theme.RowText(selected, theme.Text4))
	case now-at < justNow:
		ui.Text(c, w.JustNow).TextColor(theme.RowText(selected, theme.Text3)).SingleLine()
	default:
		ui.Text(c, time.Unix(at, 0).Format("01-02 15:04")).FontSize(theme.FSLabel).TextColor(theme.RowText(selected, theme.Text3)).SingleLine().Tooltip(time.Unix(at, 0).Format("2006-01-02 15:04:05"))
	}
}

func (d *dirtyPage) open(p *Page, f session.AggFile) {
	src := session.DiffSource{Kind: "wc", Repo: f.Repo, Worktree: f.Key, Path: f.Path, OldPath: f.OldPath, Area: dirtyArea(f)}
	if err := p.h.Windows().OpenDiff(src); err != nil {
		p.h.Toasts().Error(err.Error())
	}
}

// newFile: discarding the file's changes deletes it.
func newFile(f session.AggFile) bool { return f.Untracked || f.Status == "A" }

// menu adds 丢弃改动 (删除文件 for a new file) to a file's menu; choosing it asks to confirm below the file, where.
func (d *dirtyPage) menu(m *ui.Menu, w *words, f session.AggFile, where string) {
	label := w.DiscardFile
	if newFile(f) {
		label = w.DeleteFile
	}
	if m.Item(label).Chosen() {
		d.discard, d.discardAt = fileKey(f), where
	}
}

// anchor asks, below row, to confirm the discard of f chosen from its menu at where. Nothing can bring the changes back,
// so there is no undo: the user confirms first.
func (d *dirtyPage) anchor(c *ui.Context, w *words, row ui.Element, f session.AggFile, where string) {
	if d.discard != fileKey(f) || d.discardAt != where {
		return
	}
	name := path.Base(f.Path)
	q := widgets.Confirm{Title: fill(w.DiscardTitle, "name", name), Detail: w.DiscardDetail, OK: w.DiscardOK, Cancel: i18n.Of(c).Cancel, Danger: true}
	if newFile(f) {
		q.Title, q.Detail, q.OK = fill(w.DeleteTitle, "name", name), w.DeleteDetail, w.DeleteOK
	}
	q.OnOK = func() { d.run(f, name) }
	open := true
	widgets.ConfirmPopover(c, row, &open, q)
	if !open {
		d.discard = ""
	}
}

// run discards f's uncommitted changes, then reads the directories again.
func (d *dirtyPage) run(f session.AggFile, name string) {
	p := d.page
	s, ctx := p.s, p.h.Caller()
	deleted := newFile(f)
	go func() {
		err := s.Discard(ctx, f.Key, f.Path)
		p.h.Update(func() {
			w := p.words()
			switch {
			case err != nil:
				p.h.Toasts().TaskError(fill(w.DiscardFailed, "reason", err.Error()))
			case deleted:
				p.h.Toasts().Show(fill(w.Deleted, "name", name))
			default:
				p.h.Toasts().Show(fill(w.Discarded, "name", name))
			}
			d.load(p)
		})
	}()
}

func dirtyArea(f session.AggFile) string {
	if f.Untracked {
		return "untracked"
	}
	return "head"
}

func dirOf(dirs []session.DirtyDir, path string) (session.DirtyDir, bool) {
	for _, d := range dirs {
		if d.Path == path && path != "" {
			return d, true
		}
	}
	return session.DirtyDir{}, false
}

func tableTheme(focused bool) *ui.Theme {
	t := theme.TableTheme(focused)
	t.Surface = ui.Transparent
	return t
}

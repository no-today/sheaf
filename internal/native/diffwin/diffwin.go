// Package diffwin is the diff window of the native UI (IDEA's "open in new window"): one file's diff, stepping through
// the files of the same commit / uncommitted changes, fixed to what it was opened with. It is a file
// list beside native/diffview, which draws the diffs.
package diffwin

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/native/i18n"
	"sheaf/internal/native/shell"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

type words struct {
	Files    string
	Loading  string
	NoFiles  string
	Reason   string // %s: why the window cannot show its source
	Commit   string
	Wc       string
	Whole    string
	Stage    string // %s: the merge flow's stage
	Req      string // %s: requirement number, %s: what of it is shown
	Range    string // %s: base, %s: branch
	StepHint string
	Staged   string
	Unstaged string
	Untrack  string
	Expand   string
	Collapse string
}

var tr = i18n.New(words{
	Files:    "文件",
	Loading:  "正在读取…",
	NoFiles:  "没有文件",
	Reason:   "无法显示：%s",
	Commit:   "提交",
	Wc:       "未提交",
	Whole:    "全部改动",
	Stage:    "待合 %s",
	Req:      "需求 %s · %s",
	Range:    "%s…%s",
	StepHint: "⌥↑ ⌥↓ 切换文件",
	Staged:   "已暂存",
	Unstaged: "未暂存",
	Untrack:  "新文件",
	Expand:   "全部展开",
	Collapse: "全部折叠",
}, words{
	Files:    "Files",
	Loading:  "Loading…",
	NoFiles:  "No files",
	Reason:   "Can't show: %s",
	Commit:   "Commit",
	Wc:       "Uncommitted",
	Whole:    "All changes",
	Stage:    "Awaiting %s",
	Req:      "Requirement %s · %s",
	Range:    "%s…%s",
	StepHint: "⌥↑ ⌥↓ step through files",
	Staged:   "staged",
	Unstaged: "unstaged",
	Untrack:  "new file",
	Expand:   "Expand all",
	Collapse: "Collapse all",
})

// Window is the diff window's state; every field is used on the main thread.
type Window struct {
	src    session.DiffSource
	cancel context.CancelFunc

	loaded bool
	err    string
	p      plan
	diff   *diffview.State
	tree   diffview.Tree
	sel    int // the file shown, as the tree's choice
	listW  float32
}

// New makes the diff window of src; update is its mygo.Window.Update.
func New(s *session.Session, src session.DiffSource, update func(func())) *Window {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Window{src: src, cancel: cancel, listW: 280}
	w.diff = diffview.New(nil, nil)
	w.tree.Selected = &w.sel
	w.tree.KeepClosed = true // the window's list is one source's: another layout of it (按仓库分组) keeps what is folded
	w.tree.OnSelect = func(i int) { w.diff.Select(i) }
	w.diff.OnSelect = func(i int) { w.sel = i }
	go func() {
		p, err := planOf(session.Caller(ctx, "diffwin"), s, src)
		update(func() {
			if ctx.Err() != nil {
				return
			}
			w.loaded = true
			if err != nil {
				w.err = err.Error()
				return
			}
			w.p = p
			w.sel = p.current
			w.tree.Group = p.group
			w.diff.SetFiles(p.files, p.load, p.current)
		})
	}()
	return w
}

// Title is the window's title: what the window shows, never one of its files.
func (w *Window) Title() string {
	main, sub := w.heading(tr.For(""))
	if sub == "" {
		return main
	}
	return main + " · " + sub
}

// Close stops the window's work.
func (w *Window) Close() {
	w.cancel()
	w.diff.Close()
}

// View builds the diff window.
func (w *Window) View(c *ui.Context) {
	c.SetTheme(theme.Theme())
	t := tr.Of(c)
	if w.diff.Theme == nil {
		w.diff.Theme = diffview.DefaultTheme()
	}
	w.diff.ToolsOutside = true // the title bar has them
	w.diff.BeginFrame()
	widgets.Shortcuts(c,
		widgets.Shortcut{Mods: ui.Alt, Key: ui.KeyUp, Do: w.diff.PrevFile},
		widgets.Shortcut{Mods: ui.Alt, Key: ui.KeyDown, Do: w.diff.NextFile},
	)
	shell.WindowWith(c, w.titleBar, w.tools(), func(c *ui.Context) {
		switch {
		case w.err != "":
			note(c, fmt.Sprintf(t.Reason, w.err), theme.ErrText)
		case !w.loaded:
			note(c, t.Loading, theme.Text4)
		case len(w.p.files) == 0:
			note(c, t.NoFiles, theme.Text4)
		case len(w.p.files) == 1:
			w.right(c)
		default:
			ui.Split(c, &w.listW, func() { w.fileList(c) }, func() { w.right(c) }).Grow(1).MinHeight(0)
		}
	}, w.status())
}

func note(c *ui.Context, text string, color ui.Color) {
	ui.Column(c).Grow(1).FillWidth().Center().Padding(16).Children(func() {
		ui.Text(c, text).TextColor(color).TextAlign(ui.Center)
	})
}

// fileList is the source's files as a tree (an uncommitted file notes its area).
func (w *Window) fileList(c *ui.Context) {
	t := tr.Of(c)
	w.tree.Note = func(f diffview.File) string {
		if fs, ok := f.Source.(session.FileStatus); ok {
			return areaName(t, fs.Area)
		}
		return ""
	}
	ui.Column(c).Fill().Background(theme.Rail).Children(func() {
		paneHead(c, func() {
			ui.Text(c, t.Files).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0)
			ui.Text(c, fmt.Sprint(len(w.p.files))).FontSize(theme.FSSmall).FontFeatures("tnum").TextColor(theme.Text5).Shrink(0)
			ui.Spacer(c)
			if bare(widgets.IconButton(c, theme.IconExpandAll, t.Expand)).Clicked() {
				w.tree.ExpandAll()
			}
			if bare(widgets.IconButton(c, theme.IconFoldAll, t.Collapse)).Clicked() {
				w.tree.CollapseAll()
			}
		})
		diffview.FileTree(c, &w.tree, w.p.files).Grow(1).MinHeight(0).FillWidth()
	})
}

// paneHead is the header row of a pane, ruled off from what it heads as the workspace's panes are.
func paneHead(c *ui.Context, content func()) ui.Element {
	return ui.Row(c).FillWidth().Height(theme.HPaneHead).Shrink(0).PaddingStart(12).PaddingEnd(6).Gap(6).AlignItems(ui.Center).
		Border(1, theme.Line).BorderWidth(0, 0, 1, 0).Children(content)
}

// bare is an icon button without its outline, for a pane header whose rule already frames it.
func bare(b ui.Element) ui.Element { return b.Border(0, ui.Transparent) }

// right is Preview Diff of the current file.
func (w *Window) right(c *ui.Context) {
	diffview.View(c, w.diff).Grow(1).FillWidth().MinHeight(0)
}

// titleBar names the source: the commit, the uncommitted changes of a directory, or a requirement's view of its changes;
// sub (hash, directory, range) is dimmed.
func (w *Window) titleBar(c *ui.Context) {
	main, sub := w.heading(tr.Of(c))
	ui.Row(c).Gap(8).PaddingX(8).Shrink(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
		if sub != "" && w.src.Kind == "commit" {
			ui.Text(c, sub).Font(theme.MonoFont).TextColor(theme.Text4).SingleLine().Shrink(0)
			sub = ""
		}
		ui.Text(c, main).FontWeight(600).SingleLine().Shrink(1).MinWidth(0).Tooltip(main)
		if sub != "" {
			ui.Text(c, sub).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(2).MinWidth(0).Tooltip(sub)
		}
	})
}

// tools is the title bar's buttons: change and file navigation and the layout switch; nil until there is a diff.
func (w *Window) tools() func(c *ui.Context) {
	if !w.loaded || w.err != "" || len(w.p.files) == 0 {
		return nil
	}
	return func(c *ui.Context) { diffview.Tools(c, w.diff) }
}

// heading is the title in two parts: what the source is, and the detail that tells it apart.
func (w *Window) heading(t *words) (main, sub string) {
	switch w.src.Kind {
	case "commit":
		if d := w.p.commit; d != nil {
			return d.Subject, short(d.Hash)
		}
		return t.Commit, short(w.src.Hash)
	case "wc":
		if w.src.Worktree == "" {
			return t.Wc, ""
		}
		return t.Wc, path.Base(w.src.Worktree)
	case "agg":
		main = t.Whole
		if a := w.src.Agg; a != nil {
			switch {
			case w.src.Only || a.DiffMode == session.DiffWorktree:
				main = t.Wc
			case a.DiffMode == session.DiffStage:
				main = fmt.Sprintf(t.Stage, w.p.stage)
			default:
				sub = w.rangeOf(t)
			}
			if a.Requirement != "" {
				main = fmt.Sprintf(t.Req, a.Requirement, main)
			}
		}
		return main, sub
	}
	return path.Base(w.src.Path), ""
}

// rangeOf is base…branch of the row the current file belongs to, "" when the row has none.
func (w *Window) rangeOf(t *words) string {
	i := w.diff.Current()
	if i < 0 || i >= len(w.p.files) {
		return ""
	}
	f, ok := w.p.files[i].Source.(session.AggFile)
	if !ok {
		return ""
	}
	for _, row := range w.p.rows {
		if row.Key == f.Key && row.Base != "" && row.Branch != "" && row.Skip == "" {
			return fmt.Sprintf(t.Range, row.Base, row.Branch)
		}
	}
	return ""
}

// status is the window's status bar: the commit's author and time, and how to step through the files; nil when it has
// nothing to say.
func (w *Window) status() func(c *ui.Context) {
	if !w.loaded || w.err != "" || len(w.p.files) < 2 && w.p.commit == nil {
		return nil
	}
	return func(c *ui.Context) {
		t := tr.Of(c)
		if d := w.p.commit; d != nil {
			ui.Text(c, d.Author).PaddingX(4).SingleLine().Shrink(1).MinWidth(0)
			ui.Text(c, time.Unix(d.Time, 0).Format("2006/01/02 15:04")).FontFeatures("tnum").SingleLine().Shrink(0)
		}
		ui.Spacer(c)
		if len(w.p.files) > 1 {
			ui.Text(c, t.StepHint).SingleLine().Shrink(0)
		}
	}
}

func areaName(t *words, area string) string {
	switch area {
	case "staged":
		return t.Staged
	case "unstaged":
		return t.Unstaged
	case "untracked":
		return t.Untrack
	}
	return area
}

func short(hash string) string { return hash[:min(len(hash), 7)] }

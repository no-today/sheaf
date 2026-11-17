package track

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/shell"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/view"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

// Panes puts the batch actions in the title bar, requirements on the left, and changes or commits in the middle.
func (p *Page) Panes(c *ui.Context) view.Panes {
	p.refreshIfHidden()
	w := tr.Of(c)
	p.w = w
	return view.Panes{
		Left:      []shell.Pane{{ID: "tags", Title: w.Filters, Icon: theme.IconSidebar, Bare: true, Body: p.leftBody}},
		Main:      shell.Pane{ID: "track", Bare: true, Body: p.mainBody},
		Status:    p.statusBar,
		StatusEnd: func(c *ui.Context) { p.outlineToggle(c, tr.Of(c)) },
		Title: func(c *ui.Context) {
			s := p.shown()
			s.titleSummary(c)
			ui.Spacer(c)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Disabled(s != p).DimDisabled(false).Children(func() {
				s.titleActions(c, tr.Of(c))
			})
		},
	}
}

func (p *Page) statusBar(c *ui.Context) {
	w := tr.Of(c)
	ui.Text(c, p.h.Info().Name).PaddingX(8)
	crumb := func(s string) ui.Element {
		ui.Icon(c, theme.IconChevRight).FontSize(theme.IconS).TextColor(theme.Text5)
		return ui.Text(c, s).PaddingX(4)
	}
	crumb(i18n.Of(c).Track)
	if n := p.tagName(w); n != "" {
		crumb(n)
	}
	ui.Spacer(c)
}

// shows: a requirement or a filter is on screen with its overview.
func (p *Page) shows() bool {
	id := p.picked()
	return p.ready() && id != "" && id != wcID && p.res != nil
}

// titleActions are the batch actions on the rows that can take them. They always show, greyed out with the reason as
// their tooltip when there is nothing for them to act on.
func (p *Page) titleActions(c *ui.Context, w *words) {
	why := ""
	switch {
	case !p.shows():
		why = w.ActNeedReq
	case len(p.rows) == 0:
		why = w.ActNoRows
	}
	n := len(p.ticked())
	tickWhy := why
	if tickWhy == "" && n == 0 {
		tickWhy = w.ActNoTicked
	}
	tip := func(hint, why string) string {
		if why != "" {
			return why
		}
		return hint
	}
	if titleAction(secondary(widgets.Button(c.Key("fetch"), w.Fetch), why == ""), tip(w.FetchHint, why), why != "") {
		p.fetch()
	}
	planning := p.sheet.planning() // a second click would only start the same plan again
	if titleAction(secondary(widgets.Button(c.Key("sync"), w.SyncAction), tickWhy == ""), tip(w.SyncHint, tickWhy), tickWhy != "" || planning) {
		p.startSync()
	}
	var mr ui.Element
	ui.Box(c).Shrink(0).Children(func() {
		mr = widgets.PrimaryButton(c.Key("mr"), w.MrAction).PaddingStart(12).PaddingEnd(12 + countSlot)
		if tickWhy != "" {
			greyed(mr).Background(theme.Accent)
		}
		switch {
		case planning:
			ui.Spinner(c).Absolute().Top((theme.HSelect-theme.IconS)/2).Right(10).Size(theme.IconS, theme.IconS).TextColor(theme.OnAccent)
		case n > 0:
			countBadge(c, n)
		}
		if n > 0 {
			mr.Label(fill(w.MrCount, "n", n))
		}
	})
	if titleAction(mr, tip(w.MrHint, tickWhy), tickWhy != "" || planning) {
		p.startMR()
	}
	if p.sheet != nil {
		p.sheetPopover(c)
	}
}

// outlineToggle is the status bar's button, at its bottom-right corner below the outline, that shows or hides the outline beside the
// stream; ⌥⌘B does the same. It is there only while the outline can be: the window is wide enough and there are files.
func (p *Page) outlineToggle(c *ui.Context, w *words) {
	if width, _ := c.Size(); width < outlineFrom || len(p.rows) == 0 || len(p.stream.Files()) == 0 {
		return
	}
	folded := &p.h.Dock().Folded[splitOutline]
	label := w.HideOutline
	if *folded {
		label = w.ShowOutline
	}
	if shell.PaneToggle(c.Key("outline-toggle"), theme.IconSidebarRight, label+"  ⌥⌘B", !*folded).Clicked() {
		*folded = !*folded
	}
	widgets.Shortcuts(c, widgets.Shortcut{Mods: ui.Alt | ui.Cmd, Key: ui.KeyB, Do: func() { *folded = !*folded }})
}

// showsOutline: the outline is beside the stream: the window is wide enough, there are files, the user did not hide it.
func (p *Page) showsOutline(width float32) bool {
	return width >= outlineFrom && len(p.rows) > 0 && len(p.stream.Files()) > 0 && !p.h.Dock().Folded[splitOutline]
}

// countSlot is the room the MR button always keeps for its count, so the button keeps its width whatever the number.
const countSlot = 22

// countBadge is the number of rows an action takes, in the slot at the end of the button.
func countBadge(c *ui.Context, n int) {
	ui.Row(c).Absolute().Top((theme.HSelect - 16) / 2).Right(8).Height(16).MinWidth(16).PaddingX(4).Center().
		Background(theme.FrameBg).Children(func() {
		ui.Text(c, itoa(n)).FontSize(theme.FSLabel).FontWeight(600).FontFeatures("tnum").
			FixedLineHeight(14).TextColor(theme.TextStrong).SingleLine()
	})
}

// titleSummary names what the window shows: the uncommitted changes, or the requirement or branch on screen with its
// lines changed, else the workspace. The repos are listed on the left, so the title does not count them.
func (p *Page) titleSummary(c *ui.Context) {
	t := p.temp
	if p.picked() == wcID {
		ui.Text(c, tr.Of(c).Uncommitted).FontWeight(600).TextColor(theme.TextStrong).PaddingX(8).SingleLine()
		return
	}
	if !p.shows() || t == nil {
		ui.Text(c, p.h.Info().Name).FontWeight(600).TextColor(theme.Text).PaddingX(8).SingleLine()
		return
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).Shrink(1).MinWidth(0).PaddingStart(8).Children(func() {
		ui.Text(c, t.name()).Font(theme.MonoFont).FontWeight(600).TextColor(theme.TextStrong).SingleLine().Shrink(0)
		if t.Title != "" && t.Title != t.name() {
			ui.Text(c, t.Title).TextColor(theme.Text).SingleLine().Shrink(1).MinWidth(0)
		}
		if add, del, ok := p.totals(); ok {
			ui.RichText(c,
				ui.Span{Text: "+" + itoa(add), Color: theme.Add},
				ui.Span{Text: " "},
				ui.Span{Text: "−" + itoa(del), Color: theme.Del},
			).FontFeatures("tnum").SingleLine().Shrink(0)
		}
	})
}

// titleAction reports a click on b, a title bar action whose tooltip is tip. When off, b reports no click but keeps its
// tooltip, which then says why it is off.
func titleAction(b ui.Element, tip string, off bool) bool {
	return b.Tooltip(tip).Clicked() && !off
}

// secondary is a hairline action of the title bar, greyed out when not on.
func secondary(b ui.Element, on bool) ui.Element {
	if !on {
		return greyed(b)
	}
	return hairline(b)
}

// greyed shows b as unavailable without disabling it, so its tooltip still shows.
func greyed(b ui.Element) ui.Element {
	return b.AccessDisabled(true).Opacity(0.45).Cursor(ui.CursorDefault).Background(ui.Transparent)
}

// hairline is a secondary action of the title bar: its outline lights up under the pointer.
func hairline(b ui.Element) ui.Element {
	b.PaddingX(12).Shrink(0).Background(ui.Transparent)
	if b.Hovered() {
		b.Border(1, theme.LineHover).TextColor(theme.TextStrong)
	}
	return b
}

func (p *Page) mainBody(c *ui.Context) {
	w := tr.Of(c)
	empty := func(s string) {
		ui.Column(c).Grow(1).Center().Gap(10).Padding(16).Children(func() {
			ui.Text(c, s).TextColor(theme.Text4).TextAlign(ui.Center)
			if p.aggErr != "" {
				p.refreshStatus(c, w)
			}
		})
	}
	switch id := p.picked(); {
	case !p.h.State().Ready:
		widgets.EmptyState(c, theme.IconPulse, w.BuildingIndex, w.BuildingIndexHint, nil)
		p.h.Painted()
	case !p.ready():
		empty(w.Loading)
		p.h.Painted()
	case id == wcID:
		p.dirty.body(c, p)
	case id == "":
		if p.reqsOK && len(p.reqs) == 0 && len(p.cands) == 0 {
			p.introduction(c, w)
		} else {
			widgets.EmptyState(c, theme.IconTag, w.None, w.NoneHint, nil)
		}
		p.h.Painted()
	case p.aggErr != "":
		empty(fill(w.Error, "reason", p.aggErr))
		p.h.Painted()
	case p.prev != nil && p.shown() != p:
		p.drawPrev(c, w)
	case p.res == nil:
		empty(w.Loading)
	default:
		p.page(c, w)
		p.h.Painted()
	}
}

func (p *Page) introduction(c *ui.Context, w *words) {
	pattern := p.h.Info().Requirement
	if pattern == "" {
		pattern = session.DefaultRequirement
	}
	widgets.EmptyState(c, theme.IconTag, w.Intro, w.IntroHint, func() {
		ui.Column(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fill(w.IntroPattern, "pattern", pattern)).Font(theme.MonoFont).FontSize(theme.FSSmall).
				TextColor(theme.Text3).TextAlign(ui.Center).Wrap()
			if widgets.Button(c, w.IntroSettings).Clicked() {
				if err := p.h.Windows().OpenSettings("me"); err != nil {
					p.h.Toasts().Error(err.Error())
				}
			}
		})
	})
}

// drawPrev keeps the previous entry's page on screen, untouchable, while the chosen one loads; it dims only when the
// load takes long enough to be noticed.
func (p *Page) drawPrev(c *ui.Context, w *words) {
	if p.prevAt.IsZero() {
		p.prevAt = c.Now()
	}
	opacity := float32(1)
	if wait := p.prevAt.Add(switchDim).Sub(c.Now()); wait > 0 {
		c.After(wait)
	} else {
		opacity = 0.6
	}
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Disabled(true).DimDisabled(false).Opacity(opacity).Children(func() {
		p.prev.page(c, w)
	})
}

// switchDim is how long a switch shows the previous page as it was before dimming it.
const switchDim = 200 * time.Millisecond

// page is the tabs over the changes or commits, and the outline while the window is wide enough.
func (p *Page) page(c *ui.Context, w *words) {
	width, _ := c.Size()
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Opacity(p.dim()).Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			p.tabs(c, w)
			p.doneBar(c, w)
			switch {
			case len(p.rows) == 0 && p.shownReq() != "":
				widgets.EmptyState(c, theme.IconSearch, fill(w.ReqEmpty, "id", p.shownReq()), w.ReqEmptyHint, nil)
			case len(p.rows) == 0:
				widgets.EmptyState(c, theme.IconSearch, w.Empty, w.EmptyHint, nil)
			case p.tab == 1:
				p.commitsList(c, w)
			default:
				p.changes(c, w)
			}
		})
		if p.showsOutline(width) {
			if w := p.outlineW(); widgets.Divider(c.Key("outline-divider"), &w, -1, false) {
				p.h.Dock().Splits[splitOutline] = max(outlineMin, min(w, outlineMax))
			}
			p.outlineList(c)
		}
	})
	widgets.Shortcuts(c,
		widgets.Shortcut{Mods: ui.Alt, Key: ui.KeyDown, Do: func() { p.tab = 0; p.stream.Next() }},
		widgets.Shortcut{Mods: ui.Alt, Key: ui.KeyUp, Do: func() { p.tab = 0; p.stream.Prev() }},
	)
}

// changes is the stream of every file shown.
func (p *Page) changes(c *ui.Context, w *words) {
	p.syncStream()
	switch {
	case p.filesErr != "":
		emptyNote(c, fill(w.Error, "reason", p.filesErr))
	case p.diffPending():
		ui.Column(c).Grow(1).Center().Children(func() { widgets.DelayedSpinner(c, "loading", true) })
	case len(p.stream.Files()) == 0 && (p.filesBusy || len(p.shownFiles()) > 0):
		ui.Column(c).Grow(1).Center().Children(func() { widgets.DelayedSpinner(c, "loading", true) })
	case len(p.stream.Files()) == 0:
		widgets.EmptyState(c, theme.IconFiles, w.NoFiles, w.NoFilesHint, nil)
	default:
		p.stream.View(c).Grow(1).MinHeight(0).FillWidth()
	}
}

func emptyNote(c *ui.Context, s string) {
	ui.Column(c).Grow(1).Center().Padding(16).Children(func() {
		ui.Text(c, s).TextColor(theme.Text4).TextAlign(ui.Center)
	})
}

// dim is the opacity of the page while a newer overview is being read: the previous one stays on screen, dimmed.
func (p *Page) dim() float32 {
	if p.aggBusy {
		return 0.6
	}
	return 1
}

// tabs are 改动 and 提交, and on the right what the changes are compared with and which of them show.
func (p *Page) tabs(c *ui.Context, w *words) {
	width, _ := c.Size()
	if p.showsOutline(width) {
		width -= p.outlineW() + theme.Gap
	}
	if p.h.Dock().Left.Open {
		width -= p.h.Dock().Left.Size + theme.Gap
	}
	width -= 2 * theme.Gap
	gap, padding := float32(22), float32(20)
	if width < 760 {
		gap, padding = 12, 12
	}
	ui.Row(c).Height(38).Shrink(0).PaddingX(padding).Gap(gap).AlignItems(ui.Center).Border(1, theme.Line).BorderWidth(0, 0, 1, 0).Children(func() {
		files := "…"
		if !p.filesBusy || len(p.files) > 0 {
			files = itoa(len(p.shownFiles()) - p.worktreeFiles())
		}
		commits := "…"
		if p.allCommits != nil {
			commits = itoa(p.allCommits.Total)
		}
		p.tab0(c, 0, w.TabChanges+" "+files)
		p.tab0(c, 1, w.TabCommits+" "+commits)
		p.refreshStatus(c, w)
		ui.Spacer(c)
		ui.Row(c).Gap(6).AlignItems(ui.Center).FontSize(theme.FSSmall).Shrink(1).MinWidth(0).Children(func() {
			p.diffControls(c, w, width >= 760)
		})
	})
}

// tab0 is one tab: lit with a hairline under it when on.
func (p *Page) tab0(c *ui.Context, i int, label string) {
	b := pageTab(c, "tab"+itoa(i), label, p.tab == i)
	if b.Clicked() {
		p.tab = i
		p.refreshIfHidden()
		if p.side == 1 {
			p.side = 0
			p.setFocus("")
		}
	}
}

func pageTab(c *ui.Context, key, label string, on bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(38).Label(label).Cursor(ui.CursorPointer)
	if on {
		b.Border(1, theme.TextStrong).BorderWidth(0, 0, 1, 0)
	}
	b.Children(func() {
		color := theme.Text4
		switch {
		case on:
			color = theme.TextStrong
		case b.Hovered():
			color = theme.Text
		}
		ui.Text(c, label).FontFeatures("tnum").TextColor(color).SingleLine()
	})
	return b
}

// optButton is a setting of the tab row: its name, and its value, lit under the pointer.
func optButton(c *ui.Context, key, label, value, hint string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Gap(6).Label(label + " " + value).Tooltip(hint).Cursor(ui.CursorPointer)
	b.Children(func() {
		ui.Text(c, label).TextColor(theme.Text4).SingleLine()
		v := ui.Text(c, value).TextColor(theme.Text).SingleLine().Shrink(1).MinWidth(0)
		if b.Hovered() {
			v.TextColor(theme.TextStrong)
		}
	})
	return b
}

func valueOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// doneBar is the summary of the last batch action, closed with its ✕.
func (p *Page) doneBar(c *ui.Context, w *words) {
	show := func(head, text string, cl func()) {
		ui.Row(c).Height(30).PaddingX(20).Gap(8).AlignItems(ui.Center).Border(1, theme.Line).BorderWidth(0, 0, 1, 0).Children(func() {
			ui.Text(c, head).FontWeight(600).FontSize(theme.FSSmall)
			ui.Text(c, text).FontSize(theme.FSSmall).FontFeatures("tnum").TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).Tooltip(text)
			ui.Spacer(c)
			if widgets.IconButton(c.Key(head), theme.IconClose, w.Close).Clicked() {
				cl()
			}
		})
	}
	if p.showSync() {
		show(w.SyncDone, syncSummary(w, *p.syncRun), func() { p.syncRun = nil })
	}
	if p.mrResults() != nil {
		show(w.MrDone, mrSummary(w, *p.mrRun), func() { p.mrRun = nil })
	}
}

func itoa(n int) string { return i18n.Thousands(n) }

// fetch runs git fetch in the repos of the rows, at once (a fetch changes no branch), and says in a toast what happened.
func (p *Page) fetch() {
	var repos []string
	seen := map[string]bool{}
	for _, r := range p.rows {
		if !seen[r.Repo] {
			seen[r.Repo] = true
			repos = append(repos, r.Repo)
		}
	}
	w, s, ctx, subject := p.words(), p.s, p.h.Context(), p.subject()
	go func() {
		run, err := s.FetchAll(ctx, repos, subject)
		p.h.Update(func() { // progress and each repo's outcome are in the task list; only a failure speaks up
			switch {
			case err != nil:
				p.h.Toasts().TaskError(fill(w.FetchFailed, "reason", err.Error()))
			case run.Failed > 0:
				p.h.Toasts().TaskError(fetchSummary(w, run) + "\n" + fetchFailures(run))
			}
		})
	}()
}

// subject is what the task list calls this page's runs: the requirement or branch on screen.
func (p *Page) subject() string {
	if p.temp == nil {
		return ""
	}
	return p.temp.name()
}

func fetchSummary(w *words, run session.FetchRun) string {
	return joinCounts([]int{run.Fetched, run.NoRemote, run.Failed}, []string{w.FetchSumDone, w.FetchSumNoRemote, w.SumFailed})
}

func fetchFailures(run session.FetchRun) string {
	var lines []string
	for _, r := range run.Rows {
		if r.Outcome == "failed" {
			lines = append(lines, r.Repo+": "+failReason(r.Error))
		}
	}
	return strings.Join(lines, "\n")
}

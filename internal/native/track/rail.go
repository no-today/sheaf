package track

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

// leftBody lists the tracked requirements, the current requirement's repo stages, uncommitted changes and an opened branch.
func (p *Page) leftBody(c *ui.Context) {
	w := tr.Of(c)
	needle := strings.ToLower(strings.TrimSpace(p.find))
	hit := func(s ...string) bool {
		for _, x := range s {
			if needle == "" || strings.Contains(strings.ToLower(x), needle) {
				return true
			}
		}
		return false
	}
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		find := widgets.Caret(ui.TextInputBase(c.Key("find"), &p.find)).Height(30).Margin(12, 12, 12, 12).PaddingX(10).
			Border(1, theme.Line).Background(ui.Transparent).Placeholder(w.FindReq).Label(w.FindReq)
		if find.Shortcut(0, ui.KeyEscape) {
			p.find = ""
		}
		if p.focusFind {
			find.Focus()
			p.focusFind = false
		}
		if find.Submitted() {
			p.openFound(hit)
		}
		ui.Themed(c, theme.ListTheme()).Background(ui.Transparent).AlignItems(ui.Stretch).Shrink(0).Padding(0, 0, 4, 0).Children(func() {
			if hit(w.Uncommitted) {
				p.entryRow(c, wcID, w.Uncommitted, w.UncommittedHint).Children(func() {
					ui.Text(c, w.Uncommitted).SingleLine().Grow(1).Shrink(1).MinWidth(0)
					if n := p.dirtyDirs(); n > 0 {
						railCount(c, i18n.Thousands(n))
					}
				})
			}
		})
		// The requirements and the picked one's repos are two panes of fixed share, each scrolling on its own, so
		// picking another requirement never moves the list above.
		top := p.railTopHeight(c)
		ui.Scroll(c.Key("reqs")).Height(top).Shrink(0).MinHeight(0).Padding(0, 0, 12, 0).Border(1, theme.Line).BorderWidth(1, 0, 0, 0).Children(func() {
			ui.Themed(c, theme.ListTheme()).Background(ui.Transparent).AlignItems(ui.Stretch).Children(func() {
				p.reqGroups(c, w, hit)
				p.trackable(c, w, hit)
				p.textEntry(c, w)
				if p.temp != nil && p.temp.Req == "" && hit(p.temp.name()) {
					railRule(c)
					p.entryRow(c, tmpID, p.temp.name(), w.TempHint).Children(func() {
						ui.Text(c, p.temp.name()).SingleLine().Grow(1).Shrink(1).MinWidth(0)
						railCount(c, w.Unsaved)
					})
				}
			})
		})
		if widgets.Divider(c.Key("rail-divider"), &top, 1, true) {
			p.h.Dock().Splits[splitRail] = top
			p.h.Dock().Splits[splitRail] = p.railTopHeight(c)
		}
		p.reposPane(c, w)
	})
}

// railTopHeight is the requirements pane's height: as the user dragged it, else half the room, kept so that both panes
// stay usable.
func (p *Page) railTopHeight(c *ui.Context) float32 {
	_, h := c.Size()
	room := h - theme.HTitleBar - theme.HStatusBar - railHead
	top := p.h.Dock().Splits[splitRail]
	if top <= 0 {
		top = room / 2
	}
	return max(railPaneMin, min(top, room-railPaneMin))
}

// railHead is the height of what sits above the two panes (the find field and 未提交改动); railPaneMin the least
// either pane keeps.
const (
	railHead    = 96
	railPaneMin = 80
)

// reposPane is the lower pane: the repos of the requirement shown, under a heading that stays while they scroll.
func (p *Page) reposPane(c *ui.Context, w *words) {
	s := p.shown()
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).
		Disabled(s != p).DimDisabled(false).Children(func() {
		if !s.hasStages() {
			railGroup(c, w.ReposPane)
			ui.Text(c, w.ReposPaneEmpty).FontSize(theme.FSSmall).TextColor(theme.Text5).Wrap().Padding(8, 16, 0, 16)
			return
		}
		railGroup(c, fill(w.InRepos, "name", s.tagName(w)))
		ui.Scroll(c.Key("repos")).Grow(1).MinHeight(0).Padding(4, 0, 12, 0).Children(func() {
			ui.Themed(c, theme.ListTheme()).Background(ui.Transparent).AlignItems(ui.Stretch).Children(func() {
				s.repoStages(c, w)
			})
		})
	})
}

func (p *Page) hasStages() bool {
	return p.res != nil && len(p.rows) > 0 && p.picked() != wcID
}

func (p *Page) stageName(w *words, stage string) string {
	switch stage {
	case session.StageArchived:
		return w.StageArchived
	case session.StageUnknown:
		return w.StageUnknown
	case session.StageReading:
		return w.StageReading
	case session.StageLanded:
		return w.StageLanded
	}
	if index, ok := session.MergeStageIndex(stage); ok && index < len(p.flow) {
		return fill(w.StageAwaiting, "branch", p.flow[index].Name)
	}
	return w.StageDev
}

func (p *Page) reqGroups(c *ui.Context, w *words, hit func(...string) bool) {
	if p.reqsOK && len(p.reqs) == 0 {
		if strings.TrimSpace(p.find) == "" {
			railGroup(c, w.TrackHint)
		}
		return
	}
	shown := p.shownReq()
	searching := strings.TrimSpace(p.find) != ""
	for _, stage := range session.StageOrder(p.flow) {
		head := false
		folded := stage == session.StageArchived && !p.archivedOpen
		for _, r := range p.reqs {
			if r.Stage != stage || !hit(r.ID, r.Title) {
				continue
			}
			if !head {
				if stage == session.StageArchived {
					p.archivedHead(c, w, folded && !searching)
				} else {
					railGroup(c, p.stageName(w, stage))
				}
				head = true
			}
			if folded && !searching && r.ID != shown {
				continue
			}
			if r.ID == p.renaming {
				p.renameRow(c, w, r)
				continue
			}
			selected := r.ID == shown
			row := widgets.RowLook(ui.ButtonBase(c.Key("req "+r.ID)).Justify(ui.Start).Height(26).PaddingX(16).Gap(8).Label(r.ID+" "+r.Title), selected)
			if row.Clicked() {
				p.chooseReq(r)
			}
			widgets.ContextMenu(c, row, func(m *ui.Menu) {
				if m.Item(w.Rename).Chosen() {
					p.renaming, p.renameText, p.renameFocus = r.ID, r.Title, false
					c.Invalidate()
				}
				archive := r.Stage != session.StageArchived
				action := w.Archive
				if !archive {
					action = w.Restore
				}
				if m.Item(action).Chosen() {
					p.archiveRequirement(r.ID, archive)
				}
				if m.Item(w.Untrack).Chosen() {
					p.untrack(r.ID)
				}
			})
			row.Children(func() {
				id := ui.Text(c, r.ID).FontWeight(500).Width(76).SingleLine().Shrink(0)
				if selected {
					id.TextColor(theme.TextStrong)
				}
				ui.Text(c, r.Title).TextColor(theme.RowText(selected, theme.Text4)).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				railCount(c, i18n.Thousands(r.Repos))
			})
		}
	}
}

// openFound is Enter in the search field: the first tracked requirement it hits; else, for a whole requirement number,
// start tracking it; else the commits whose message holds the text.
func (p *Page) openFound(hit func(...string) bool) {
	for _, stage := range session.StageOrder(p.flow) {
		for _, r := range p.reqs {
			if r.Stage == stage && hit(r.ID, r.Title) {
				p.chooseReq(r)
				return
			}
		}
	}
	if id, ok := p.normalize(p.find); ok {
		p.track(id, "")
		return
	}
	p.chooseText(p.find)
}

// maxTrackable is how many candidates the search lists.
const maxTrackable = 8

// trackable lists, while something is typed or nothing is tracked yet, the numbers found in working directory branches
// and my recent commits that are not tracked, and the typed number itself when it is none of them. A row opens the
// requirement to look at; its button adds it to the tracked ones.
func (p *Page) trackable(c *ui.Context, w *words, hit func(...string) bool) {
	typed := strings.TrimSpace(p.find)
	if !p.reqsOK || (typed == "" && len(p.reqs) > 0) {
		return
	}
	var list []session.Candidate
	for _, k := range p.cands {
		if len(list) < maxTrackable && hit(k.ID, k.Title) {
			list = append(list, k)
		}
	}
	extra, isID := p.normalize(typed)
	isID = isID && typed != ""
	for _, k := range p.cands {
		isID = isID && k.ID != extra
	}
	for _, r := range p.reqs {
		isID = isID && r.ID != extra
	}
	if len(list) == 0 && !isID {
		return
	}
	railGroup(c, w.Trackable)
	shown := p.shownReq()
	for _, k := range list {
		p.trackRow(c, w, k.ID, k.Title, k.ID == shown, w.Track)
	}
	if isID {
		p.trackRow(c, w, extra, "", extra == shown, w.Track)
	}
}

// trackRow is a number not tracked: the row looks at it, the text button on its right tracks it.
func (p *Page) trackRow(c *ui.Context, w *words, id, title string, selected bool, action string) {
	ui.Row(c).Height(26).AlignItems(ui.Center).Children(func() {
		row := widgets.RowLook(ui.ButtonBase(c.Key("cand "+id)).Justify(ui.Start).Grow(1).Shrink(1).MinWidth(0).Height(26).PaddingX(16).Gap(8).Label(id+" "+title), selected)
		if row.Clicked() {
			p.chooseReq(session.Requirement{ID: id, Title: title})
		}
		row.Children(func() {
			ui.Text(c, id).FontWeight(500).Width(76).SingleLine().Shrink(0)
			ui.Text(c, title).TextColor(theme.RowText(selected, theme.Text4)).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		})
		add := widgets.RowLook(ui.ButtonBase(c.Key("track "+id)).Height(26).Padding(0, 16, 0, 8).Label(fill(w.TrackLabel, "id", id)), false)
		if add.Clicked() {
			p.track(id, title)
		}
		add.Children(func() {
			ui.Text(c, action).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(0)
		})
	})
}

// textEntry is the last line of the requirement list while something is typed in the search field: it opens the
// commits whose message holds the text, for a fix that carries no requirement number.
func (p *Page) textEntry(c *ui.Context, w *words) {
	text := strings.TrimSpace(p.find)
	if text == "" {
		return
	}
	label := fill(w.FindText, "q", text)
	row := widgets.RowLook(ui.ButtonBase(c.Key("find text")).Justify(ui.Start).Height(26).PaddingX(16).Gap(8).Label(label),
		p.picked() == tmpID && p.temp != nil && p.temp.Text == text)
	if row.Clicked() {
		p.chooseText(text)
	}
	row.Children(func() {
		ui.Text(c, label).SingleLine().Grow(1).Shrink(1).MinWidth(0)
	})
}

func (p *Page) archivedHead(c *ui.Context, w *words, folded bool) {
	n := 0
	for _, r := range p.reqs {
		if r.Stage == session.StageArchived {
			n++
		}
	}
	icon := theme.IconChevDown
	if folded {
		icon = theme.IconChevRight
	}
	name := w.StageArchived
	b := ui.ButtonBase(c.Key("archived")).Justify(ui.Start).Padding(10, 16, 2, 16).Gap(4).Label(name) // as tall as railGroup
	if b.Clicked() {
		p.archivedOpen = !p.archivedOpen
	}
	b.Children(func() {
		ui.Text(c, name).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(0)
		ui.Icon(c, icon).FontSize(theme.IconS).TextColor(theme.Text4)
		ui.Spacer(c)
		railCount(c, i18n.Thousands(n))
	})
}

// renameRow is a requirement's row while its title is edited: Enter saves, Esc or leaving the field drops the edit.
func (p *Page) renameRow(c *ui.Context, w *words, r session.Requirement) {
	ui.Row(c).Height(26).PaddingX(16).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, r.ID).FontWeight(500).Width(76).SingleLine().Shrink(0)
		in := widgets.Caret(ui.TextInputBase(c.Key("rename "+r.ID), &p.renameText)).Grow(1).MinWidth(0).Height(24).PaddingX(6).
			Border(1, theme.Line).Background(ui.Transparent).Label(w.RenameLabel).AutoFocus()
		focused := in.Focused()
		switch {
		case in.Submitted():
			p.rename(r.ID, p.renameText)
			p.renaming = ""
		case in.Shortcut(0, ui.KeyEscape), p.renameFocus && !focused:
			p.renaming = ""
		}
		p.renameFocus = focused
	})
}

// At float32 layout precision, the stage reaches its ellipsis minimum before the repo loses width.
const repoStageShrink = 1e9

// repoStages scrolls the stream to the repo picked in the current requirement.
func (p *Page) repoStages(c *ui.Context, w *words) {
	if !p.hasStages() {
		return
	}
	mrs := p.mrResults()
	for _, r := range p.rows {
		b := widgets.RowLook(ui.ButtonBase(c.Key("stage "+r.Key)).Padding(3, 16, 3, 16).Label(r.Repo+" "+p.stageName(w, r.Stage)), false)
		if b.Clicked() {
			p.tab = 0
			p.scrollToRepo(r.Key)
		}
		b.Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					p.stageSteps(c, r.Stage)
					ui.Text(c, rowLabel(r.Key, r.Repo)).SingleLine().Shrink(1).MinWidth(0)
					stageName := p.stageName(w, r.Stage)
					ui.Text(c, stageName).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().
						Shrink(repoStageShrink).MinWidth(theme.FSSmall).Margin(0, 0, 0, ui.Auto).Tooltip(stageName)
				})
				ui.Row(c).FixedLineHeight(18).FontSize(theme.FSSmall).MinWidth(0).Children(func() {
					sep := false
					part := func(text string, color ui.Color, shrink float32) {
						if sep {
							ui.Text(c, "·").TextColor(theme.Text5).PaddingX(5).Shrink(0)
						}
						sep = true
						ui.Text(c, text).TextColor(color).SingleLine().Shrink(shrink).MinWidth(0)
					}
					if r.Uncommitted > 0 {
						part(fill(w.RowDirty, "n", r.Uncommitted), theme.Ticket, 0)
					}
					part(branchShown(w, r), theme.Text5, 1)
					if mr := mrs[r.Key]; mr.URL != "" {
						part(fill(w.RowMR, "mr", mrRef(mr.URL)), theme.Text5, 0)
					}
				})
			})
		})
	}
}

func (p *Page) stageSteps(c *ui.Context, stage string) {
	order := session.StageOrder(p.flow)
	order = slices.DeleteFunc(order, func(step string) bool {
		return step == session.StageUnknown || step == session.StageReading || step == session.StageArchived
	})
	reached := slices.Index(order, stage)
	ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
		for i := range order {
			color := theme.StepOff
			switch {
			case i <= reached && stage == session.StageDev:
				color = theme.Ticket
			case stage == session.StageUnknown || stage == session.StageReading:
				color = theme.Text4
			case i <= reached:
				color = theme.Text
			}
			ui.Box(c).Size(9, 9).Background(color)
		}
	})
}

func railGroup(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Padding(10, 16, 2, 16)
}

func railRule(c *ui.Context) {
	ui.Box(c).Height(1).FillWidth().Background(theme.Line).Margin(12, 0, 12, 0)
}

func railCount(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(theme.FSSmall).FontFeatures("tnum").TextColor(theme.Text4).SingleLine().Shrink(0)
}

func (p *Page) entryRow(c *ui.Context, id, name, hint string) ui.Element {
	row := widgets.RowLook(ui.ButtonBase(c.Key(id)).Justify(ui.Start).Height(26).PaddingX(16).Gap(8).Label(name), id == p.picked())
	if row.Clicked() {
		p.choose(id)
	}
	if hint != "" {
		row.Tooltip(hint)
	}
	return row
}

package track

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

// aggRow is the Row hook of a tree of AggFiles: the uncommitted / new badge, and the context menu and the double click
// that open the diff window (open; the files tab gives p.review, the uncommitted page its own). The double click is the
// row's (RowDoubleClicked): the click itself stays with the outline's row, which chooses the file.
func (p *Page) aggRow(w *words, files []diffview.File, open func(session.AggFile)) func(c *ui.Context, row ui.Element, i int) {
	return func(c *ui.Context, row ui.Element, i int) {
		f := files[i].Source.(session.AggFile)
		if f.Uncommitted {
			badge := w.WcBadge
			if f.Untracked {
				badge = w.NewBadge
			}
			ui.Text(c, badge).FontSize(theme.FSLabel).TextColor(theme.Ticket)
		}
		if row.RowDoubleClicked() {
			open(f)
		}
		widgets.ContextMenu(c, row, func(m *ui.Menu) {
			if m.Item(i18n.Of(c).Diff).Chosen() {
				open(f)
			}
		})
	}
}

// commitsList is the commits tab: the rows' own commits (merges edited while merging marked, clean ones hidden and
// counted), or what the compare target has beyond one row (its ↓). Enter or a double click opens the commit's diff.
func (p *Page) commitsList(c *ui.Context, w *words) {
	l, down := p.commits, p.focus != "" && p.side == 1
	if down {
		l = p.down
	}
	if p.commitOf != l {
		p.commitOf, p.commitSel = l, -1
	}
	if l == nil {
		ui.Column(c).Grow(1).Center().Children(func() { widgets.DelayedSpinner(c, "loading", true) })
		return
	}
	if down {
		r, _ := p.rowOf(p.focus)
		ui.Row(c).Height(30).PaddingX(20).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, fill(w.DownHead, "b", valueOr(p.master, r.Base), "branch", branchShown(w, r), "n", l.Total)).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0)
			if optButton(c, "back", w.BackToOwn, "", "").Clicked() {
				p.side = 0
				p.setFocus("")
			}
		})
	}
	now := time.Now()
	ui.Themed(c, theme.ListTheme()).Grow(1).MinHeight(0).Background(ui.Transparent).AlignItems(ui.Stretch).Children(func() {
		if l.Total == 0 {
			if !down && l.Merges > 0 && !p.merges {
				widgets.EmptyState(c, theme.IconLog, w.MergesHiddenTitle, "", func() {
					if widgets.Button(c.Key("show-merges"), w.Show).Clicked() {
						p.toggleMerges()
					}
				})
			} else {
				widgets.EmptyState(c, theme.IconLog, w.NoCommits, w.NoCommitsHint, nil)
			}
			return
		}
		tail := !down && (l.Merges > 0 || p.merges)
		n := l.Total
		if tail {
			n++
		}
		list := ui.List(c, &p.commitSt, n, func(i int) {
			if i == l.Total {
				p.mergesNote(c, w, l)
				return
			}
			r, ok := p.commitRow(l, i)
			if !ok {
				ui.Row(c).Height(32)
				return
			}
			selected := p.commitSel == i
			ui.Row(c).Height(32).PaddingX(20).Gap(12).AlignItems(ui.Center).Border(1, theme.RowRule).BorderWidth(0, 0, 1, 0).Children(func() {
				ui.Text(c, r.Hash[:min(len(r.Hash), 8)]).TextColor(theme.RowText(selected, theme.Text4)).SingleLine().Width(84).Shrink(0)
				ui.Text(c, r.Repo).TextColor(theme.RowText(selected, theme.Text4)).SingleLine().Width(118).Shrink(0)
				ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
					subject := ui.Text(c, r.Subject).SingleLine().Shrink(1).MinWidth(0)
					if r.Resolved {
						ui.Text(c, w.Resolved).FontSize(theme.FSSmall).FixedLineHeight(18).TextColor(theme.Resolved).Border(1, theme.Resolved).PaddingX(6).SingleLine().Shrink(0).Tooltip(w.ResolvedHint)
						subject.TextColor(theme.ResolvedText)
					}
				})
				when := time.Unix(r.Time, 0).In(now.Location()).Format("01-02 15:04")
				ui.Text(c, r.Author+" · "+when).FontSize(theme.FSSmall).TextColor(theme.RowText(selected, theme.Text4)).SingleLine().Shrink(0)
			})
		}).Grow(1)
		if list.Submitted() && p.commitSel >= 0 && p.commitSel < l.Total {
			if r, ok := p.commitRow(l, p.commitSel); ok {
				p.openCommit(r)
			}
		}
	})
}

// mergesNote ends the commits: how many synced merges nobody edited are hidden, and a click shows them (or hides them again).
func (p *Page) mergesNote(c *ui.Context, w *words, l *commitList) {
	text := w.MergesShown
	if !p.merges {
		text = fill(w.MergesHidden, "n", l.Merges)
	}
	b := ui.ButtonBase(c.Key("merges")).Justify(ui.Start).FillWidth().Padding(12, 20, 12, 20).Label(text).Cursor(ui.CursorPointer)
	b.Children(func() {
		color := theme.Text4
		if b.Hovered() {
			color = theme.Text
		}
		ui.Text(c, text).FontSize(theme.FSSmall).TextColor(color).Wrap()
	})
	if b.Clicked() {
		p.toggleMerges()
	}
}

func (p *Page) toggleMerges() {
	p.merges = !p.merges
	if p.input != nil {
		p.loadCommits(*p.input)
	}
}

// popup is the one popover open on the page: a compare branch picker or a row's branch picker.
type popup struct {
	kind string // "release" | "master" | "branch"
	key  string // branch: the row
	q    string
	opts []session.Option
	ok   bool
	brs  []session.AggBranch
	seq  int
}

// maxShown caps the compare branches drawn: every local and remote branch can be hundreds; typing narrows them.
const maxShown = 100

// targetChip is a compare branch of the workspace (the same for every filter), picked from the branches the repos have.
func (p *Page) targetChip(c *ui.Context, w *words, kind, label, value, hint string) {
	open := p.pop.kind == kind
	b := optButton(c, "tgt"+kind, label, value, hint)
	if b.Clicked() {
		if open {
			p.pop = popup{}
		} else {
			p.openTargets(kind)
		}
		open = p.pop.kind == kind
	}
	if !open || !p.pop.ok { // opens once its branches are read, at its final height
		return
	}
	w2 := w
	ui.PopoverBase(c, b, &open, func(panel ui.Element) {
		widgets.Panel(panel).Margin(4, 0, 0, 0).Width(360).Children(func() { p.targetBody(c, w2, kind) })
	})
	if !open && p.pop.kind == kind {
		p.pop = popup{}
	}
}

// openTargets opens the picker of kind and reads the branches the repos have.
func (p *Page) openTargets(kind string) {
	p.pop = popup{kind: kind, seq: p.pop.seq + 1}
	seq, s := p.pop.seq, p.s
	go func() {
		opts, err := s.Options(query.Default(), session.KeyTarget)
		p.h.Update(func() {
			if p.pop.seq != seq || p.pop.kind != kind {
				return
			}
			p.pop.opts, p.pop.ok = opts, true
			_ = err
		})
	}()
}

func (p *Page) targetBody(c *ui.Context, w *words, kind string) {
	head, cur, anyLabel := w.TargetHead, p.master, w.CompareBase
	pick := func(b string) {
		p.pop = popup{}
		p.setBase(b)
	}
	ui.Text(c, head).FontSize(theme.FSLabel).FontWeight(600).TextColor(theme.Text4).PaddingX(8).Height(24)
	widgets.SearchField(c.Key("tq"), &p.pop.q).Placeholder(w.BranchSearch).Label(w.BranchSearch).AutoFocus().Margin(0, 4, 4, 4)
	favs, rest := targetGroups(p.pop.opts, p.favs, p.pop.q)
	option := func(o session.Option) {
		fav := false
		for _, f := range p.favs {
			fav = fav || f == o.Value
		}
		ui.Row(c.Key("o" + o.Value)).Height(theme.HMenuItem).PaddingX(4).Gap(6).AlignItems(ui.Center).Children(func() {
			star, color, tip := theme.IconStar, theme.Text4, w.Fav
			if fav {
				star, color, tip = theme.IconStarFill, theme.Ticket, w.Unfav
			}
			sb := ui.ButtonBase(c.Key("s"+o.Value)).Size(20, 20).Center().Label(tip).Tooltip(tip)
			sb.Children(func() { ui.Icon(c, star).FontSize(theme.IconS).TextColor(color) })
			if sb.Clicked() {
				p.setFavs(toggleFav(p.favs, o.Value))
			}
			selected := o.Value == cur
			row := ui.ButtonBase(c.Key("p" + o.Value)).Justify(ui.Start).Label(o.Value).Grow(1).Height(theme.HMenuItem).Gap(8).Radius(theme.RRow).PaddingX(4)
			widgets.PanelRowLook(row, selected)
			if row.Clicked() {
				pick(o.Value)
			}
			row.Children(func() {
				ui.Text(c, o.Value).Font(theme.MonoFont).SingleLine().Shrink(1).MinWidth(0)
				ui.Spacer(c)
				ui.Text(c, fill(w.TargetRepos, "n", o.Count)).FontSize(theme.FSLabel).FontFeatures("tnum").TextColor(theme.RowText(selected, theme.Text4)).SingleLine()
			})
		})
	}
	ui.Scroll(c).MaxHeight(360).Children(func() {
		if anyLabel != "" {
			row := widgets.RowLook(ui.ButtonBase(c).Justify(ui.Start).Label(anyLabel).Height(theme.HMenuItem).PaddingX(28).Radius(theme.RRow), cur == "")
			if row.Clicked() {
				pick("")
			}
			row.Children(func() { ui.Text(c, anyLabel).SingleLine() })
		}
		if len(favs) > 0 {
			ui.Text(c, w.Favs).FontSize(theme.FSLabel).FontWeight(600).TextColor(theme.Text4).PaddingX(8).Height(24)
			for _, o := range favs {
				option(o)
			}
			ui.Box(c).Height(1).Background(theme.Line).Margin(4, 0)
		}
		for i, o := range rest {
			if i == maxShown {
				ui.Text(c, fill(w.More, "n", len(rest)-maxShown)).FontSize(theme.FSSmall).TextColor(theme.Text4).PaddingX(8).Height(24)
				break
			}
			option(o)
		}
	})
	ui.Text(c, w.TargetFoot).FontSize(theme.FSLabel).TextColor(theme.Text4).Wrap().Padding(6, 8)
}

// branchButton is a row's branch in its heading; a click lists its candidates by the filter's commits they hold, 自动 on
// the inferred one, branches shown on another row of the same repo greyed out. Only this page: not saved with the
// filter. A merged row has no branch to pick: it says where its branch went.
func (p *Page) branchButton(c *ui.Context, w *words, r session.AggRepo) {
	text := branchShown(w, r)
	if r.BranchFrom == session.FromMerged {
		ui.Text(c, text).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0).MaxWidth(240).Tooltip(text)
		return
	}
	open := p.pop.kind == "branch" && p.pop.key == r.Key
	b := ui.ButtonBase(c.Key("br" + r.Key)).Gap(8).Shrink(1).MinWidth(0).MaxWidth(240).Label(text).Cursor(ui.CursorPointer).Tooltip(text + "\n" + fill(w.SwitchBranch, "repo", r.Repo))
	b.Children(func() {
		t := ui.Text(c, text).TextColor(theme.Text4).SingleLine().Shrink(1).MinWidth(0)
		if b.Hovered() || open {
			t.TextColor(theme.Text)
		}
		if r.BranchFrom == session.FromOverride {
			ui.Text(c, w.Manual).FontSize(theme.FSSmall).TextColor(theme.Ticket).Shrink(0)
		}
	})
	if b.Clicked() {
		if open {
			p.pop = popup{}
		} else {
			p.openBranches(r)
		}
		open = p.pop.kind == "branch" && p.pop.key == r.Key
	}
	if !open || !p.pop.ok { // opens once its branches are read, at its final height
		return
	}
	ui.PopoverBase(c, b, &open, func(panel ui.Element) {
		widgets.Panel(panel).Margin(4, 0, 0, 0).Width(360).Children(func() { p.branchBody(c, w, r) })
	})
	if !open && p.pop.kind == "branch" && p.pop.key == r.Key {
		p.pop = popup{}
	}
}

func (p *Page) openBranches(r session.AggRepo) {
	p.pop = popup{kind: "branch", key: r.Key, seq: p.pop.seq + 1}
	seq, s, in := p.pop.seq, p.s, *p.input
	go func() {
		list, err := s.TagAggBranches(in, r.Key)
		p.h.Update(func() {
			if p.pop.seq != seq {
				return
			}
			p.pop.brs, p.pop.ok = list, true
			_ = err
		})
	}()
}

func (p *Page) branchBody(c *ui.Context, w *words, r session.AggRepo) {
	ui.Text(c, fill(w.BranchesHead, "repo", r.Repo)).FontSize(theme.FSLabel).FontWeight(600).TextColor(theme.Text4).PaddingX(8).Height(24)
	widgets.SearchField(c.Key("bq"), &p.pop.q).Placeholder(w.BranchSearch).Label(w.BranchSearch).AutoFocus().Margin(0, 4, 4, 4)
	needle := strings.ToLower(strings.TrimSpace(p.pop.q))
	pick := func(b string) {
		p.pop = popup{}
		if b == r.Auto {
			b = ""
		}
		p.setBranch(r.Key, b)
	}
	ui.Scroll(c).MaxHeight(320).Children(func() {
		if len(p.pop.brs) == 0 {
			ui.Text(c, w.NoCandidates).FontSize(theme.FSSmall).TextColor(theme.Text4).PaddingX(8).Height(24)
		}
		for _, b := range p.pop.brs {
			if needle != "" && !strings.Contains(strings.ToLower(b.Name), needle) {
				continue
			}
			used := usedBy(p.rows, r, b.Name)
			selected := b.Name == r.Branch && used == 0
			row := ui.ButtonBase(c.Key("c" + b.Name)).Justify(ui.Start).Label(b.Name).Height(theme.HMenuItem).PaddingX(8).Gap(8).Radius(theme.RRow)
			widgets.PanelRowLook(widgets.Disable(row, used > 0), selected)
			if row.Clicked() {
				pick(b.Name)
			}
			row.Children(func() {
				if b.Name == r.Branch {
					ui.Icon(c, theme.IconCheck).FontSize(theme.IconS).TextColor(theme.AccentTxt).Width(theme.IconS)
				} else {
					ui.Box(c).Width(theme.IconS)
				}
				ui.Text(c, b.Name).Font(theme.MonoFont).SingleLine().Shrink(1).MinWidth(0)
				if b.Name == r.Auto {
					ui.Text(c, w.AutoMark).FontSize(theme.FSLabel).TextColor(theme.AccentTxt)
				}
				ui.Spacer(c)
				note := fill(w.Commits, "n", b.Commits)
				if used > 0 {
					note = fill(w.UsedBy, "n", used)
				}
				ui.Text(c, note).FontSize(theme.FSLabel).FontFeatures("tnum").TextColor(theme.RowText(selected, theme.Text4)).SingleLine()
			})
		}
		if r.BranchFrom == session.FromOverride && r.Auto != "" {
			ui.Box(c).Height(1).Background(theme.Line).Margin(4, 0)
			row := widgets.RowLook(ui.ButtonBase(c).Justify(ui.Start).Label(fill(w.BackToAuto, "b", r.Auto)).Height(theme.HMenuItem).PaddingX(28).Radius(theme.RRow), false)
			if row.Clicked() {
				pick(r.Auto)
			}
			row.Children(func() {
				ui.Text(c, fill(w.BackToAuto, "b", r.Auto)).Font(theme.MonoFont).SingleLine().Shrink(1).MinWidth(0)
			})
		}
	})
}

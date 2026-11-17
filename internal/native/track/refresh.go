package track

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

func (p *Page) reviewing() bool {
	if !p.visible || p.prev != nil {
		return false
	}
	if p.picked() == wcID {
		return p.dirty.tab == 0 && p.dirty.view != nil && len(p.dirty.view.Files) > 0
	}
	return p.tab == 0 && p.res != nil && len(p.stream.Files()) > 0
}

func (p *Page) holdChanges() bool {
	if !p.reviewing() {
		return false
	}
	p.pending = true
	if p.picked() == wcID {
		p.dirty.stream.SetPaused(true)
	} else {
		p.stream.SetPaused(true)
	}
	return true
}

func (p *Page) clearChanges() {
	p.pending = false
	p.stream.SetPaused(false)
	p.dirty.stream.SetPaused(false)
}

func (p *Page) refreshIfHidden() {
	if p.pending && !p.reviewing() {
		p.reload()
	}
}

func (p *Page) affectedRepos(repos []string) bool {
	if p.picked() == wcID {
		return false
	}
	for _, row := range p.rows {
		if slices.Contains(repos, row.Repo) {
			return true
		}
	}
	// A repo the requirement has no row in yet may just have received its first commit for it.
	req := p.shownReq()
	if req == "" {
		return false
	}
	for _, repo := range p.h.Info().Repos {
		if !slices.Contains(repos, repo.Name) {
			continue
		}
		for _, wt := range repo.Worktrees {
			if wt.Requirement == req {
				return true
			}
		}
	}
	return false
}

func (p *Page) affectedWorktrees(paths []string) bool {
	if p.picked() == wcID {
		return len(paths) > 0
	}
	for _, row := range p.rows {
		if slices.Contains(paths, row.Worktree) {
			return true
		}
	}
	req := p.shownReq()
	if req == "" {
		return false
	}
	for _, repo := range p.h.Info().Repos {
		for _, wt := range repo.Worktrees {
			if wt.Requirement == req && slices.Contains(paths, wt.Path) {
				return true
			}
		}
	}
	return false
}

func (p *Page) refreshStatus(c *ui.Context, w *words) {
	busy, failed := p.aggBusy || p.filesBusy, p.aggErr != "" || p.filesErr != ""
	if p.picked() == wcID {
		busy, failed = p.dirty.busy, p.dirty.err != ""
	}
	var label string
	color := theme.Text4
	switch {
	case p.pending:
		label, color = w.ReviewChanged, theme.Ticket
	case busy:
		label = w.ReviewUpdating
	case failed:
		label, color = w.ReviewFailed, theme.DangerTxt
	default:
		return
	}
	b := ui.ButtonBase(c.Key("review-refresh")).Gap(6).AlignItems(ui.Center).Shrink(0).
		Label(label).Tooltip(w.ReviewHint)
	b.Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Background(color).Shrink(0)
		ui.Text(c, label).FontSize(theme.FSSmall).TextColor(color).SingleLine()
	})
	if p.pending || failed {
		b.Cursor(ui.CursorPointer)
		if b.Clicked() {
			p.reload()
		}
	}
}

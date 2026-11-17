package track

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

type sheetAction int

const (
	actionMR sheetAction = iota
	actionSync
)

// sheet is the confirmation of a batch action, listing what each ticked row would do: the commands run
// only after it, and only for the rows still ticked in it.
type sheet struct {
	action sheetAction
	open   bool
	rows   []sheetRow
	gh     bool
	glab   bool
	busy   bool
	in     session.TagAggInput // the filter the sheet was opened for, not whatever shows when it is confirmed
}

type sheetRow struct {
	key, repo, branch string
	pick              session.BatchPick
	how               func(*words) string // the whole sentence, translated in the current frame
	brief             func(*words) string // the same in a few words; nil: how
	route             string              // the branch and where it goes; "": branch
	skip              bool
	off               bool // unticked in the sheet
	tag               string
}

// planning: the plan is being made; the sheet opens once it is ready, so it never changes height in front of the user.
func (sh *sheet) planning() bool { return sh != nil && sh.busy && !sh.open && len(sh.rows) == 0 }

func (sh *sheet) count() (run, skipped int) {
	for _, r := range sh.rows {
		if r.skip {
			skipped++
		} else if !r.off {
			run++
		}
	}
	return run, skipped
}

// startMR makes the plan of 创建 MR for the ticked rows and opens the sheet.
func (p *Page) startMR() {
	if p.input == nil {
		return
	}
	in, keys := *p.input, p.ticked()
	s, ctx := p.s, p.h.Caller()
	p.sheet = &sheet{action: actionMR, busy: true, in: in}
	sh := p.sheet
	go func() {
		plan, err := s.MRPlan(ctx, session.MRInput{Agg: in, Rows: keys})
		p.h.Update(func() {
			if p.sheet != sh {
				return
			}
			if err != nil {
				p.sheet = nil
				p.h.Toasts().Error(fill(p.w.MrFailedToast, "reason", err.Error()))
				return
			}
			for _, r := range plan.Rows {
				sh.gh = sh.gh || r.Tool == "gh"
				sh.glab = sh.glab || r.Tool == "glab"
				sh.rows = append(sh.rows, mrSheetRow(r))
			}
			sh.busy, sh.open = false, true // opened only now, at the height its rows give it
		})
	}()
}

func (p *Page) startSync() {
	if p.input == nil {
		return
	}
	repos := checkedRepos(p.rows, p.checked)
	if len(repos) == 0 {
		return
	}
	favs := p.s.Favs()
	if len(favs) == 0 {
		p.h.Toasts().Error(p.w.SyncNoFavs)
		return
	}
	sh := &sheet{action: actionSync, busy: true, in: *p.input}
	p.sheet = sh
	s, ctx := p.s, p.h.Caller()
	go func() {
		plan, err := s.BranchesPlan(ctx, session.BatchPlanInput{Repos: repos, Branches: favs})
		p.h.Update(func() {
			if p.sheet != sh {
				return
			}
			if err != nil {
				p.sheet = nil
				p.h.Toasts().Error(fill(p.w.SyncFailedToast, "reason", err.Error()))
				return
			}
			if len(plan.Rows) == 0 {
				p.sheet = nil
				p.h.Toasts().Error(p.w.SyncNoFavs)
				return
			}
			sh.rows = syncSheetRows(plan)
			sh.busy, sh.open = false, true
		})
	}()
}

// mrSheetRow is a row of the MR sheet: the branch and its target, what happens in a few words, the whole sentence as
// its tooltip.
func mrSheetRow(r session.MRPlanRow) sheetRow {
	row := sheetRow{key: r.Key, repo: r.Repo, branch: r.Branch, route: mrRoute(r), skip: r.Skip != "" || r.Missing,
		how: func(w *words) string { return mrHow(w, r) }, brief: func(w *words) string { return mrBrief(w, r) }}
	if r.Push && r.Skip == "" {
		row.tag = "push"
	}
	return row
}

func syncSheetRows(plan session.BatchPlan) []sheetRow {
	rows := make([]sheetRow, 0, len(plan.Rows))
	for _, r := range plan.Rows {
		rows = append(rows, sheetRow{
			key: r.Repo + "\n" + r.Branch, repo: r.Repo, branch: r.Branch,
			pick: session.BatchPick{Repo: r.Repo, Branch: r.Branch},
			how:  func(w *words) string { return syncHow(w, r) }, skip: r.How == "skip",
			brief: func(w *words) string { return syncBrief(w, r) }, route: syncRoute(r),
		})
	}
	return rows
}

func (sh *sheet) syncPicks() []session.BatchPick {
	var picks []session.BatchPick
	for _, r := range sh.rows {
		// Rows skipped in the plan were never confirmed, even if they can run now.
		if !r.off && !r.skip {
			picks = append(picks, r.pick)
		}
	}
	return picks
}

// runSheet runs the action of the sheet on the rows it still has ticked.
func (p *Page) runSheet() {
	sh := p.sheet
	if sh == nil || sh.busy {
		return
	}
	var keys []string
	for _, r := range sh.rows {
		if !r.skip && !r.off {
			keys = append(keys, r.key)
		}
	}
	if len(keys) == 0 {
		return
	}
	sh.busy, sh.open = true, false // it runs as a task: the corner of the status bar follows it
	// The rows keep their order while the run changes the counts the server sorts by.
	p.frozen = make([]string, len(p.rows))
	for i, r := range p.rows {
		p.frozen[i] = r.Key
	}
	in, fk, s, ctx, subject := sh.in, p.filterKey(), p.s, p.h.Context(), p.subject()
	switch sh.action {
	case actionSync:
		picks := sh.syncPicks()
		go func() {
			run, err := s.BranchesRun(ctx, session.BatchRunInput{Picks: picks, Subject: subject})
			p.h.Update(func() {
				if p.sheet != sh {
					return
				}
				p.sheet = nil
				if err != nil {
					p.h.Toasts().TaskError(fill(p.w.SyncFailedToast, "reason", err.Error()))
					return
				}
				p.syncRun, p.syncFor = &run, fk
				p.apply(true)
			})
		}()
		return
	}
	go func() {
		run, err := s.CreateMR(ctx, session.MRInput{Agg: in, Rows: keys, Subject: subject})
		p.h.Update(func() {
			if p.sheet == sh {
				p.sheet = nil
			}
			if err != nil {
				p.h.Toasts().TaskError(fill(p.w.MrFailedToast, "reason", err.Error()))
				return
			}
			p.mrRun, p.mrFor = &run, fk
			p.rebuildRows()
			p.apply(true)
		})
	}()
}

func (p *Page) mrResults() map[string]session.MRRow {
	if p.mrRun == nil || len(p.mrRun.Rows) == 0 || p.input == nil || p.mrFor != p.filterKey() {
		return nil
	}
	out := make(map[string]session.MRRow, len(p.mrRun.Rows))
	for _, r := range p.mrRun.Rows {
		out[r.Key] = r
	}
	return out
}

func (p *Page) showSync() bool {
	return p.syncRun != nil && len(p.syncRun.Rows) > 0 && p.input != nil && p.syncFor == p.filterKey()
}

func planCount(text string, n int) string {
	one, many, plural := strings.Cut(text, "|")
	if plural && n != 1 {
		one = many
	}
	return fill(one, "n", n)
}

func (p *Page) sheetPopover(c *ui.Context) {
	sh := p.sheet
	if sh == nil || !sh.open {
		return
	}
	w := tr.Of(c)
	run, skipped := sh.count()
	title, promise, goLabel := planCount(w.MrTitle, run), w.MrPromise, planCount(w.MrGo, run)
	if sh.action == actionMR && sh.gh {
		kind := "PR"
		if sh.glab {
			kind = "MR/PR"
		}
		title = fill(planCount(w.MrBatchTitle, run), "kind", kind)
		goLabel = fill(w.MrBatchGo, "n", run, "kind", kind)
	}
	switch sh.action {
	case actionSync:
		title, promise, goLabel = planCount(w.SyncTitle, run), w.SyncPromise, planCount(w.SyncGo, run)
	}
	open := sh.open
	widgets.PlanSheet(c, &open, widgets.Plan{
		Title: title, Promise: promise,
		Rows: func() {
			for _, skip := range []bool{false, true} {
				if skip && skipped > 0 {
					widgets.PlanSkipped(c, fill(w.PlanSkipped, "n", skipped))
				}
				for i := range sh.rows {
					r := &sh.rows[i]
					if r.skip != skip {
						continue
					}
					var pick func()
					if !skip {
						pick = func() {
							on := !r.off
							label := fill(w.PlanPick, "repo", r.repo, "branch", r.branch)
							if widgets.Disable(ui.Checkbox(c, &on, "").Label(label), sh.busy).Changed() {
								r.off = !on
							}
						}
					}
					how, brief, route := r.how(w), r.how(w), cmpOr(r.route, r.branch)
					if r.brief != nil {
						brief = r.brief(w)
					}
					widgets.PlanRow(c.Key(r.key), widgets.PlanEntry{Repo: r.repo, Branch: route, Action: brief, Detail: how,
						Skipped: skip, Lead: r.tag == "push", Leading: pick})
				}
			}
		},
		Actions: func() {
			if sh.busy && len(sh.rows) > 0 {
				ui.Spinner(c).Size(theme.IconS, theme.IconS)
				ui.Text(c, fill(w.PlanRunning, "n", run)).FontSize(theme.FSSmall).TextColor(theme.Text3)
				return
			}
			if widgets.Button(c, i18n.Of(c).Cancel).Clicked() {
				open = false
			}
			if run > 0 && !sh.busy {
				if widgets.PrimaryButton(c, goLabel).Clicked() {
					open = false
					p.runSheet()
				}
			}
		},
	})
	if !open && p.sheet == sh && !sh.busy {
		p.sheet = nil
	}
}

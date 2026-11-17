package track

import (
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

func (p *Page) diffPending() bool {
	return p.filesBusy && (p.filesIn.DiffMode != p.diffMode ||
		p.diffMode == session.DiffStage && p.filesIn.DiffStage != p.diffStage)
}

// diffControls is the 查看改动 picker: 整体改动, 未提交, then one 未合并 entry per merge flow step, named by the flow
// so a workspace with more stages lists them all; 整体改动 also has its base chip.
func (p *Page) diffControls(c *ui.Context, w *words, showBase bool) {
	label := w.DiffBase
	switch {
	case p.diffMode == session.DiffWorktree:
		label = w.DiffWorktree
	case p.diffMode == session.DiffStage && p.diffStage < len(p.flow):
		label = stageLabel(w, p.flow[p.diffStage])
	}
	p.diffPicker(c, "diffmode", w.DiffLabel, label, w.DiffHint, func(c *ui.Context) {
		item := func(key, name, note, tip string, on, empty bool, pick func()) {
			row := widgets.RowLook(ui.ButtonBase(c.Key(key)).Label(w.DiffLabel+" · "+name).Justify(ui.Start).Height(theme.HMenuItem).PaddingX(12).Radius(theme.RRow), on)
			widgets.Disable(row, empty && !on)
			if tip != "" {
				row.Tooltip(tip)
			}
			row.Children(func() {
				ui.Text(c, name).SingleLine()
				ui.Spacer(c)
				if empty && !on {
					ui.Text(c, w.DiffNone).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine()
				} else if note != "" {
					ui.Text(c, note).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine()
				}
			})
			if row.Clicked() {
				p.pop = popup{}
				pick()
				p.apply(false)
			}
		}
		item(session.DiffBase, w.DiffBase, "", "", p.diffMode == session.DiffBase, p.diffEmpty(session.DiffBase, 0), func() { p.diffMode = session.DiffBase })
		item(session.DiffWorktree, w.DiffWorktree, "", "", p.diffMode == session.DiffWorktree, p.diffEmpty(session.DiffWorktree, 0), func() { p.diffMode = session.DiffWorktree })
		for index, step := range p.flow {
			note := valueOr(step.Branch, w.CompareBase)
			if note == step.Name {
				note = ""
			}
			item("stage:"+step.Name, stageLabel(w, step), note, w.DiffStageHint, p.diffMode == session.DiffStage && p.diffStage == index,
				p.diffEmpty(session.DiffStage, index), func() { p.diffMode, p.diffStage = session.DiffStage, index })
		}
	})
	if p.diffMode == session.DiffBase && showBase {
		p.targetChip(c, w, "master", w.Master, valueOr(p.master, w.CompareBase), w.MasterHint)
	}
}

// stageLabel names the changes a requirement has not yet brought to step, by the step's configured name.
func stageLabel(w *words, step session.MergeStep) string {
	return fill(w.StageAwaiting, "branch", step.Name)
}

func (p *Page) diffPicker(c *ui.Context, kind, label, value, hint string, body func(*ui.Context)) {
	button := optButton(c, kind, label, value, hint)
	open := p.pop.kind == kind
	if button.Clicked() {
		open = !open
		p.pop = popup{}
		if open {
			p.pop.kind = kind
		}
	}
	if open {
		ui.PopoverBase(c, button, &open, func(panel ui.Element) {
			widgets.Panel(panel).Margin(4, 0, 0, 0).Width(310).Children(func() { body(c) })
		})
		if !open && p.pop.kind == kind {
			p.pop = popup{}
		}
	}
}

package widgets

import (
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

// Plan supplies translated content and controls for a batch confirmation or its results.
type Plan struct {
	Title, Promise string
	Rows           func()
	Options        func()
	Actions        func()
}

func PlanSheet(c *ui.Context, open *bool, p Plan) ui.Element {
	_, height := c.Size()
	return Dialog(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(theme.Scrim)
		panelLook(panel).Width(640).MaxHeight(max(0, height-40)).Padding(20).Gap(16).Radius(theme.RDialog).
			Border(0, theme.MenuRing).Label(p.Title).Description(p.Promise)
		ui.Column(c).Gap(4).Shrink(0).Children(func() {
			ui.Text(c, p.Title).FontSize(theme.FSTitle).FontWeight(600).SingleLine().Shrink(1).MinWidth(0).Tooltip(p.Title)
			if p.Promise != "" {
				ui.Text(c, p.Promise).FontSize(theme.FSSmall).TextColor(theme.Text2).Wrap()
			}
		})
		ui.Scroll(c.Key("rows")).MinHeight(theme.HMenuItem + 4).Shrink(1).MinWidth(0).Children(p.Rows)
		ui.Row(c).Padding(8, 0, 0, 0).Gap(12).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Wrap().Children(p.Options)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(p.Actions)
		})
	})
}

// PlanEntry is one row of a batch confirmation, on one line in columns that line up from row to row: the repo, the
// branch it acts on (Branch, which may show where it goes: "feat → main"), and on the right a few words of what happens
// (Action). Detail, the whole sentence, is the tooltip. A skipped row is dimmed and has no Leading control.
type PlanEntry struct {
	Repo, Branch, Action, Detail string
	Skipped, Lead                bool // Lead: Action is the row's main outcome, drawn brighter
	Leading                      func()
}

// planRowH is the height of a plan row: every row is one line, so the columns read down the sheet.
const planRowH = theme.HMenuItem + 6

func PlanRow(c *ui.Context, r PlanEntry) ui.Element {
	name, branch, action := theme.TextStrong, theme.Text3, theme.Text3
	switch {
	case r.Skipped:
		name, branch, action = theme.Text4, theme.Text4, theme.Text4
	case r.Lead:
		action = theme.Text
	}
	detail := r.Detail
	if detail == "" {
		detail = r.Action
	}
	return ui.Row(c).Height(planRowH).Shrink(0).Gap(10).AlignItems(ui.Center).Tooltip(detail).
		Border(1, theme.RowRule).BorderWidth(0, 0, 1, 0).Children(func() {
		ui.Box(c).Width(18).Shrink(0).Center().Children(r.Leading)
		ui.Text(c, r.Repo).Width(150).Shrink(0).FontWeight(600).TextColor(name).SingleLine().MinWidth(0).Tooltip(r.Repo)
		ui.Text(c, r.Branch).Grow(1).Shrink(1).MinWidth(60).Font(theme.MonoFont).FontSize(theme.FSLabel).TextColor(branch).SingleLine().Tooltip(r.Branch)
		ui.Text(c, r.Action).Shrink(2).MinWidth(0).MaxWidth(240).FontSize(theme.FSSmall).TextColor(action).SingleLine().Selectable()
	})
}

func PlanSkipped(c *ui.Context, heading string) {
	ui.Text(c, heading).FontSize(theme.FSLabel).FontWeight(600).TextColor(theme.Text4).SingleLine().Margin(14, 0, 2, 0).Shrink(0)
}

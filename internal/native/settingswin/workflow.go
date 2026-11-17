package settingswin

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

func (w *Window) mergeFlow(c *ui.Context, text *words) {
	from := fmt.Sprint(w.info.MergeFlow)
	if w.flowFrom != from {
		w.flow, w.flowFrom = slices.Clone(w.info.MergeFlow), from
	}
	names := w.branchNames()
	var action func()
	hint := text.FlowHint
	if len(w.flow) > 0 {
		hint += fmt.Sprintf(text.FlowBaseline, w.flow[len(w.flow)-1].Name)
	}
	pageSection(c, text.FlowHead, hint, func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, text.FlowNameHead).Width(160).FontSize(theme.FSLabel).TextColor(theme.Text4)
			ui.Text(c, text.FlowBranchHead).Grow(1).FontSize(theme.FSLabel).TextColor(theme.Text4)
			ui.Box(c).Width(88)
		})
		for index := range w.flow {
			step := &w.flow[index]
			ui.Row(c.Key(fmt.Sprintf("flow %d", index))).MinHeight(42).Gap(8).AlignItems(ui.Center).Children(func() {
				nameLabel := fmt.Sprintf(text.FlowName, index+1)
				name := widgets.TextInput(c, &step.Name).Width(160).Shrink(0).FontSize(theme.FSSmall).Label(nameLabel).Placeholder(nameLabel)
				branchLabel := fmt.Sprintf(text.FlowBranch, index+1)
				branch := widgets.Caret(ui.Autocomplete(c, &step.Branch, names)).Grow(1).MinWidth(0).Height(theme.HSelect).
					Font(theme.MonoFont).FontSize(theme.FSSmall).Label(branchLabel).Placeholder(text.MasterPh)
				focused := ""
				if name.Focused() {
					focused = nameLabel
				}
				if branch.Focused() {
					focused = branchLabel
				}
				if name.Submitted() || branch.Submitted() || w.flowFocus != "" && (w.flowFocus == nameLabel || w.flowFocus == branchLabel) && focused != w.flowFocus ||
					branch.Changed() && slices.Contains(names, strings.TrimSpace(step.Branch)) {
					action = func() { w.commitMergeFlow(w.flow) }
				}
				if focused != "" {
					w.flowFocus = focused
				} else if w.flowFocus == nameLabel || w.flowFocus == branchLabel {
					w.flowFocus = ""
				}
				up := widgets.Disable(widgets.IconButton(c, theme.IconArrowUp, fmt.Sprintf(text.FlowUp, index+1)), index == 0)
				if up.Clicked() {
					next := slices.Clone(w.flow)
					next[index-1], next[index] = next[index], next[index-1]
					action = func() { w.commitMergeFlow(next) }
				}
				down := widgets.Disable(widgets.IconButton(c, theme.IconArrowDown, fmt.Sprintf(text.FlowDown, index+1)), index == len(w.flow)-1)
				if down.Clicked() {
					next := slices.Clone(w.flow)
					next[index], next[index+1] = next[index+1], next[index]
					action = func() { w.commitMergeFlow(next) }
				}
				remove := widgets.Disable(widgets.IconButton(c, theme.IconClose, fmt.Sprintf(text.FlowRemove, index+1)), len(w.flow) == 1)
				if remove.Clicked() {
					previous := slices.Clone(w.flow)
					next := slices.Delete(slices.Clone(w.flow), index, index+1)
					action = func() {
						w.commitMergeFlow(next)
						w.toasts.Undo(text.FlowRemoved, func() { w.commitMergeFlow(previous) }, "")
					}
				}
			})
		}
		if ui.ButtonBase(c).Height(theme.HSelect).PaddingX(0).TextColor(theme.Text).Label(text.FlowAdd).
			AlignSelf(ui.Start).Children(func() { ui.Text(c, text.FlowAdd).FontSize(theme.FSBody) }).Clicked() {
			action = func() { w.flow = append(w.flow, session.MergeStep{}) }
		}
	})
	if action != nil {
		action()
	}
}

func (w *Window) commitMergeFlow(flow []session.MergeStep) {
	next := slices.Clone(flow)
	w.flow = slices.Clone(next)
	w.mutate(func() error { return w.s.SetMergeFlow(next) })
}

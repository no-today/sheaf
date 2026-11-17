package widgets

import (
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	sheetEnter = 120 * time.Millisecond
	sheetExit  = 80 * time.Millisecond
)

// Dialog keeps the framework's dismissal and focus handling while the whole
// backdrop can leave together with its panel.
func Dialog(c *ui.Context, open *bool, build func(backdrop, panel ui.Element)) ui.Element {
	// Exit copies need their overlay parent to survive after the dialog closes.
	ui.Overlay(c, func() {})
	wasOpen := *open
	panel := ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		build(backdrop, panel)
		panel.Transition(FadeColors)
		started := ui.Local(backdrop, "sheet-enter", func() bool { return false })
		if !*started {
			backdrop.Jump("opacity", 0)
			panel.Jump("offset", 6)
			*started = true
		}
		backdrop.Opacity(backdrop.Animate("opacity", 1, sheetEnter)).Transition(ui.ElementTransition{
			Duration: sheetExit, Colors: true, Exit: &ui.Motion{},
		})
		panel.Transform(ui.Translation(0, panel.Animate("offset", 0, sheetEnter)))
	})
	if wasOpen && !*open {
		c.Invalidate()
	}
	return panel
}

func popoverMotion(panel ui.Element) ui.Element {
	const duration = 100 * time.Millisecond
	started := ui.Local(panel, "popover-enter", func() bool { return false })
	if !*started {
		panel.Jump("opacity", 0)
		panel.Jump("offset", -4)
		*started = true
	}
	return panel.Transition(FadeColors).Opacity(panel.Animate("opacity", 1, duration)).
		Transform(ui.Translation(0, panel.Animate("offset", 0, duration)))
}

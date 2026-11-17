package diffview

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"sheaf/internal/native/theme"
)

// Theme is how Preview Diff looks.
type Theme struct {
	Editor     *editor.Theme
	Font       ui.Font // the code's
	LineHeight float32 // a multiple of Font.Size, as editor.State's

	HeaderHeight float32
	HeaderFont   float32 // size of the header's text, as the change stream's file heads
	Background   ui.Color
	Border       ui.Color
	Text         ui.Color
	TextMuted    ui.Color
	TextDim      ui.Color
	Added        ui.Color
	Deleted      ui.Color
	Modified     ui.Color
	Renamed      ui.Color
	Part         ui.Color // the band of a part's title (Texts.Parts)
}

// DefaultTheme is the code as the workspace's change stream shows it, so a diff reads the same in every window: the body
// size on theme.HLine rows, the stream's change tints and faint line numbers.
func DefaultTheme() *Theme {
	editor := theme.Code()
	editor.Added, editor.Deleted = theme.StreamAddTint, theme.StreamDelTint
	editor.LineNumber = theme.Faint
	return &Theme{
		Editor:       editor,
		Font:         ui.Font{Family: theme.CodeFont(), Size: theme.FSBody},
		LineHeight:   float32(theme.HLine) / theme.FSBody, // line height as a multiple of the font size
		HeaderHeight: 32,
		HeaderFont:   theme.FSBody,
		Background:   theme.Island,
		Border:       theme.IslandRing,
		Text:         theme.Text,
		TextMuted:    theme.Text3,
		TextDim:      theme.Text4,
		Added:        theme.Add,
		Deleted:      theme.Del,
		Modified:     theme.Mod,
		Renamed:      theme.Remote,
		Part:         theme.Island,
	}
}

func dockTheme() *Theme {
	t := DefaultTheme()
	t.Background, t.Editor.Background, t.Part = theme.FrameBg, theme.FrameBg, theme.FrameBg
	return t
}

func (t *Theme) statusColor(status string) ui.Color {
	switch status {
	case "A":
		return t.Added
	case "D":
		return t.Deleted
	case "R", "C":
		return t.Renamed
	}
	return t.Modified
}

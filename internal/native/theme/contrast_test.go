package theme

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestContrast(t *testing.T) {
	for _, text := range []struct {
		name    string
		color   ui.Color
		minimum float64
	}{
		{"Text", Text, 7},
		{"Text2", Text2, 7},
		{"Text3", Text3, 5.5},
		{"Text4", Text4, 4.5},
		{"Text5", Text5, 3},
		{"Add", Add, 4.5},
		{"Del", Del, 4.5},
		{"Mod", Mod, 4.5},
		{"Ticket", Ticket, 4.5},
		{"AccentTxt", AccentTxt, 4.5},
	} {
		t.Run(text.name, func(t *testing.T) {
			for _, background := range []struct {
				name     string
				color    ui.Color
				selected bool
			}{
				{"Panel", Panel, false},
				{"Selection", Selection, true},
				{"SelDim", SelDim, true},
			} {
				t.Run(background.name, func(t *testing.T) {
					foreground := RowText(background.selected, text.color)
					got := contrast(foreground, background.color)
					t.Logf("#%02x%02x%02x on #%02x%02x%02x: %.4f:1", foreground.R, foreground.G, foreground.B, background.color.R, background.color.G, background.color.B, got)
					if got < text.minimum {
						t.Errorf("contrast %.4f:1, want at least %.1f:1", got, text.minimum)
					}
				})
			}
		})
	}
}

// TestSyntaxContrast: every code colour stays readable on the diff's plain, added and deleted lines.
func TestSyntaxContrast(t *testing.T) {
	for kind, color := range Syntax {
		for _, bg := range []struct {
			name  string
			color ui.Color
		}{{"Island", Island}, {"AddBg", AddBg}, {"DelBg", DelBg}} {
			if got := contrast(color, bg.color); got < 4.5 {
				t.Errorf("token %d on %s: contrast %.2f:1, want at least 4.5:1", kind, bg.name, got)
			}
		}
	}
}

func contrast(foreground, background ui.Color) float64 {
	luminance := func(color ui.Color) float64 {
		linear := func(channel uint8) float64 {
			value := float64(channel) / 255
			if value <= 0.04045 {
				return value / 12.92
			}
			return math.Pow((value+0.055)/1.055, 2.4)
		}
		return 0.2126*linear(color.R) + 0.7152*linear(color.G) + 0.0722*linear(color.B)
	}
	a, b := luminance(foreground.Over(background)), luminance(background)
	return (max(a, b) + 0.05) / (min(a, b) + 0.05)
}

// The file tree keeps a file's status colour on the selected row.
func TestStatusContrastOnSelection(t *testing.T) {
	for _, status := range []string{"A", "D", "M", "R", "?"} {
		for _, background := range []ui.Color{Panel, Rail, Selection, SelDim} {
			if got := contrast(Status(status), background); got < 4.5 {
				t.Errorf("status %s on #%02x%02x%02x: %.2f:1, want 4.5", status, background.R, background.G, background.B, got)
			}
		}
	}
}

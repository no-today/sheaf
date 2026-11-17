package widgets

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
)

func TestDialogMotion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reduced bool
		close   string
	}{
		{name: "escape", close: "escape"},
		{name: "backdrop", close: "backdrop"},
		{name: "cancel", close: "cancel"},
		{name: "reduced motion", reduced: true, close: "escape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			open, frames := false, 0
			tt := ui.NewTester(func(c *ui.Context) {
				frames++
				c.SetTheme(theme.Theme())
				ui.Column(c).Fill().Background(theme.Island).Padding(16).AlignItems(ui.Start).Children(func() {
					if Button(c, "Open").Clicked() {
						open = true
					}
				})
				Dialog(c, &open, func(backdrop, panel ui.Element) {
					backdrop.Background(theme.Scrim)
					panelLook(panel).Size(320, 180).Padding(20).Gap(12).Label("sheet")
					Button(c, "Accept")
					if Button(c, "Cancel").Clicked() {
						open = false
					}
				})
			}, 600, 400)
			tt.SetPreferences(ui.Preferences{ReduceMotion: tc.reduced})
			tt.Key(0, ui.KeyTab)
			if !tt.Focused("Open") {
				t.Fatal("trigger did not take keyboard focus")
			}
			if err := tt.Click("Open"); err != nil {
				t.Fatal(err)
			}
			if !tt.Focused("Accept") {
				t.Fatal("sheet did not take keyboard focus")
			}
			start := planRect(t, tt, "sheet")
			tt.Advance(60 * time.Millisecond)
			tt.Frame()
			middle := planRect(t, tt, "sheet")
			tt.Advance(60 * time.Millisecond)
			tt.Frame()
			end := planRect(t, tt, "sheet")
			if tc.reduced {
				if start != end || middle != end {
					t.Fatalf("reduced motion moved the sheet: %+v, %+v, %+v", start, middle, end)
				}
			} else {
				if math.Abs(float64(start.Y-end.Y)-6) > 0.01 {
					t.Fatalf("sheet did not enter from Y+6: start %+v, end %+v", start, end)
				}
				if math.Abs(float64(middle.Y-end.Y)-0.75) > 0.02 {
					t.Fatalf("sheet did not ease out over 120 ms: middle %+v, end %+v", middle, end)
				}
			}
			if !imageHasColor(tt, theme.Panel) {
				t.Fatal("settled sheet is not opaque")
			}
			before := frames
			tt.Advance(time.Second)
			if frames != before || tt.FramePending() {
				t.Fatal("open sheet keeps drawing at rest")
			}
			switch tc.close {
			case "escape":
				tt.Key(0, ui.KeyEscape)
			case "backdrop":
				tt.ClickAt(580, 380)
			case "cancel":
				if err := tt.Click("Cancel"); err != nil {
					t.Fatal(err)
				}
			}
			if open {
				t.Fatal("sheet stayed open")
			}
			tt.Advance(40 * time.Millisecond)
			if !imageHasColor(tt, theme.Panel) && !tc.reduced {
				// Its ghost still dims the surface halfway through the exit.
				p := tt.Image().RGBAAt(300, 200)
				if p.R == theme.Island.R && p.G == theme.Island.G && p.B == theme.Island.B {
					t.Fatal("sheet vanished before its 80 ms exit")
				}
			}
			tt.Advance(40 * time.Millisecond)
			tt.Frame()
			p := tt.Image().RGBAAt(300, 200)
			if p.R != theme.Island.R || p.G != theme.Island.G || p.B != theme.Island.B {
				t.Fatalf("sheet ghost remains after 80 ms: %v", p)
			}
			if !tt.Focused("Open") {
				t.Fatal("closing the sheet did not return focus")
			}
			tt.Advance(60 * time.Millisecond)
			before = frames
			tt.Advance(time.Second)
			if frames != before || tt.FramePending() {
				t.Fatal("closed sheet keeps drawing")
			}
			if err := tt.Click("Open"); err != nil {
				t.Fatal(err)
			}
			if again := planRect(t, tt, "sheet"); again != start {
				t.Fatalf("reopening lost its enter motion: %+v, want %+v", again, start)
			}
		})
	}
}

func TestPopoverMotion(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reduced"}[reduced], func(t *testing.T) {
			open, frames, height := false, 0, float32(100)
			tt := ui.NewTester(func(c *ui.Context) {
				frames++
				c.SetTheme(theme.Theme())
				ui.Column(c).Fill().Background(theme.Island).Padding(40).AlignItems(ui.Start).Children(func() {
					b := Button(c, "Open")
					if b.Clicked() {
						open = true
					}
					ui.PopoverBase(c, b, &open, func(panel ui.Element) {
						Panel(panel).Size(220, height).Label("popup")
					})
				})
			}, 600, 400)
			tt.SetPreferences(ui.Preferences{ReduceMotion: reduced})
			if err := tt.Click("Open"); err != nil {
				t.Fatal(err)
			}
			start := planRect(t, tt, "popup")
			tt.Advance(50 * time.Millisecond)
			tt.Frame()
			middle := planRect(t, tt, "popup")
			tt.Advance(50 * time.Millisecond)
			tt.Frame()
			end := planRect(t, tt, "popup")
			if reduced {
				if start != end || middle != end {
					t.Fatalf("reduced motion moved the popover: %+v, %+v, %+v", start, middle, end)
				}
			} else if math.Abs(float64(start.Y-end.Y)+4) > 0.01 || math.Abs(float64(middle.Y-end.Y)+0.5) > 0.02 {
				t.Fatalf("popover did not ease from Y-4 in 100 ms: %+v, %+v, %+v", start, middle, end)
			}
			before := frames
			tt.Advance(time.Second)
			if frames != before || tt.FramePending() {
				t.Fatal("settled popover keeps drawing")
			}
			height = 140
			tt.Frame()
			if r := planRect(t, tt, "popup"); r.H != height || r.Y != end.Y || tt.FramePending() {
				t.Fatalf("data change animated the open popover: %+v", r)
			}
		})
	}
}

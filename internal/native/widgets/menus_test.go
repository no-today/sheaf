package widgets

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
)

func TestOverlayShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, width := range []int{1180, 1400} {
			t.Run(fmt.Sprintf("%s/%d", locale, width), func(t *testing.T) {
				w := i18n.Common.For(locale)
				value, prompt := "feature/workspace", false
				var toasts Toaster
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(theme.Theme())
					ui.Column(c).Fill().Background(theme.Island).Padding(32).Gap(20).AlignItems(ui.Start).Children(func() {
						ui.Text(c, w.Log).FontSize(theme.FSTitle).FontWeight(600)
						MenuButton(c, Button(c, w.Track), func(m *ui.Menu) {
							m.Item(w.AllRepos).Checked(true)
							m.Item(w.Diff).Shortcut(ui.Cmd, ui.KeyD)
							m.Separator()
							m.Item(w.Close).Danger()
						})
						b := Button(c, w.OK)
						if b.Clicked() {
							prompt = true
						}
						PromptPopover(c, b, &prompt, &value, Prompt{Title: w.Track, Placeholder: w.AllRepos, OK: w.OK, Cancel: w.Cancel})
						ProgressStrip(c, []ui.Color{theme.Add, theme.Add, theme.Ticket, theme.Mod, theme.Del})
					})
					toasts.View(c)
				}, width, 760)
				tt.SetLocale(locale)
				tt.SetScale(2)
				shot := func(name string) {
					tt.Advance(200 * time.Millisecond)
					widgetShot(t, tt, fmt.Sprintf("%s-%s-%d", name, locale, width))
				}
				shot("overlay-controls")
				if err := tt.Click(w.Track); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyDown)
				shot("overlay-menu")
				tt.Key(0, ui.KeyEscape)
				if err := tt.Click(w.OK); err != nil {
					t.Fatal(err)
				}
				shot("overlay-prompt")
				tt.Key(0, ui.KeyEscape)
				toasts.Undo(w.AllRepos+": feature/workspace", func() {}, "")
				tt.Frame()
				shot("overlay-toast-undo")
				toasts.Error(w.AllRepos + ": git fetch origin — feature/workspace")
				tt.Frame()
				shot("overlay-toast-error")
			})
		}
	}
}

func TestMenusKeepCallerTheme(t *testing.T) {
	for _, attach := range []struct {
		name string
		menu func(*ui.Context, ui.Element, func(*ui.Menu)) ui.Element
	}{{"context", ContextMenu}, {"button", MenuButton}} {
		t.Run(attach.name, func(t *testing.T) {
			original := theme.DimListTheme()
			ui.NewTester(func(c *ui.Context) {
				c.SetTheme(original)
				attach.menu(c, Button(c, "Menu"), func(m *ui.Menu) { m.Item("Open") })
				if c.Theme() != original {
					t.Fatal("attaching a menu replaced the caller's theme")
				}
			}, 400, 300)
		})
	}
}

func TestMenusKeepKeyboardSubmenusAndCheckedItems(t *testing.T) {
	for _, context := range []bool{false, true} {
		t.Run(fmt.Sprint(context), func(t *testing.T) {
			chosen := 0
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme.Theme())
				b := Button(c, "Menu")
				build := func(m *ui.Menu) {
					m.Item("Unavailable").Disabled(true)
					m.Item("Checked").Checked(true)
					m.Submenu("Actions", func(m *ui.Menu) {
						if m.Item("Run").Shortcut(ui.Cmd, ui.KeyR).Chosen() {
							chosen++
						}
					})
				}
				if context {
					ContextMenu(c, b, build)
				} else {
					MenuButton(c, b, build)
				}
			}, 600, 400)
			tt.Key(0, ui.KeyTab)
			if context {
				tt.Key(ui.Shift, ui.KeyF10)
			} else {
				tt.Key(0, ui.KeyDown)
			}
			if a, ok := tt.Access("Checked"); !ok || !a.Checked {
				t.Fatal("menu lost its checked state or keyboard trigger")
			}
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyRight)
			if !tt.HasText("Run") {
				t.Fatal("keyboard did not open the submenu")
			}
			tt.Key(0, ui.KeyEnter)
			if chosen != 1 || tt.HasText("Actions") || !tt.Focused("Menu") {
				t.Fatalf("choice or returned focus lost: chosen %d", chosen)
			}
		})
	}
}

package widgets

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
)

func TestSwitchKeepsKeyboardAndAccessibilityContract(t *testing.T) {
	on, changes := false, 0
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme.Theme())
		if Switch(c, &on).Label("Track repository").Changed() {
			changes++
		}
	}, 160, 80)
	if err := tt.Click("Track repository"); err != nil {
		t.Fatal(err)
	}
	if !on || changes != 1 {
		t.Fatalf("click: on=%v changes=%d", on, changes)
	}
	if !rectHasColor(tt, planRect(t, tt, "Track repository"), theme.OnAccent) {
		t.Fatal("enabled switch thumb has no contrast against its track")
	}
	tt.Key(0, ui.KeySpace)
	if on || changes != 2 {
		t.Fatalf("keyboard: on=%v changes=%d", on, changes)
	}
	state, ok := tt.Access("Track repository")
	if !ok || state.Role != ui.RoleSwitch {
		t.Fatalf("switch semantics missing: %+v", state)
	}
}

func TestFieldsKeepInputContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		build  func(*ui.Context, *string) ui.Element
		search bool
	}{
		{name: "text", build: TextInput},
		{name: "search", build: SearchField, search: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, invalid, changes, submissions := "", false, 0, 0
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme.Theme())
				ui.Column(c).Fill().Padding(16).AlignItems(ui.Start).Children(func() {
					field := tc.build(c, &value).Width(220).Label("Query").Placeholder("Search repositories")
					if c.Shortcut(ui.Cmd, ui.KeyF) {
						field.Focus()
					}
					if invalid {
						field.Error("Invalid query")
					}
					if field.Changed() {
						changes++
					}
					if field.Submitted() {
						submissions++
					}
				})
			}, 320, 160)
			if r := planRect(t, tt, "Query"); r.W != 220 {
				t.Fatalf("field width = %v, want 220", r.W)
			}
			tt.Key(ui.Cmd, ui.KeyF)
			if !tt.Focused("Query") {
				t.Fatal("shortcut did not focus the named input")
			}
			tt.Type("lobe")
			if value != "lobe" || changes == 0 {
				t.Fatalf("value = %q, changes = %d", value, changes)
			}
			tt.Key(0, ui.KeyEnter)
			if submissions != 1 {
				t.Fatalf("submissions = %d, want one", submissions)
			}
			if !tc.search {
				invalid = true
				tt.Frame()
				if !rectHasColor(tt, planRect(t, tt, "Query"), theme.Del) {
					t.Fatal("validation lost its error border")
				}
			}
			if tc.search {
				tt.Key(0, ui.KeyEscape)
				if value != "" || !tt.Focused("Query") {
					t.Fatal("Escape did not clear the search and keep its focus")
				}
			}
		})
	}
}

func TestSelectKeepsGenericKeyboardChoice(t *testing.T) {
	type option struct {
		id   int
		name string
	}
	options := []option{{1, "First"}, {2, "Second"}, {3, "Third"}}
	choice, changes := options[0], 0
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme.Theme())
		if Select(c, &choice, options, func(o option) string { return o.name }, "Choice").Changed() {
			changes++
		}
	}, 320, 200)
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("Choice") {
		t.Fatal("Tab did not focus the select")
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	if choice != options[2] || changes != 1 {
		t.Fatalf("choice = %+v, changes = %d", choice, changes)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyEscape)
	if choice != options[2] || changes != 1 || tt.HasText("First") {
		t.Fatal("Escape did not close the popup without changing its choice")
	}
}

func TestSegmentedKeepsArrowSelection(t *testing.T) {
	selected, changes := 0, 0
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme.Theme())
		if Segmented(c, &selected, "Layout", "Split", "Unified").Changed() {
			changes++
		}
	}, 320, 100)
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeyRight)
	if selected != 1 || changes != 1 || !tt.Focused("Unified") {
		t.Fatalf("selected = %d, changes = %d", selected, changes)
	}
	tt.Key(0, ui.KeyLeft)
	if selected != 0 || changes != 2 || !tt.Focused("Split") {
		t.Fatalf("selected = %d, changes = %d", selected, changes)
	}
}

func TestControlsShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, width := range []int{1400, 1180} {
			t.Run(fmt.Sprintf("%s/%d", locale, width), func(t *testing.T) {
				w := i18n.Common.For(locale)
				text, query, choice, segment := "", "", 0, 0
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(theme.Theme())
					ui.Row(c).Fill().Background(theme.FrameBg).Padding(16).Gap(20).Children(func() {
						ui.Column(c).Width(260).Shrink(0).Padding(8).Gap(16).AlignItems(ui.Stretch).Children(func() {
							ui.Text(c, w.Settings).FontWeight(600)
							TextInput(c.Key("name"), &text).Label(w.Welcome).Placeholder(w.Welcome)
							SearchField(c.Key("search"), &query).Label(w.AllRepos).Placeholder(w.AllRepos)
							Select(c, &choice, []int{0, 1}, func(i int) string { return []string{w.Log, w.Track}[i] }, w.Scope)
							Segmented(c, &segment, w.Settings, w.Log, w.Track)
							ui.Row(c).Gap(8).Children(func() {
								IconButton(c, theme.IconRefresh, w.Opening)
								Disable(IconButton(c, theme.IconClose, w.Close), true)
							})
						})
						ui.Column(c).Grow(1).MinWidth(0).Background(theme.Island).Radius(theme.RIsland).Padding(24).Gap(24).Children(func() {
							ui.Row(c).Gap(8).Children(func() {
								ui.Text(c, w.Log).FontSize(theme.FSTitle).FontWeight(600)
								Badge(c, "128")
								Badge(c, w.AllRepos)
							})
							ui.Row(c).Gap(8).Children(func() {
								RefTag(c, "feature/lobe-controls", "local")
								RefTag(c, "origin/feature/lobe-controls", "remote")
								RefTag(c, "v1.2.3", "tag")
							})
							ui.Row(c).Width(360).Gap(8).Children(func() {
								RefTag(c, "feature/a-long-branch-name-that-must-yield-to-the-subject", "local")
								ui.Text(c, w.AllRepos).SingleLine().Shrink(0)
							})
							ui.Row(c).Grow(1).Gap(16).Children(func() {
								for i, icon := range []*ui.SVG{theme.IconFolder, theme.IconDiff, theme.IconFiles} {
									EmptyState(c.Key(i), icon, []string{w.Log, w.Diff, w.History}[i], w.AllRepos, func() {
										Button(c, w.OK)
									}).Basis(0).MinWidth(0)
								}
							})
						})
					})
				}, width, 480)
				tt.SetLocale(locale)
				tt.SetScale(2)
				shot := func(name string) {
					widgetShot(t, tt, fmt.Sprintf("controls-%s-%s-%d", name, locale, width))
				}
				shot("ready")
				r := planRect(t, tt, w.Opening)
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				shot("hover")
				if err := tt.Click(w.Welcome); err != nil {
					t.Fatal(err)
				}
				shot("input-focus")
				if err := tt.Click(w.AllRepos); err != nil {
					t.Fatal(err)
				}
				tt.Type("lobe-controls")
				shot("search-focus")
				tt.Key(0, ui.KeyEscape)
				if query != "" {
					t.Fatal("Escape did not clear the search")
				}
				if err := tt.Click(w.Scope); err != nil {
					t.Fatal(err)
				}
				tt.Advance(100 * time.Millisecond)
				shot("select")
				tt.Key(0, ui.KeyEnd)
				tt.Key(0, ui.KeyEnter)
				if choice != 1 {
					t.Fatal("select popup did not accept the keyboard choice")
				}
			})
		}
	}
}

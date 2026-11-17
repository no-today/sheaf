package widgets

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
)

func TestShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, width := range []int{1400, 1180} {
			t.Run(fmt.Sprintf("%s/%d", locale, width), func(t *testing.T) {
				w := i18n.Common.For(locale)
				open, confirm, busy := false, false, false
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(theme.Theme())
					ui.Column(c).Fill().Background(theme.Island).Padding(32).Gap(20).AlignItems(ui.Start).Children(func() {
						ui.Text(c, w.Log).FontSize(theme.FSTitle).FontWeight(600)
						if Button(c, w.Track).Clicked() {
							open = true
						}
						b := Button(c, w.Close)
						if b.Clicked() {
							confirm = true
						}
						ConfirmPopover(c, b, &confirm, Confirm{Title: w.Close, Detail: w.AllRepos, OK: w.OK, Cancel: w.Cancel})
						DelayedSpinner(c, "shot-load", busy)
					})
					PlanSheet(c, &open, Plan{
						Title: w.Track, Promise: w.AllRepos,
						Rows: func() {
							for i := range 3 {
								PlanRow(c.Key(i), PlanEntry{Repo: fmt.Sprintf("repo-%d", i+1), Branch: "feature/workspace", Action: "git fetch origin"})
							}
						},
						Actions: func() {
							if Button(c, w.Cancel).Clicked() {
								open = false
							}
							PrimaryButton(c, w.OK)
						},
					})
				}, width, 760)
				tt.SetLocale(locale)
				tt.SetScale(2)
				shot := func(name string) { widgetShot(t, tt, fmt.Sprintf("%s-%s-%d", name, locale, width)) }
				if err := tt.Click(w.Track); err != nil {
					t.Fatal(err)
				}
				shot("sheet-enter-0")
				tt.Advance(60 * time.Millisecond)
				shot("sheet-enter-60")
				tt.Advance(60 * time.Millisecond)
				shot("sheet-open")
				tt.Key(0, ui.KeyEscape)
				shot("sheet-exit-0")
				tt.Advance(40 * time.Millisecond)
				shot("sheet-exit-40")
				tt.Advance(40 * time.Millisecond)
				shot("sheet-closed")
				if err := tt.Click(w.Close); err != nil {
					t.Fatal(err)
				}
				shot("popover-enter-0")
				tt.Advance(50 * time.Millisecond)
				shot("popover-enter-50")
				tt.Advance(50 * time.Millisecond)
				shot("popover-open")
				tt.Key(0, ui.KeyEscape)
				busy = true
				tt.Frame()
				shot("spinner-0")
				tt.Advance(179 * time.Millisecond)
				shot("spinner-179")
				tt.Advance(time.Millisecond)
				shot("spinner-180")
				busy = false
				tt.Frame()
				shot("spinner-finished")
			})
		}
	}
}

func widgetShot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	tt.Frame()
	dir := os.Getenv("SHEAF_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, tt.Image()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

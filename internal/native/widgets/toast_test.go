package widgets

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
)

func TestToastDeadlineAndIdle(t *testing.T) {
	var toasts Toaster
	frames, undone := 0, 0
	tt := ui.NewTester(func(c *ui.Context) {
		frames++
		c.SetTheme(theme.Theme())
		toasts.View(c)
	}, 600, 400)
	toasts.Undo("Deleted", func() { undone++ }, "")
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	before := frames
	tt.Advance(UndoTime - 251*time.Millisecond)
	if !tt.HasText("Undo") || frames != before || tt.FramePending() {
		t.Fatalf("before deadline: visible %v, frames %d -> %d, pending %v", tt.HasText("Undo"), before, frames, tt.FramePending())
	}
	tt.Advance(time.Millisecond)
	if tt.HasText("Undo") || undone != 0 {
		t.Fatal("toast did not expire at five seconds, or ran Undo itself")
	}
	tt.Advance(250 * time.Millisecond)
	before = frames
	tt.Advance(time.Second)
	if frames != before || tt.FramePending() {
		t.Fatalf("expired toast keeps drawing: frames %d -> %d, pending %v", before, frames, tt.FramePending())
	}
}

func TestToastLongMessageKeepsActionsVisible(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, width := range []int{360, 1180} {
			t.Run(fmt.Sprintf("%s/%d", locale, width), func(t *testing.T) {
				w := i18n.Common.For(locale)
				message := strings.Repeat(w.AllRepos+": feature/workspace; ", 5)
				var toasts Toaster
				undone := 0
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(theme.Theme())
					ui.Box(c).Fill().Background(theme.Island)
					toasts.View(c)
				}, width, 600)
				tt.SetLocale(locale)
				tt.SetScale(2)
				toasts.Undo(message, func() { undone++ }, "")
				tt.Frame()
				tt.Advance(100 * time.Millisecond)
				widgetShot(t, tt, fmt.Sprintf("toast-long-undo-%s-%d", locale, width))
				text, action := planRect(t, tt, message), planRect(t, tt, w.Undo)
				if text.X < 20 || action.X+action.W > float32(width)-20 || text.X+text.W > action.X || text.Y+text.H > 580 {
					t.Fatalf("toast text or action escaped: text %+v, action %+v", text, action)
				}
				if err := tt.Click(w.Undo); err != nil {
					t.Fatal(err)
				}
				if undone != 1 || tt.HasText(message) {
					t.Fatal("long toast could not be undone")
				}
				toasts.Error(message)
				tt.Frame()
				tt.Advance(100 * time.Millisecond)
				widgetShot(t, tt, fmt.Sprintf("toast-long-error-%s-%d", locale, width))
				close := planRect(t, tt, w.Close)
				if close.X < 20 || close.X+close.W > float32(width)-20 || close.Y+close.H > 580 {
					t.Fatalf("close button escaped: %+v", close)
				}
				tt.Advance(2 * UndoTime)
				if !tt.HasText(message) {
					t.Fatal("error toast expired without dismissal")
				}
				if err := tt.Click(w.Close); err != nil {
					t.Fatal(err)
				}
				if tt.HasText(message) {
					t.Fatal("localized close button did not dismiss the error")
				}
			})
		}
	}
}

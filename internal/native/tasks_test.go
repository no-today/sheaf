package native

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

// The corner of the status bar names the task running and its progress, holds back a task until it has lasted a
// moment, and opens the task list above it with each row's outcome and a Stop button.
func TestTaskStrip(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			w := taskText.For(locale)
			var ts taskStrip
			var tasks []session.Task
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme.Theme())
				ui.Column(c).Fill().Background(theme.FrameBg).Children(func() {
					ui.Spacer(c)
					ui.Row(c).Height(theme.HStatusBar).PaddingX(10).Border(1, theme.Line).BorderWidth(1, 0, 0, 0).FontSize(theme.FSLabel).Children(func() {
						ui.Spacer(c)
						ts.View(c, nil, tasks, false)
					})
				})
			}, 1180, 640)
			tt.SetLocale(locale)
			now := tt.Now()
			tasks = []session.Task{{ID: 9, Kind: session.TaskDiscard, Subject: "a.go", State: session.TaskRunning, Started: now}}
			tt.Frame()
			if _, ok := tt.Find(w.Discard + " · a.go"); ok {
				t.Error("a task shows before it has lasted a moment")
			}
			tasks = []session.Task{
				{ID: 3, Kind: session.TaskSync, Subject: "YT-2841", State: session.TaskRunning, Done: 2, Total: 4, CanStop: true, Started: now.Add(-3 * time.Second),
					Rows: []session.TaskRow{
						{Name: "pay-core · feat/settle", State: session.TaskDone, Note: "updated"},
						{Name: "wms-server · feat/settle", State: session.TaskSkipped, Note: "dirty"},
						{Name: "order-server · feat/settle", State: session.TaskRunning},
						{Name: "settle-web · feat/settle", State: session.TaskWaiting},
					}},
				{ID: 4, Kind: session.TaskIndex, State: session.TaskRunning, Done: 12, Total: 35, Started: now.Add(-2 * time.Second)},
				{ID: 5, Kind: session.TaskMR, Subject: "YT-2841", State: session.TaskDone, Done: 1, Total: 1, Started: now.Add(-90 * time.Second), Ended: now.Add(-80 * time.Second),
					Rows: []session.TaskRow{{Name: "pay-core · feat/settle", State: session.TaskDone, Note: "created", URL: "https://gitlab.example.com/pay/core/-/merge_requests/12"}}},
				{ID: 2, Kind: session.TaskFetch, Subject: "YT-2841", State: session.TaskDone, Done: 2, Total: 2, Started: now.Add(-3 * time.Minute), Ended: now.Add(-2 * time.Minute),
					Rows: []session.TaskRow{
						{Name: "pay-core", State: session.TaskDone, Note: "fetched"},
						{Name: "gateway", State: session.TaskFailed, Note: "fatal: could not read from remote repository"},
					}},
			}
			tt.Frame()
			title := w.Sync + " · YT-2841"
			if _, ok := tt.Find(title); !ok || !tt.HasText("2/4") || !tt.HasText(fill(w.More, 1)) {
				t.Fatalf("the corner does not show the running task: %q", tt.Texts())
			}
			shot(t, tt, "tasks-strip-"+locale)
			if err := tt.Click(title); err != nil {
				t.Fatal(err)
			}
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			for _, want := range []string{w.Stop, w.SkipDirty, w.Waiting, fill(w.FailedRows, 1), "fatal: could not read from remote repository", w.Clear} {
				if !tt.HasText(want) {
					t.Errorf("the task list lacks %q: %q", want, tt.Texts())
				}
			}
			if !tt.HasText(w.Created + " MR !12") {
				t.Errorf("the MR row does not name its merge request: %q", tt.Texts())
			}
			panel, _ := tt.Find(w.Stop)
			strip, _ := tt.Find("2/4")
			if panel.Y >= strip.Y {
				t.Errorf("the task list is not above the corner: stop %+v, corner %+v", panel, strip)
			}
			shot(t, tt, "tasks-panel-"+locale)
			if err := tt.Click(w.Created + " MR !12"); err != nil {
				t.Fatal(err)
			}
			if got := tt.OpenedURLs(); len(got) != 1 || got[0] != "https://gitlab.example.com/pay/core/-/merge_requests/12" {
				t.Errorf("clicking the MR row opened %q", got)
			}
		})
	}
}

// shot writes the tester's frame at 2x to $SHEAF_SHOTS, if set.
func shot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("SHEAF_SHOTS")
	if dir == "" {
		return
	}
	tt.SetScale(2)
	tt.Frame()
	var buf bytes.Buffer
	if err := png.Encode(&buf, tt.Image()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	tt.SetScale(1)
}

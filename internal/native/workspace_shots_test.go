package native

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/testutil"
)

func TestShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			root := viewtest.Fixture(t)
			r := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
			r.Git("checkout", "-q", "-b", "feat-YT-2841")
			r.Commit("YT-2841 shell review", map[string]string{"y.txt": "shell review\n"})
			h := viewtest.Open(t, root)
			viewtest.TrackAll(t, h.S)
			w := New(h.Update, &h.Wins)
			w.smoke = smoke{}
			t.Cleanup(w.Close)
			w.Attach(h.S)
			w.Go(view.Track, view.Smoke{Q: "YT-2841", Row: -1})
			tt := ui.NewTester(w.View, 1440, 900)
			tt.SetLocale(locale)
			hr := &viewtest.Harness{T: t, H: h, TT: tt}
			hr.Until("the requirement stream", func() bool {
				return tt.HasText("repo-a/y.txt") && tt.HasText("shell review") && tt.HasText("+1 −0") && !tt.HasText("正在读取…") && !tt.HasText("Loading…") && !tt.FramePending()
			})
			for _, text := range tt.Texts() {
				r, ok := tt.Find(text)
				if !ok || r.Y >= theme.HTitleBar {
					continue
				}
				switch text {
				case "YT-2841", "shell review", "+1 −0":
					if r.X > 720 {
						t.Errorf("summary %q is not on the left of the title bar: %+v", text, r)
					}
				case "刷新远端", "Refresh Remotes", "更新分支", "Update Branches", "创建 MR/PR", "Create MR/PR", "1", "创建 MR/PR · 1", "Create MR/PR · 1":
					if r.X < 720 {
						t.Errorf("action %q is not on the right of the title bar: %+v", text, r)
					}
				default:
					t.Errorf("unexpected title bar text %q at %+v", text, r)
				}
			}
			hr.Shot("workspace-requirement-1440-" + locale)
			w0 := i18n.Common.For(locale)
			if err := tt.Click(w0.HideLeft + "  ⌘B"); err != nil {
				t.Fatal(err)
			}
			hide := "隐藏右栏  ⌥⌘B"
			if locale == "en-US" {
				hide = "Hide right pane  ⌥⌘B"
			}
			if err := tt.Click(hide); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if tt.HasText("shell review") && tt.HasText(w0.ShowLeft+"  ⌘B") {
				hr.Shot("workspace-requirement-folded-1440-" + locale)
			} else {
				t.Errorf("hiding both panes: %q", tt.Texts())
			}
		})
	}
}

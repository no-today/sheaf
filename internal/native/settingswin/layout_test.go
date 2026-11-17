package settingswin

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsPageControlsFitContent(t *testing.T) {
	w, h, tt := open(t, "general", true, Deps{RevealCommandLog: func() {}})
	until(t, h, tt, "indexed settings", func() bool { return w.haveInfo && w.info.State.Phase == "ready" && !w.info.State.Indexing })
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, width := range []float32{640, 1180} {
			t.Run(fmt.Sprintf("%s/%.0f", locale, width), func(t *testing.T) {
				tt.SetLocale(locale)
				tt.SetSize(int(width), 1600)
				text := tr.For(locale)
				rail := float32(180)
				if width == 640 {
					rail = 160
				}
				left := rail + 41
				right := min(left+640, width-41)
				for _, page := range []struct {
					tab    tabID
					adding bool
					labels []string
				}{
					{tabGeneral, false, []string{text.LangAuto, text.LangZh, text.LangEn}},
					{tabMe, true, []string{text.EmailPh, text.Add, text.ReqPattern}},
					{tabRepos, false, []string{text.RepoSearch, fmt.Sprintf(text.RepoTrack, "repo-a")}},
					{tabGlab, false, []string{text.GlabDir, text.Copy, text.GhCopy}},
					{tabIndex, false, []string{text.Refresh, text.Reveal, text.CmdLogOpen}},
				} {
					w.tab, w.adding = page.tab, page.adding
					tt.Frame()
					for _, label := range page.labels {
						r, ok := tt.Find(label)
						if !ok || r.X < left || r.X+r.W > right {
							t.Fatalf("%s: %q = %+v, want inside %.0f…%.0f", page.tab, label, r, left, right)
						}
						if label == text.EmailPh || label == text.ReqPattern || label == text.GlabDir {
							field, ok := tt.AccessByRole(ui.RoleTextField, label)
							if !ok || field.Bounds.W != 240 || field.Bounds.X+field.Bounds.W != right {
								t.Fatalf("%q = %+v, want 240 wide aligned to %.0f", label, field.Bounds, right)
							}
						}
					}
				}
			})
		}
	}
}

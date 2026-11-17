package settingswin

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsNavigation(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			w, h, tt := open(t, "general", true, Deps{})
			tt.SetSize(640, 600)
			tt.SetLocale(locale)
			text := tr.For(locale)
			until(t, h, tt, "workspace navigation", func() bool {
				return w.haveInfo && tt.HasText(text.Workspace+" "+w.info.Name)
			})
			if err := tt.Click(text.TabBase); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if w.shown() != tabBase || !tt.HasText(text.FlowHead) {
				t.Fatal("clicking Branches did not open the page")
			}
			for _, want := range []tabID{tabRepos, tabGlab, tabIndex, tabIndex} {
				tt.Key(0, ui.KeyDown)
				tt.Frame()
				if w.shown() != want {
					t.Fatalf("Down opened %q, want %q", w.shown(), want)
				}
			}
			for _, want := range []tabID{tabGlab, tabRepos, tabBase, tabMe, tabGeneral, tabGeneral} {
				tt.Key(0, ui.KeyUp)
				tt.Frame()
				if w.shown() != want {
					t.Fatalf("Up opened %q, want %q", w.shown(), want)
				}
			}
		})
	}
}

func TestSettingsArrowsStayInInputs(t *testing.T) {
	w, h, tt := open(t, "base", true, Deps{})
	until(t, h, tt, "merge flow", func() bool { return len(w.flow) > 0 })
	if err := tt.Click("第 1 阶段名称"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []ui.Key{ui.KeyDown, ui.KeyUp} {
		tt.Key(0, key)
		tt.Frame()
		if w.shown() != tabBase {
			t.Fatal("editing a flow name switched pages")
		}
	}
	if err := tt.Click("仓库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("搜索仓库"); err != nil {
		t.Fatal(err)
	}
	tt.Type("repo")
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if w.shown() != tabRepos || w.repoSearch != "repo" {
		t.Fatal("editing the repository search switched pages")
	}
}

func TestSettingsNavigationWithoutWorkspace(t *testing.T) {
	w, h, tt := open(t, "base", false, Deps{})
	until(t, h, tt, "general settings", func() bool { return tt.HasText("语言") })
	if tt.HasText("工作区") {
		t.Fatal("global settings show a workspace group")
	}
	for _, key := range []ui.Key{ui.KeyDown, ui.KeyUp} {
		tt.Key(0, key)
		tt.Frame()
		if w.shown() != tabGeneral {
			t.Fatal("global settings navigated to a workspace page")
		}
	}
}

func TestBranchesLayoutFitsSettingsWindow(t *testing.T) {
	w, h, tt := open(t, "base", true, Deps{})
	until(t, h, tt, "branch settings", func() bool { return w.haveInfo })
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, width := range []float32{640, 1180, 1440} {
			t.Run(fmt.Sprintf("%s/%.0f", locale, width), func(t *testing.T) {
				tt.SetLocale(locale)
				tt.SetSize(int(width), 1600)
				tt.Frame()
				text := tr.For(locale)
				railWidth := float32(180)
				if width == 640 {
					railWidth = 160
				}
				nav, ok := tt.Find(text.TabGeneral)
				if !ok || nav.W != railWidth-1 || nav.H != 32 {
					t.Fatalf("navigation row = %+v, want %.0f × 32", nav, railWidth-1)
				}
				left := railWidth + 41
				right := min(left+640, width-41)
				for _, field := range []struct {
					label string
					width float32
				}{
					{fmt.Sprintf(text.FlowName, 1), 160},
					{text.ColBase + " · repo-a", 240},
				} {
					r, ok := tt.Find(field.label)
					if !ok || r.W != field.width || r.X < left || r.X+r.W > right {
						t.Fatalf("%q = %+v, want width %.0f inside %.0f…%.0f", field.label, r, field.width, left, right)
					}
				}
				remove, ok := tt.Find(fmt.Sprintf(text.FlowRemove, 1))
				if !ok || remove.X+remove.W != right {
					t.Fatalf("flow controls = %+v, want right edge %.0f", remove, right)
				}
			})
		}
	}
}

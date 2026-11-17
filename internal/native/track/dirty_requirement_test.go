package track

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

func TestDirtyRequirementLabelsAndTooltips(t *testing.T) {
	p := &Page{reqs: []session.Requirement{{ID: "YT1", Title: "支付校验"}}}
	dirs := []session.DirtyDir{
		{Path: "/repo-a", Repo: "repo-a", Branch: "feat-YT1", Requirement: "YT1", LastChange: 1},
		{Path: "/repo-b", Repo: "repo-b", Branch: "feat-YT2", Requirement: "YT2", LastChange: 1},
		{Path: "/repo-c", Repo: "repo-c", Branch: "master", LastChange: 1},
	}
	d := dirtyPage{view: &session.DirtyView{Groups: []session.DirtyGroup{{Dirs: dirs}}}}
	d.init(p)
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			w := tr.For(locale)
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Themed(c, theme.Theme()).Grow(1).Children(func() { d.table(c, p, w, dirs) })
			}, 554, 400)
			tt.SetLocale(locale)
			texts := tt.Texts()
			for _, id := range []string{"YT1", "YT2"} {
				if !slices.Contains(texts, id) {
					t.Errorf("missing requirement %q: %q", id, texts)
				}
			}
			if slices.Contains(texts, "—") {
				t.Errorf("unassigned directory shows a placeholder: %q", texts)
			}
			found := false
			for _, a := range tt.AccessAll() {
				found = found || strings.Contains(a.Description, "YT1 支付校验")
			}
			if !found {
				t.Error("row tooltip lacks the requirement title")
			}
			for _, a := range tt.AccessAll() {
				if a.Bounds.W > 0 && a.Bounds.X+a.Bounds.W > 555 {
					t.Errorf("directory row overflows the narrow list: %+v", a)
				}
			}
			head := ui.NewTester(func(c *ui.Context) {
				ui.Themed(c, theme.Theme()).Grow(1).Children(func() {
					for _, dir := range dirs {
						d.dirHead(c, w, dir.Path)
					}
				})
			}, 554, 400)
			for _, id := range []string{"YT1", "YT2"} {
				if !slices.Contains(head.Texts(), id) {
					t.Errorf("stream header missing requirement %s: %q", id, head.Texts())
				}
			}
			if slices.Contains(head.Texts(), "—") {
				t.Errorf("unassigned stream header has a requirement placeholder: %q", head.Texts())
			}
		})
	}
}

func TestSettingKeyFollowsRequirementNumber(t *testing.T) {
	inf := session.Info{Repos: []session.RepoInfo{{Worktrees: []session.WorktreeInfo{{Path: "/repo"}}}}}
	keys := map[string]bool{}
	for _, id := range []string{"", "YT1", "YT2"} {
		inf.Repos[0].Worktrees[0].Requirement = id
		key := settingKey(inf)
		if keys[key] {
			t.Errorf("requirement %q did not change settings key", id)
		}
		keys[key] = true
	}
}

package i18n_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"sheaf/internal/native/track"
	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/testutil"
)

func TestResultTabCountsInBothLanguages(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			testutil.Isolate(t)
			root := filepath.Join(t.TempDir(), "workspace")
			for i := range max(1, n) {
				r := testutil.NewRepo(t, filepath.Join(root, fmt.Sprintf("repo-%d", i)))
				r.Commit("base", map[string]string{"base.txt": "base\n"})
				if n > 0 {
					r.Git("checkout", "-q", "-b", "feature")
					r.Commit("counted", map[string]string{"feature.txt": "feature\n"})
				}
			}
			h := viewtest.Open(t, root)
			h.DockOf = track.Dock()
			hr := viewtest.Mount(t, h, "en-US", func(host view.Host) view.View {
				p := track.New(host)
				p.Open(view.TempIntent{Branch: "feature"})
				return p
			})
			hr.Until("the result's tab counts", func() bool {
				return hr.TT.HasText(fmt.Sprintf("Changes %d", n)) && hr.TT.HasText(fmt.Sprintf("Commits %d", n))
			})
			for _, language := range []struct{ locale, changes, commits, summary string }{
				{"en-US", "Changes", "Commits", "repo"},
				{"zh-Hans", "改动", "提交", "仓库"},
			} {
				hr.TT.SetLocale(language.locale)
				hr.TT.Frame()
				for _, label := range []string{language.changes, language.commits} {
					want := fmt.Sprintf("%s %d", label, n)
					if _, ok := hr.TT.Find(want); !ok {
						t.Errorf("%s: missing tab count %q; shown %q", language.locale, want, hr.TT.Texts())
					}
				}
				if hr.TT.HasText(fmt.Sprintf("%d %s", n, language.summary)) {
					t.Errorf("%s: removed repo summary is still visible: %q", language.locale, hr.TT.Texts())
				}
				for _, text := range hr.TT.Texts() {
					if strings.Contains(text, "|") {
						t.Errorf("%s: raw plural template shown: %q", language.locale, text)
					}
				}
			}
		})
	}
}

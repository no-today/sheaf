package settingswin

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/testutil"
)

func TestShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			w, h, tt := open(t, "general", true, Deps{RevealCommandLog: func() {}})
			if err := h.S.SetMe([]string{"old@example.com"}); err != nil {
				t.Fatal(err)
			}
			until(t, h, tt, "settings fixtures", func() bool {
				return w.haveInfo && len(w.info.MeManual) == 1
			})
			tt.SetLocale(locale)
			hr := &viewtest.Harness{T: t, H: h, TT: tt}
			for _, tab := range w.tabs() {
				w.tab = tab
				tt.Frame()
				hr.Shot("settings-" + string(tab) + "-" + locale)
				if tab == tabGlab || tab == tabBase || tab == tabMe || tab == tabGeneral || tab == tabRepos || tab == tabIndex {
					tt.SetSize(1180, 640)
					hr.Shot("settings-" + string(tab) + "-1180-" + locale)
					tt.SetSize(640, 600)
					hr.Shot("settings-" + string(tab) + "-640-" + locale)
					tt.SetSize(900, 640)
				}
			}
			w.tab, w.adding = tabMe, true
			tt.Frame()
			hr.Shot("settings-me-adding-" + locale)
			w.tab, w.adding, w.req = tabMe, false, "[unclosed"
			tt.Frame()
			hr.Shot("settings-me-pattern-invalid-" + locale)
			w.tab, w.glab = tabGlab, "/fixture/missing/glab"
			tt.Frame()
			hr.Shot("settings-glab-missing-" + locale)
		})
	}
}

// A workspace of dozens of repos: the repos page scrolls inside itself instead of growing the window.
func TestShotsManyRepos(t *testing.T) {
	root := viewtest.Fixture(t)
	for i := range 38 {
		testutil.NewRepo(t, filepath.Join(root, fmt.Sprintf("service-%02d", i))).Commit("init", map[string]string{"f": "1\n"})
	}
	h := viewtest.Open(t, root)
	if err := h.S.SetIgnored([]string{"service-03", "service-04"}); err != nil {
		t.Fatal(err)
	}
	w := New("repos", h.S, h.Update, Deps{})
	tt := ui.NewTester(w.View, 900, 640)
	t.Cleanup(w.Close)
	until(t, h, tt, "the repo list", func() bool { return w.haveInfo && len(w.info.Scanned) == 40 && len(w.info.Ignored) == 2 })
	hr := &viewtest.Harness{T: t, H: h, TT: tt}
	for _, locale := range []string{"zh-Hans", "en-US"} {
		tt.SetLocale(locale)
		tt.Frame()
		hr.Shot("settings-repos-many-" + locale)
		w.repoSearch = "service-0"
		tt.Frame()
		hr.Shot("settings-repos-search-" + locale)
		w.repoSearch = ""
	}
}

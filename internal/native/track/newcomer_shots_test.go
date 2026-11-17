package track

import (
	"path/filepath"
	"testing"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestShotsNewcomer(t *testing.T) {
	for _, scene := range []string{"empty", "mismatch", "candidates"} {
		for _, locale := range []string{"zh-Hans", "en-US"} {
			t.Run(scene+"/"+locale, func(t *testing.T) {
				root := t.TempDir()
				if scene != "empty" {
					r := testutil.NewRepo(t, filepath.Join(root, "api"))
					r.Commit("init", map[string]string{"api.go": "package api\n"})
					r.Git("checkout", "-q", "-b", "feat/ABC-12345")
					r.Commit("ABC-12345 validate input", map[string]string{"api.go": "package api\n\nfunc Valid() bool { return true }\n"})
				}
				h := viewtest.Open(t, root)
				if scene == "mismatch" {
					if err := h.S.SetRequirement(`TEAM-\d+`); err != nil {
						t.Fatal(err)
					}
				}
				h.DockOf = Dock()
				hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
				x := &harness{hr, hr.V.(*Page)}
				x.Until("discovery", func() bool { return x.p.reqsOK })
				x.Shot("newcomer-" + scene + "-" + locale)
				w := tr.For(locale)
				if scene == "candidates" {
					if x.TT.HasText(w.IntroSettings) || !x.TT.HasText("ABC-12345") || len(x.p.cands) != 1 {
						t.Fatalf("candidates must keep their existing presentation: %q", x.TT.Texts())
					}
					return
				}
				pattern := session.DefaultRequirement
				if scene == "mismatch" {
					pattern = `TEAM-\d+`
				}
				for _, text := range []string{w.Intro, w.IntroHint, fill(w.IntroPattern, "pattern", pattern), w.IntroSettings} {
					if !x.TT.HasText(text) {
						t.Fatalf("missing introduction text %q in %q", text, x.TT.Texts())
					}
				}
				if err := x.TT.Click(w.IntroSettings); err != nil {
					t.Fatal(err)
				}
				if tabs := h.Wins.Settings(); len(tabs) != 1 || tabs[0] != "me" {
					t.Fatalf("pattern button opened %v", tabs)
				}
			})
		}
	}
}

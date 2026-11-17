package track

import (
	"path/filepath"
	"testing"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestDeletedRequirementWithoutOwnCommitsShowsUnconfirmed(t *testing.T) {
	for _, tc := range []struct{ locale, label string }{
		{"zh-Hans", "无法确认"},
		{"en-US", "Unconfirmed"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			testutil.Isolate(t)
			root := t.TempDir()
			r := testutil.NewRepo(t, filepath.Join(root, "repo"))
			r.Commit("init", map[string]string{"base": "base\n"})
			r.Git("checkout", "-q", "-b", "side")
			r.Commit("other work", map[string]string{"own": "own\n"})
			r.Git("checkout", "-q", "master")
			r.Merge("side", "Merge YT42")
			h := viewtest.Open(t, root)
			if err := h.S.SetRequirement(`YT\d+`); err != nil {
				t.Fatal(err)
			}
			if err := h.S.Track("YT42"); err != nil {
				t.Fatal(err)
			}
			h.DockOf = Dock()
			hr := viewtest.Mount(t, h, tc.locale, func(h view.Host) view.View { return New(h) })
			p := hr.V.(*Page)
			p.chooseReq(session.Requirement{ID: "YT42"})
			hr.Until("unconfirmed repository and requirement group", func() bool {
				return len(p.rows) == 1 && p.rows[0].Stage == session.StageUnknown && len(p.reqs) == 1 && p.reqs[0].Stage == session.StageUnknown && hr.TT.HasText(tc.label)
			})
			if _, ok := hr.TT.Find("YT42 "); !ok {
				t.Fatalf("unconfirmed requirement missing from rail: %v", hr.TT.Texts())
			}
			if name := p.stageName(tr.For(tc.locale), session.StageUnknown); name != tc.label {
				t.Errorf("stage label=%q want=%q", name, tc.label)
			}
			w := tr.For(tc.locale)
			if hr.TT.HasText(fill(w.Merged, "base", p.rows[0].Base)) || branchShown(w, p.rows[0]) != w.NoBranch {
				t.Fatalf("unconfirmed row claims to be merged: %v", hr.TT.Texts())
			}
			hr.Shot("requirement-unknown-" + tc.locale)
		})
	}
}

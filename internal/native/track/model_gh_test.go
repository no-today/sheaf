package track

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

func TestGitHubPRTexts(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			w := tr.For(locale)
			url := "https://github.com/g/p/pull/12"
			if mrRef(url) != "PR #12" {
				t.Fatal(mrRef(url))
			}
			for _, outcome := range []string{"created", "exists"} {
				cell := mrCell(w, session.MRRow{Tool: "gh", Outcome: outcome, URL: url})
				if !strings.Contains(cell.Text, "PR #12") || cell.URL != url {
					t.Errorf("cell %+v", cell)
				}
			}
			auth := mrCell(w, session.MRRow{Tool: "gh", Outcome: "failed", Auth: true, Error: "HTTP 401"})
			if auth.Text != w.MrGhResAuth || !strings.Contains(auth.Title, "gh auth login") || strings.Contains(auth.Title, "glab") {
				t.Errorf("auth %+v", auth)
			}
			for _, tool := range []string{"gh", "glab"} {
				r := session.MRPlanRow{Tool: tool, Branch: "f", Target: "main"}
				kind := "MR"
				if tool == "gh" {
					kind = "PR"
				}
				for _, push := range []bool{false, true} {
					r.Push = push
					if got := mrHow(w, r); !strings.Contains(got, kind) || strings.Contains(got, "an PR") {
						t.Errorf("how %s", got)
					}
				}
				r.Missing = true
				if got := mrHow(w, r); !strings.Contains(got, tool+" auth login") {
					t.Errorf("missing %s", got)
				}
			}
		})
	}
}

func TestMixedMRPRSheetKeepsInstalledRowsRunnable(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			missing := session.MRPlanRow{Key: "hub", Repo: "hub", Tool: "gh", Missing: true, Branch: "feat", Target: "main"}
			allowed := session.MRPlanRow{Key: "lab", Repo: "lab", Tool: "glab", Branch: "feat", Target: "main"}
			sh := &sheet{open: true, action: actionMR, gh: true, glab: true, rows: []sheetRow{
				mrSheetRow(missing), mrSheetRow(allowed),
			}}
			p := &Page{sheet: sh}
			tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.Theme()); p.w = tr.Of(c); p.sheetPopover(c) }, 1180, 760)
			tt.SetLocale(locale)
			w := tr.For(locale)
			if !tt.HasText(fill(w.MrBriefMissing, "tool", "gh")) || !tt.HasText(fill(w.MrBatchGo, "n", 1, "kind", "MR/PR")) {
				t.Fatalf("missing controls: %v", tt.Texts())
			}
			if _, ok := tt.Find(fill(w.PlanPick, "repo", "hub", "branch", "feat")); ok {
				t.Error("missing tool row can be selected")
			}
			if run, skip := sh.count(); run != 1 || skip != 1 {
				t.Errorf("counts %d %d", run, skip)
			}
		})
	}
}

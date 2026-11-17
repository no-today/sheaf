package track

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/view"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestCheckedRepos(t *testing.T) {
	rows := []session.AggRepo{
		{Key: "api:feature/a", Repo: "api", Branch: "feature/a"},
		{Key: "billing", Repo: "billing"},
		{Key: "api:feature/b", Repo: "api", Branch: "feature/b"},
		{Key: "docs", Repo: "docs"},
	}
	for _, tc := range []struct {
		name    string
		checked map[string]bool
		want    []string
	}{
		{name: "nothing selected"},
		{name: "stale key", checked: map[string]bool{"gone": true}},
		{name: "duplicate repo", checked: map[string]bool{"api:feature/a": true, "api:feature/b": true}, want: []string{"api"}},
		{name: "second branch only", checked: map[string]bool{"api:feature/b": true}, want: []string{"api"}},
		{name: "table order", checked: map[string]bool{"docs": true, "api:feature/b": true, "billing": true, "api:feature/a": false}, want: []string{"billing", "api", "docs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkedRepos(rows, tc.checked); !slices.Equal(got, tc.want) {
				t.Errorf("repos = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSyncSheetRowsAndPicks(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []session.BatchPlanRow
		off  []int
		want []session.BatchPick
	}{
		{name: "empty"},
		{
			name: "same repo and same branch in different rows",
			rows: []session.BatchPlanRow{
				{Repo: "group/api", Branch: "release/candidate", Remote: "origin/release/candidate", How: "ref"},
				{Repo: "group/api", Branch: "staging", Remote: "origin/staging", How: "worktree", Worktree: "/workspace/api-staging"},
				{Repo: "billing", Branch: "release/candidate", How: "skip", Skip: "no_remote"},
			},
			want: []session.BatchPick{{Repo: "group/api", Branch: "release/candidate"}, {Repo: "group/api", Branch: "staging"}},
		},
		{
			name: "unticked and skipped excluded",
			rows: []session.BatchPlanRow{
				{Repo: "api", Branch: "release/candidate", Remote: "origin/release/candidate", How: "ref"},
				{Repo: "billing", Branch: "staging", How: "skip", Skip: "unpushed"},
				{Repo: "api", Branch: "staging", Remote: "origin/staging", How: "ref"},
			},
			off:  []int{0},
			want: []session.BatchPick{{Repo: "api", Branch: "staging"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := sheet{action: actionSync, rows: syncSheetRows(session.BatchPlan{Rows: tc.rows})}
			if len(sh.rows) != len(tc.rows) {
				t.Fatalf("got %d sheet rows, want %d", len(sh.rows), len(tc.rows))
			}
			keys := map[string]bool{}
			for i, r := range sh.rows {
				plan := tc.rows[i]
				if r.repo != plan.Repo || r.branch != plan.Branch || r.skip != (plan.How == "skip") || r.off || keys[r.key] {
					t.Errorf("invalid sheet row %d: %+v", i, r)
				}
				keys[r.key] = true
				for _, locale := range []string{"zh-Hans", "en-US"} {
					w := tr.For(locale)
					if got := r.how(w); got == "" || got != syncHow(w, plan) {
						t.Errorf("%s row %d lost its action: %q", locale, i, got)
					}
				}
				// Picks must survive independently of the key used by the UI.
				sh.rows[i].key = "opaque"
				sh.rows[i].off = slices.Contains(tc.off, i)
			}
			if got := sh.syncPicks(); !slices.Equal(got, tc.want) {
				t.Errorf("picks = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSyncPlanText(t *testing.T) {
	for _, tc := range []struct {
		name   string
		row    session.BatchPlanRow
		zh, en string
	}{
		{"without checkout", session.BatchPlanRow{How: "ref", Branch: "release/a", Remote: "origin/release/a"}, "把 release/a 更新到 origin/release/a", "Update release/a to origin/release/a"},
		{"worktree", session.BatchPlanRow{How: "worktree", Branch: "staging", Remote: "origin/staging", Worktree: "/workspace/api-staging"}, "在 api-staging 把 staging 更新到 origin/staging", "In api-staging, update staging to origin/staging"},
		{"no local", session.BatchPlanRow{How: "skip", Skip: "no_local", Branch: "release/a"}, "本地没有 release/a，跳过", "No local release/a, skipped"},
		{"no remote", session.BatchPlanRow{How: "skip", Skip: "no_remote", Branch: "release/a"}, "远端没有 release/a，跳过", "No release/a on the remote, skipped"},
		{"busy", session.BatchPlanRow{How: "skip", Skip: "merging", Worktree: "/workspace/api-staging"}, "api-staging 有操作没做完，跳过", "api-staging has an unfinished operation, skipped"},
		{"dirty one", session.BatchPlanRow{How: "skip", Skip: "dirty", Worktree: "/workspace/api-staging", Dirty: 1}, "api-staging 有 1 处未提交改动，跳过", "api-staging has 1 uncommitted change: skipped"},
		{"dirty many", session.BatchPlanRow{How: "skip", Skip: "dirty", Worktree: "/workspace/api-staging", Dirty: 2}, "api-staging 有 2 处未提交改动，跳过", "api-staging has 2 uncommitted changes: skipped"},
		{"unpushed", session.BatchPlanRow{How: "skip", Skip: "unpushed"}, "有提交没推送，跳过", "Unpushed commits, skipped"},
		{"unknown reason", session.BatchPlanRow{How: "skip", Skip: "future_reason"}, "future_reason", "future_reason"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, lang := range []struct{ locale, want string }{{"zh-Hans", tc.zh}, {"en-US", tc.en}} {
				if got := syncHow(tr.For(lang.locale), tc.row); got != lang.want {
					t.Errorf("%s = %q, want %q", lang.locale, got, lang.want)
				}
			}
		})
	}
}

func TestSyncSummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		run    session.BatchRun
		zh, en string
	}{
		{name: "empty"},
		{name: "singular", run: session.BatchRun{Updated: 1}, zh: "已更新 1 个分支", en: "1 branch updated"},
		{name: "plural", run: session.BatchRun{Updated: 3}, zh: "已更新 3 个分支", en: "3 branches updated"},
		{name: "up to date", run: session.BatchRun{UpToDate: 2}, zh: "2 个已是最新", en: "2 up to date"},
		{name: "skipped", run: session.BatchRun{Skipped: 2}, zh: "跳过 2 个", en: "2 skipped"},
		{name: "failed", run: session.BatchRun{Failed: 1}, zh: "1 个失败", en: "1 failed"},
		{name: "mixed", run: session.BatchRun{Updated: 2, UpToDate: 3, Failed: 1, Skipped: 4}, zh: "已更新 2 个分支 · 3 个已是最新 · 1 个失败 · 跳过 4 个", en: "2 branches updated · 3 up to date · 1 failed · 4 skipped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, lang := range []struct{ locale, want string }{{"zh-Hans", tc.zh}, {"en-US", tc.en}} {
				if got := syncSummary(tr.For(lang.locale), tc.run); got != lang.want {
					t.Errorf("%s = %q, want %q", lang.locale, got, lang.want)
				}
			}
		})
	}
}

func TestBatchButtonsGreyOutWithoutSelectedRows(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			p := &Page{rows: []session.AggRepo{{Key: "api", Repo: "api"}}, checked: map[string]bool{"api": true}}
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme.Theme())
				p.w = tr.Of(c)
				ui.Row(c).Gap(6).Children(func() { p.titleActions(c, p.w) })
			}, 1180, 100)
			tt.SetLocale(locale)
			w := tr.For(locale)
			mr, mrOK := tt.Find(fill(w.MrCount, "n", 1))
			sync, syncOK := tt.Find(w.SyncAction)
			if !mrOK || !syncOK || mr.X < sync.X+sync.W || sync.Y != mr.Y {
				t.Errorf("MR is not to the right of sync: mr %+v, sync %+v", mr, sync)
			}
			badge, badgeOK := tt.Find("1")
			if !badgeOK || badge.X < mr.X+mr.W/2 || badge.X+badge.W > mr.X+mr.W || badge.Y < mr.Y || badge.Y+badge.H > mr.Y+mr.H {
				t.Errorf("the count is not inside the end of MR: badge %+v, mr %+v", badge, mr)
			}
			// The count sits in a slot the button always keeps: the button keeps its width whatever the number.
			p.rows = append(p.rows, session.AggRepo{Key: "web", Repo: "web"})
			for i := range 9 {
				k := fmt.Sprint("r", i)
				p.rows = append(p.rows, session.AggRepo{Key: k, Repo: k})
				p.checked[k] = true
			}
			p.checked["web"] = true
			tt.Frame()
			if wide, _ := tt.Find(fill(w.MrCount, "n", 11)); wide.W != mr.W || !tt.HasText("11") {
				t.Errorf("MR changed width with 11 rows: %+v, was %+v; texts %q", wide, mr, tt.Texts())
			}
			p.rows = p.rows[:1]
			p.checked = map[string]bool{"api": false}
			tt.Frame()
			for _, label := range []string{w.Fetch, w.SyncAction, w.MrAction} {
				if !tt.HasText(label) {
					t.Errorf("action %q is hidden without selected rows; texts %q", label, tt.Texts())
				}
			}
			r, _ := tt.Find(w.SyncAction)
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(time.Second)
			if !tt.HasText(w.ActNeedReq) {
				t.Errorf("a greyed-out action does not say why; texts %q", tt.Texts())
			}
			if err := tt.Click(w.MrAction); err == nil && p.mrRun != nil {
				t.Error("a greyed-out MR started")
			}
		})
	}
}

func TestSyncWithoutPlanShowsToast(t *testing.T) {
	for _, tc := range []struct {
		name string
		favs []string
	}{
		{name: "no favourites"},
		{name: "no local branches", favs: []string{"unknown-branch"}},
	} {
		for _, locale := range []string{"zh-Hans", "en-US"} {
			t.Run(tc.name+"/"+locale, func(t *testing.T) {
				h := open(t, locale)
				if err := h.H.S.SetFavs(tc.favs); err != nil {
					t.Fatal(err)
				}
				w := tr.For(locale)
				shotClick(t, h, w.SyncAction)
				if h.p.sheet != nil && h.p.sheet.open {
					t.Fatal("opened a sheet before finding any plan rows")
				}
				h.Until("favourite hint", func() bool { return h.p.sheet == nil && h.TT.HasText(w.SyncNoFavs) })
				if h.p.syncRun != nil {
					t.Error("sync ran without a plan")
				}
			})
		}
	}
}

func TestSyncResultsBelongToTheirFilter(t *testing.T) {
	for _, tc := range []struct {
		name, req, branch, runFor string
		want                      bool
	}{
		{name: "requirement", req: "YT-2841", runFor: "tmp:YT-2841", want: true},
		{name: "other requirement", req: "YT-2856", runFor: "tmp:YT-2841"},
		{name: "temporary", branch: "feature/a", runFor: "tmp:feature/a", want: true},
		{name: "other temporary", branch: "feature/b", runFor: "tmp:feature/a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Page{input: &session.TagAggInput{}, syncFor: tc.runFor, syncRun: &session.BatchRun{Rows: []session.BatchRow{{Repo: "api"}}}, temp: &tempFilter{Req: tc.req, Branch: tc.branch}}
			if got := p.showSync(); got != tc.want {
				t.Errorf("showSync = %v, want %v", got, tc.want)
			}
			p.input = nil
			if p.showSync() {
				t.Error("sync result shown outside a filter")
			}
		})
	}
}

func syncShots(t *testing.T) {
	testutil.Isolate(t)
	tmp := t.TempDir()
	root := filepath.Join(tmp, "workspace")
	branch := "release/candidate"
	repos := map[string]*testutil.Repo{}
	before, remote := map[string]string{}, map[string]string{}
	for _, name := range []string{"api", "billing", "docs", "outside"} {
		up := testutil.NewRepo(t, filepath.Join(tmp, "up", name))
		up.Commit("initial", map[string]string{"a.txt": "original\n"})
		up.Git("branch", "-M", "master")
		up.Git("branch", branch)
		up.Git("checkout", "-q", "-b", "feat-YT1")
		up.Commit("YT1 work", map[string]string{"feature.txt": name + "\n"})
		up.Git("checkout", "-q", "master")
		origin := filepath.Join(tmp, name+".git")
		up.Git("clone", "-q", "--bare", up.Dir, origin)
		up.Git("remote", "add", "origin", origin)
		r := testutil.NewRepoAt(t, filepath.Join(root, name))
		up.Git("clone", "-q", origin, r.Dir)
		r.Git("config", "user.name", "Me")
		r.Git("config", "user.email", "me@example.com")
		r.Git("branch", branch, "origin/"+branch)
		r.Git("checkout", "-q", "-b", "feat-YT1", "origin/feat-YT1")
		if name == "api" || name == "billing" {
			wt := filepath.Join(root, name+"-release")
			r.Git("worktree", "add", "-q", wt, branch)
			if name == "billing" {
				shotWrite(t, filepath.Join(wt, "a.txt"), "local changes\n", 0o644)
			}
		}
		before[name] = r.Git("rev-parse", branch)
		up.Git("checkout", "-q", branch)
		remote[name] = up.Commit("remote update", map[string]string{"release.txt": "updated\n"})
		up.Git("push", "-q", "origin", branch)
		repos[name] = r
	}
	h := openAt(t, root, "zh-Hans")
	h.Until("sync fixture", func() bool { return len(h.p.rows) == 4 && !h.p.aggBusy && !h.p.filesBusy })
	if err := h.H.S.SetFavs([]string{"origin/" + branch}); err != nil {
		t.Fatal(err)
	}
	if len(h.p.favs) != 0 {
		t.Fatal("page should still have the old favourites")
	}
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h.TT.SetLocale(locale)
		h.TT.SetSize(1180, 760)
		h.Shot("track-sync-selected-1180-" + locale)
	}
	h.TT.SetLocale("zh-Hans")
	shotClick(t, h, zh.SyncAction)
	h.Until("sync plan", func() bool { return h.p.sheet != nil && !h.p.sheet.busy })
	sh := h.p.sheet
	if sh.action != actionSync || len(sh.rows) != 4 {
		t.Fatalf("unexpected sync plan: %+v", sh)
	}
	for _, r := range sh.rows {
		if r.branch != branch || r.skip != (r.repo == "billing") {
			t.Errorf("sync used the wrong scope or favourites: %+v", r)
		}
		if got := repos[r.repo].Git("rev-parse", branch); got != before[r.repo] {
			t.Error("plan changed a branch before confirmation")
		}
	}
	n, skipped := sh.count()
	if n != 3 || skipped != 1 {
		t.Fatalf("plan = %d runnable, %d skipped; want 3, 1", n, skipped)
	}
	batchSheetShots(h, "track-sync-plan", 1400)
	shotClick(t, h, fill(zh.PlanPick, "repo", "docs", "branch", branch))
	shotClick(t, h, fill(zh.PlanPick, "repo", "outside", "branch", branch))
	shotClick(t, h, planCount(zh.SyncGo, 1))
	h.Until("sync results", func() bool {
		return h.p.sheet == nil && h.p.syncRun != nil && !h.p.aggBusy && !h.p.filesBusy && h.TT.HasText(zh.SyncDone)
	})
	if run := h.p.syncRun; run.Updated != 1 || run.Skipped != 0 || run.Failed != 0 || len(run.Rows) != 1 {
		t.Errorf("unexpected sync run: %+v", run)
	}
	if len(h.p.frozen) != len(h.p.rows) {
		t.Error("sync did not preserve the table order")
	}
	for name, r := range repos {
		want := before[name]
		if name == "api" {
			want = remote[name]
		}
		if got := r.Git("rev-parse", branch); got != want {
			t.Errorf("%s branch = %s, want %s", name, got, want)
		}
		if got := r.Git("branch", "--show-current"); got != "feat-YT1" {
			t.Errorf("%s changed the checked-out branch to %s", name, got)
		}
	}
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h.TT.SetLocale(locale)
		h.TT.SetSize(1180, 760)
		h.Shot("track-sync-result-1180-" + locale)
	}
	h.TT.SetLocale("zh-Hans")
	h.p.Open(view.TempIntent{Branch: "feat-YT1"})
	h.Until("another filter", func() bool { return h.p.input != nil && !h.p.aggBusy })
	if h.p.showSync() || h.TT.HasText(zh.SyncDone) {
		t.Error("sync result leaked into another filter")
	}
	h.p.chooseReq(session.Requirement{ID: "YT1"})
	h.Until("original filter", func() bool { return h.TT.HasText(zh.SyncDone) })
	shotClick(t, h, zh.Close)
	if h.p.syncRun != nil || h.TT.HasText(zh.SyncDone) {
		t.Error("closing the summary did not clear the sync result")
	}
}

package track

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

// The histories follow the fixtures in session's aggregation tests.
func reviewFixture(t *testing.T) string {
	t.Helper()
	testutil.Isolate(t)
	root := filepath.Join(t.TempDir(), "workspace")
	for _, name := range []string{"api", "billing", "desktop", "docs", "legacy", "mobile"} {
		r := testutil.NewRepo(t, filepath.Join(root, name))
		r.Commit("初始化 "+name, map[string]string{"a.txt": "original\n", "c.txt": "original\n"})
		r.Git("checkout", "-q", "-b", "feat-YT1")
		files := map[string]string{"feature.txt": name + "\n"}
		if name == "billing" {
			files = map[string]string{"c.txt": "feature version\n"}
		}
		r.Commit("YT1 完成功能 · "+name, files)
		if name == "api" {
			r.Commit("YT1 补充校验", map[string]string{"src/validate.go": "package api\n\nfunc Valid() bool { return true }\n"})
		}
		r.Git("branch", "release", "master")
		if name == "desktop" {
			r.Git("branch", "-f", "release", "feat-YT1")
		}
		r.Git("checkout", "-q", "master")
		if name == "docs" {
			r.Merge("feat-YT1", "合入 YT1")
			r.Git("branch", "-D", "feat-YT1")
			continue
		}
		mainFiles := map[string]string{"main.txt": "main moved\n"}
		if name == "billing" {
			mainFiles = map[string]string{"c.txt": "main version\n"}
		}
		r.Commit("更新主分支", mainFiles)
		wt := filepath.Join(root, name+"-YT1")
		r.Git("worktree", "add", "-q", wt, "feat-YT1")
		if name == "legacy" {
			r.Git("branch", "-m", "master", "main")
		}
		if name == "mobile" {
			shotWrite(t, filepath.Join(wt, "feature.txt"), "edited locally\n", 0o644)
			shotWrite(t, filepath.Join(wt, "scratch.txt"), "new local file\n", 0o644)
		}
	}
	return root
}

func shotWrite(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func shotClick(t *testing.T, h *harness, label string) {
	t.Helper()
	if err := h.TT.Click(label); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
}

func batchSheetShots(h *harness, scene string, width int) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h.TT.Move(10, 10)
		h.TT.SetLocale(locale)
		h.TT.SetSize(width, 900)
		h.Shot(scene + "-" + locale)
		h.TT.SetSize(1180, 760)
		h.Shot(scene + "-1180-" + locale)
	}
	h.TT.SetLocale("zh-Hans")
	h.TT.SetSize(width, 900)
}

func reviewShots(t *testing.T) {
	root := reviewFixture(t)
	h := openAt(t, root, "zh-Hans")
	h.Until("all six repositories and their files", func() bool {
		return len(h.p.rows) == 6 && !h.p.filesBusy && h.p.commits != nil
	})
	h.p.setBase("master")
	h.Until("all overview states", func() bool {
		if h.p.aggBusy || h.p.filesBusy || h.p.commits == nil {
			return false
		}
		seen := map[string]bool{}
		for _, r := range h.p.rows {
			switch r.Repo {
			case "api":
				seen[r.Repo] = r.Ahead > 0 && r.Stage == session.StagePending
			case "desktop":
				seen[r.Repo] = r.Stage == session.StageUAT
			case "docs":
				seen[r.Repo] = r.BranchFrom == session.FromMerged
			case "legacy":
				seen[r.Repo] = r.Skip == session.SkipNoBase
			case "mobile":
				seen[r.Repo] = r.Uncommitted == 2
			}
		}
		return seen["api"] && seen["desktop"] && seen["docs"] && seen["legacy"] && seen["mobile"]
	})
	h.Until("the stream", func() bool { return h.p.stream.Loaded() > 0 && len(h.p.stats) == len(h.p.rows) })
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h.TT.SetLocale(locale)
		for _, width := range []int{1180, 1400} {
			h.TT.SetSize(width, 900)
			h.Shot(fmt.Sprintf("track-stream-%d-%s", width, locale))
		}
	}
	h.TT.SetLocale("zh-Hans")
	h.TT.SetSize(1400, 900)
	for _, r := range h.p.rows {
		if r.Repo == "mobile" {
			h.railClick(t, r.Repo+" "+h.p.stageName(wordsOf(h), r.Stage))
		}
	}
	h.Until("the mobile files", func() bool { return h.TT.HasText("feature.txt") })
	h.Shot("track-stream-jump-zh-Hans")
	shotClick(t, h, wordsOf(h).TabCommits+" "+itoa(h.p.commits.Total))
	h.Until("the commits", func() bool { return h.p.tab == 1 && h.TT.HasText("YT1 补充校验") })
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h.TT.SetLocale(locale)
		h.Shot("track-commits-" + locale)
		h.TT.SetSize(1180, 900)
		h.Shot("track-commits-1180-" + locale)
		h.TT.SetSize(1400, 900)
	}
	h.p.tab = 0
	h.TT.SetLocale("zh-Hans")

	shotClick(t, h, wordsOf(h).Master+" "+valueOr(h.p.master, wordsOf(h).CompareBase))
	h.Until("compare branch choices", func() bool { return h.p.pop.ok })
	h.Shot("track-compare-target-popover-zh-Hans")
	h.TT.Key(0, ui.KeyEscape)
	h.TT.Frame()
	h.p.stream.ScrollToTop()
	h.Until("the first repo's branch button", func() bool { return h.p.stream.Current() == 0 && !h.TT.FramePending() })
	h.mainClick(t, "feat-YT1")
	h.Until("row branch choices", func() bool { return h.p.pop.ok && h.p.pop.kind == "branch" })
	h.Shot("track-row-branch-popover-zh-Hans")
	h.TT.Key(0, ui.KeyEscape)
	h.TT.Frame()

	untrackForDirty(t, h)
	h.p.choose(wcID)
	h.Until("uncommitted files", func() bool {
		return h.p.dirty.view != nil && h.TT.HasText("scratch.txt") && h.TT.HasText("feature.txt")
	})
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h.TT.SetLocale(locale)
		h.Shot("track-uncommitted-files-" + locale)
		h.TT.SetSize(1180, 760)
		h.Shot("track-uncommitted-files-1180-" + locale)
		h.TT.SetSize(1400, 900)
	}
}

func mrReviewShots(t *testing.T) {
	// Same local bare-origin URL rewrite and glab responses as session.TestCreateMR.
	testutil.Isolate(t)
	tmp := t.TempDir()
	root := filepath.Join(tmp, "workspace")
	for _, name := range []string{"ahead", "synced", "lone"} {
		r := testutil.NewRepo(t, filepath.Join(root, name))
		r.Commit("初始化", map[string]string{"a.txt": "a\n"})
		if name != "lone" {
			origin := filepath.Join(tmp, name+".git")
			r.Git("clone", "-q", "--bare", r.Dir, origin)
			web := "https://gitlab.example.com/g/" + name + ".git"
			r.Git("remote", "add", "origin", web)
			r.Git("config", "url."+origin+".insteadOf", web)
		}
		r.Git("checkout", "-q", "-b", "feat-YT1")
		r.Commit("YT1 完成功能", map[string]string{"feature.txt": name + "\n"})
		if name == "synced" {
			r.Git("push", "-q", "origin", "feat-YT1")
		}
	}
	bin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	shotWrite(t, filepath.Join(bin, "glab"), `#!/bin/sh
case "$1 $2" in
"mr list") printf '[]' ;;
"mr create") echo "https://gitlab.example.com/g/$(basename "$PWD")/-/merge_requests/1" ;;
esac
`, 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := openAt(t, root, "zh-Hans")
	h.Until("MR rows and files", func() bool { return len(h.p.rows) == 3 && !h.p.filesBusy })
	shotClick(t, h, fill(wordsOf(h).MrCount, "n", len(h.p.ticked())))
	h.Until("MR plan", func() bool { return h.p.sheet != nil && !h.p.sheet.busy && len(h.p.sheet.rows) == 3 })
	if !h.p.sheet.glab {
		t.Fatal("fixture glab rows were not found")
	}
	batchSheetShots(h, "track-create-mr-dialog", 1400)
	shotClick(t, h, "创建 2 个 MR")
	h.Until("MR results", func() bool {
		return h.p.sheet == nil && h.p.mrRun != nil && h.p.mrRun.Created == 2 && !h.p.aggBusy && !h.p.filesBusy && h.TT.HasText("MR 已处理")
	})
	batchSheetShots(h, "track-create-mr-result", 1400)
	h.p.startMR()
	h.Until("second MR plan", func() bool { return h.p.sheet != nil && h.p.sheet.open })
	shotClick(t, h, fill(zh.PlanPick, "repo", "ahead", "branch", "feat-YT1"))
	sh := h.p.sheet
	opened := false
	h.H.Toaster.Details = func() { opened = true }
	shotClick(t, h, "创建 1 个 MR")
	if sh.open || !opened {
		t.Fatal("confirming the MR must close the dialog and show the task list")
	}
	h.Until("selected MR result", func() bool { return h.p.sheet == nil && h.p.mrRun.Created == 1 })
	var latest session.Task
	for _, task := range h.H.S.Tasks() {
		if task.Kind == session.TaskMR {
			latest = task
			break
		}
	}
	if latest.Total != 1 || latest.Done != 1 || len(latest.Rows) != 1 || latest.Rows[0].Name != "synced · feat-YT1" || latest.Rows[0].State != session.TaskDone {
		t.Fatalf("task after unchecking ahead and skipping lone = %+v", latest)
	}
	t.Run("github", mrGitHubReviewShots)
}

func mrGitHubReviewShots(t *testing.T) {
	testutil.Isolate(t)
	tmp := t.TempDir()
	root := filepath.Join(tmp, "workspace")
	for _, name := range []string{"hub", "lab"} {
		r := testutil.NewRepo(t, filepath.Join(root, name))
		r.Commit("init", map[string]string{"a.txt": "a\n"})
		origin := filepath.Join(tmp, name+".git")
		r.Git("clone", "-q", "--bare", r.Dir, origin)
		host := "gitlab.example.com"
		if name == "hub" {
			host = "github.com"
		}
		web := "https://" + host + "/g/" + name + ".git"
		r.Git("remote", "add", "origin", web)
		r.Git("config", "url."+origin+".insteadOf", web)
		r.Git("switch", "-q", "-c", "feat-YT1")
		r.Commit("YT1 add feature", map[string]string{"feature.txt": name + "\n"})
	}
	bin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	shotWrite(t, filepath.Join(bin, "glab"), `#!/bin/sh
case "$1 $2" in
"mr list") printf '[]' ;;
"mr create") echo "https://gitlab.example.com/g/lab/-/merge_requests/1" ;;
esac
`, 0755)
	shotWrite(t, filepath.Join(bin, "gh"), `#!/bin/sh
if [ "$SHOT_GH_AUTH" = 1 ]; then echo "run gh auth login" >&2; exit 4; fi
case "$1 $2" in
"pr list") printf '[]' ;;
"pr create") echo "https://github.com/g/hub/pull/12" ;;
esac
`, 0755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := openAt(t, root, "zh-Hans")
	h.Until("mixed rows", func() bool { return len(h.p.rows) == 2 && !h.p.filesBusy && !h.p.aggBusy })
	h.p.startMR()
	h.Until("mixed MR/PR plan", func() bool { return h.p.sheet != nil && !h.p.sheet.busy && len(h.p.sheet.rows) == 2 })
	batchSheetShots(h, "track-create-mr-pr-dialog", 1400)
	t.Setenv("SHOT_GH_AUTH", "1")
	h.p.runSheet()
	h.Until("GitHub auth failure", func() bool {
		return h.p.sheet == nil && h.p.mrRun != nil && h.p.mrRun.Failed == 1 && h.p.mrRun.Created == 1 && !h.p.aggBusy && !h.p.filesBusy
	})
	batchSheetShots(h, "track-create-pr-auth", 1400)
	t.Setenv("SHOT_GH_AUTH", "0")
	h.p.startMR()
	h.Until("retry MR/PR plan", func() bool { return h.p.sheet != nil && !h.p.sheet.busy && len(h.p.sheet.rows) == 2 })
	h.p.runSheet()
	h.Until("MR and PR results", func() bool {
		return h.p.sheet == nil && h.p.mrRun != nil && h.p.mrRun.Created == 2 && !h.p.aggBusy && !h.p.filesBusy
	})
	batchSheetShots(h, "track-create-mr-pr-result", 1400)
	missing := session.MRPlanRow{Key: "hub", Repo: "hub", Tool: "gh", Missing: true, Branch: "feat-YT1", Target: "master"}
	allowed := session.MRPlanRow{Key: "lab", Repo: "lab", Tool: "glab", Branch: "feat-YT1", Target: "master"}
	h.p.sheet = &sheet{open: true, action: actionMR, gh: true, glab: true, rows: []sheetRow{
		mrSheetRow(missing), mrSheetRow(allowed),
	}}
	batchSheetShots(h, "track-create-pr-missing", 1400)
}

func firstIndexShots(t *testing.T) {
	root := fixture(t)
	h := viewtest.Open(t, root)
	viewtest.TrackAll(t, h.S)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	paused, resume, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	s, err := session.Open(ctx, root, session.Options{DataDir: t.TempDir(), CacheDir: t.TempDir(),
		OnState: func(st session.State) {
			h.Update(func() { h.St = st; h.Inf = h.S.Info() })
			if st.Indexing && !st.Ready && st.Total > 0 {
				once.Do(func() { close(paused); <-resume })
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	h.S, h.Inf, h.St = s, s.Info(), s.Info().State
	go func() { done <- s.Refresh(ctx) }()
	defer func() { close(resume); <-done }()
	select {
	case <-paused:
	case <-time.After(2 * time.Second):
		t.Fatal("initial index did not report progress")
	}
	h.Drain()
	h.DockOf = Dock()
	for _, locale := range []string{"zh-Hans", "en-US"} {
		hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
		w := tr.For(locale)
		if h.St.Ready || !h.St.Indexing || !hr.TT.HasText(w.BuildingIndex) || !hr.TT.HasText(w.BuildingIndexHint) {
			t.Fatalf("not the initial indexing view: %+v, %q", h.St, hr.TT.Texts())
		}
		hr.Shot("track-first-index-running-" + locale)
		hr.TT.SetSize(1180, 900)
		hr.Shot("track-first-index-running-1180-" + locale)
	}
}

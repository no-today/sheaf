package settingswin

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/testutil"
)

// open shows the settings window of the viewtest fixture's session (nil: global settings) on page tab.
func open(t *testing.T, tab string, workspace bool, d Deps) (*Window, *viewtest.Host, *ui.Tester) {
	t.Helper()
	h := viewtest.Open(t, viewtest.Fixture(t))
	var w *Window
	if workspace {
		w = New(tab, h.S, h.Update, d)
	} else {
		w = New(tab, nil, h.Update, d)
	}
	tt := ui.NewTester(w.View, 900, 640)
	tt.SetLocale("zh-Hans")
	t.Cleanup(w.Close)
	return w, h, tt
}

func until(t *testing.T, h *viewtest.Host, tt *ui.Tester, what string, ok func() bool) {
	t.Helper()
	for range 400 {
		h.Drain()
		tt.Frame()
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; texts %q", what, tt.Texts())
}

func TestGlobalSettingsShowLanguage(t *testing.T) {
	var saved []Settings
	_, h, tt := open(t, "me", false, Deps{
		Settings:    func() Settings { return Settings{} },
		SetSettings: func(s Settings) error { saved = append(saved, s); return nil },
	})
	until(t, h, tt, "general page", func() bool { return tt.HasText("语言") }) // "me" is not there: general shows
	for _, gone := range []string{"我的提交", "主分支", "数据", "MR/PR", "打开方式", "启动时重新打开上次的工作区"} {
		if tt.HasText(gone) {
			t.Errorf("%q shows in global settings", gone)
		}
	}
	// A change applies at once, with no save button.
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Lang != "en" {
		t.Errorf("saved %+v", saved)
	}
}

func TestAnInfoReadBeforeAMutateIsDropped(t *testing.T) {
	w, h, _ := open(t, "base", true, Deps{})
	seq := w.infoSeq.Load()
	w.mutate(func() error { return nil })
	// What the poll read before the mutate must not replace what the mutate reads after.
	stale := w.s.Info()
	stale.Master = "stale"
	w.update(func() {
		if seq == w.infoSeq.Load() {
			w.info = stale
		}
	})
	for range 100 {
		h.Drain()
		time.Sleep(5 * time.Millisecond)
	}
	if w.info.Master == "stale" || !w.haveInfo {
		t.Errorf("info %+v", w.info)
	}
}

func TestMyCommitsAddAndRemoveWithUndo(t *testing.T) {
	_, h, tt := open(t, "me", true, Deps{})
	until(t, h, tt, "the page", func() bool { return tt.HasText("我的邮箱") && tt.HasText("+ 添加邮箱") })
	if err := tt.Click("+ 添加邮箱"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("bob@example.")
	tt.Frame()
	if !tt.HasText("邮箱格式不对") { // it looks like an address, so it is judged
		t.Errorf("texts %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !tt.HasText("+ 添加邮箱") {
		t.Error("Escape did not cancel adding")
	}
	if err := tt.Click("+ 添加邮箱"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("Bob@Example.com")
	tt.Frame()
	if err := tt.Click("添加"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "the email listed", func() bool { return tt.HasText("bob@example.com") && tt.HasText("手动添加") })
	if got := h.S.Info().MeManual; !slices.Equal(got, []string{"bob@example.com"}) {
		t.Fatalf("manual emails %v", got)
	}
	// Removing is done at once, with an Undo.
	if err := tt.Click("移除 bob@example.com"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "the email gone", func() bool {
		return !slices.Contains(h.S.Info().MeManual, "bob@example.com") && tt.HasText("已移除 bob@example.com")
	})
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "the email back", func() bool { return slices.Contains(h.S.Info().MeManual, "bob@example.com") })
}

func TestBaseBranchChoicesAreBranchesNotTags(t *testing.T) {
	w, h, tt := open(t, "base", true, Deps{})
	until(t, h, tt, "the repos", func() bool { return tt.HasText("repo-a") && tt.HasText("repo-b") })
	got := w.choices("repo-a", "")
	if !slices.Contains(got, "master") && !slices.Contains(got, "main") {
		t.Errorf("choices %v have no default branch", got)
	}
	if got := w.choices("repo-a", "origin/odd"); !slices.Contains(got, "origin/odd") { // the current base is always a choice
		t.Errorf("choices %v lack the current base", got)
	}
}

func TestGlabConfigAppliesOnEnterAndSaysWhenMissing(t *testing.T) {
	_, h, tt := open(t, "glab", true, Deps{})
	until(t, h, tt, "the page", func() bool { return tt.HasText("glab 配置目录") })
	if tt.HasText("这个目录不可用") {
		t.Error("an empty directory is the system's login: not a problem")
	}
	r, _ := tt.Find("glab 配置目录")
	tt.ClickAt(700, r.Y+r.H/2) // in the field at the right of the label
	tt.Frame()
	tt.Type("/definitely/not/here")
	tt.Key(0, ui.KeyEnter)
	until(t, h, tt, "saved", func() bool { return h.S.Info().GlabConfig == "/definitely/not/here" })
	until(t, h, tt, "the warning", func() bool {
		return tt.HasText("目录不存在，请先创建（可用 ~）")
	})
	if !tt.HasText("GLAB_CONFIG_DIR=/definitely/not/here glab auth login --hostname <host>") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestGlabSystemLoginCommandCanBeCopied(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			_, h, tt := open(t, "glab", true, Deps{})
			tt.SetLocale(locale)
			line := "glab auth login --hostname <host>"
			until(t, h, tt, "system glab login", func() bool { return tt.HasText(line) })
			if err := tt.Click(tr.For(locale).Copy); err != nil {
				t.Fatal(err)
			}
			if got := tt.Clipboard(); got != line {
				t.Fatalf("copied %q, want %q", got, line)
			}
		})
	}
}

func TestGitHubSystemLoginCommandCanBeCopied(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			w, h, tt := open(t, "glab", true, Deps{})
			tt.SetSize(900, 1000)
			tt.SetLocale(locale)
			words := tr.For(locale)
			until(t, h, tt, "GitHub login", func() bool { return tt.HasText(words.GhHint) && tt.HasText("gh auth login") })
			if got := w.label(words, w.shown()); got != "MR/PR" {
				t.Fatalf("tab title = %q, want MR/PR", got)
			}
			if err := tt.Click(words.GhCopy); err != nil {
				t.Fatal(err)
			}
			if got := tt.Clipboard(); got != "gh auth login" {
				t.Fatalf("copied %q, want gh auth login", got)
			}
			if got := h.S.Info().GlabConfig; got != "" {
				t.Fatalf("copying GitHub command changed glab directory to %q", got)
			}
		})
	}
}

func TestEnglishSettingsExplainBaseComparisonAndAuthorMatching(t *testing.T) {
	w, h, tt := open(t, "base", true, Deps{})
	tt.SetLocale("en-US")
	until(t, h, tt, "base branch explanation", func() bool { return w.haveInfo })
	base := fmt.Sprintf("Auto tries origin/master, origin/main, master, main (%d repos). Repos missing a stage skip it.", len(w.info.Repos))
	if !tt.HasText(base) {
		t.Fatalf("missing base comparison in %q", tt.Texts())
	}
	w.tab = tabMe
	tt.Frame()
	if !tt.HasText("Commits from these emails count as \"me\".") {
		t.Fatalf("missing author email matching explanation in %q", tt.Texts())
	}
}

func TestIndexPage(t *testing.T) {
	revealed := 0
	_, h, tt := open(t, "index", true, Deps{RevealCommandLog: func() { revealed++ }})
	until(t, h, tt, "the index line", func() bool { _, ok := tt.Find("刷新"); return ok && tt.HasText("个提交") })
	if !tt.HasText("查看记录") || !tt.HasText("数据位置") {
		t.Errorf("texts %q", tt.Texts())
	}
	if err := tt.Click("查看记录"); err != nil {
		t.Fatal(err)
	}
	if revealed != 1 {
		t.Errorf("command log revealed %d times", revealed)
	}
	if err := tt.Click("刷新"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "refreshed", func() bool {
		_, ok := tt.Find("已是最新")
		return ok
	})
	tt.SetLocale("en-US")
	tt.Frame()
	if _, ok := tt.Find("Up to date"); !ok {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestNoRepositoriesPage(t *testing.T) {
	// The pages that need the workspace say loading until its Info arrived.
	_, _, tt := open(t, "me", true, Deps{})
	tt.Frame()
	if !tt.HasText("正在读取…") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestReposSwitchChangesTheRequirementsRepoCount(t *testing.T) {
	root := viewtest.Fixture(t)
	for _, name := range []string{"repo-a", "repo-b"} {
		r := testutil.NewRepoAt(t, filepath.Join(root, name))
		main := r.Git("rev-parse", "--abbrev-ref", "HEAD")
		r.Git("checkout", "-q", "-b", "REQX-1001-x")
		r.Commit("REQX-1001 work", map[string]string{"req.txt": "1\n"})
		r.Git("checkout", "-q", main)
		r.Git("worktree", "add", "-q", filepath.Join(root, name+"-wt"), "REQX-1001-x")
	}
	h := viewtest.Open(t, root)
	w := New("repos", h.S, h.Update, Deps{})
	tt := ui.NewTester(w.View, 900, 640)
	tt.SetLocale("zh-Hans")
	t.Cleanup(w.Close)
	if err := h.S.Track("REQX-1001"); err != nil {
		t.Fatal(err)
	}
	repos := func() int {
		list, err := h.S.Requirements(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range list {
			if r.ID == "REQX-1001" {
				return r.Repos
			}
		}
		return 0
	}
	if repos() != 2 {
		t.Fatalf("the requirement spans %d repos before", repos())
	}
	until(t, h, tt, "the repo list", func() bool { return tt.HasText("repo-a") && tt.HasText("repo-b") })
	if err := tt.Click("包含 repo-b"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "repo-b ignored", func() bool {
		return slices.Equal(h.S.Info().Ignored, []string{"repo-b"}) && repos() == 1 && strings.Contains(strings.Join(tt.Texts(), "\n"), "1 个不包含")
	})
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "1 个不包含") {
		t.Errorf("texts %q", tt.Texts())
	}
	if !tt.HasText("repo-b") {
		t.Error("an ignored repo must stay listed to be switched back on")
	}
	if err := tt.Click("包含 repo-b"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "repo-b tracked again", func() bool { return len(h.S.Info().Ignored) == 0 && repos() == 2 })
}

func TestBranchesPageSavesTheComparisonBase(t *testing.T) {
	root := viewtest.Fixture(t)
	for _, repo := range []string{"repo-a", "repo-b"} {
		testutil.NewRepoAt(t, filepath.Join(root, repo)).Git("branch", "uat")
	}
	h := viewtest.Open(t, root)
	w := New("base", h.S, h.Update, Deps{})
	tt := ui.NewTester(w.View, 900, 640)
	tt.SetLocale("zh-Hans")
	t.Cleanup(w.Close)
	until(t, h, tt, "the page", func() bool { return tt.HasText("对比分支") })
	if tt.HasText("合入检查目标") {
		t.Fatal("independent merge check is still configurable")
	}
	flow := h.S.MergeFlow()
	if names := w.branchNames(); slices.Index(names, "uat") < 0 || len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Errorf("suggestions %q", names)
	}

	if err := tt.Click("对比分支输入框"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("origin/master")
	tt.Key(0, ui.KeyEnter)
	until(t, h, tt, "base saved on Enter", func() bool { return h.S.Info().Master == "origin/master" })

	if err := tt.Click("对比分支输入框"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("develop")
	tt.ClickAt(100, 600) // away from the fields
	until(t, h, tt, "master saved on leaving", func() bool {
		inf := h.S.Info()
		return inf.Master == "develop"
	})

	if err := h.S.SetCompareBase("trunk"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "the fields follow", func() bool { return w.master == "trunk" })

	// A suggestion taken with Enter is saved at once, the field keeping the focus.
	if err := tt.Click("对比分支输入框"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("ua")
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	until(t, h, tt, "the suggestion saved", func() bool { return h.S.Info().Master == "uat" })
	if !w.masterFocus {
		t.Error("the field lost the focus")
	}
	if !slices.Equal(h.S.MergeFlow(), flow) {
		t.Fatal("comparison base changed merge progress targets")
	}
}

func TestRepositoriesSearchKeepsTrackingControls(t *testing.T) {
	_, h, tt := open(t, "repos", true, Deps{})
	until(t, h, tt, "the repos", func() bool { return tt.HasText("repo-a") && tt.HasText("repo-b") })
	if err := tt.Click("搜索仓库"); err != nil {
		t.Fatal(err)
	}
	tt.Type("REPO-B")
	tt.Frame()
	if tt.HasText("repo-a") || !tt.HasText("repo-b") {
		t.Fatalf("search did not filter names: %q", tt.Texts())
	}
	if err := tt.Click("包含 repo-b"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "filtered repo ignored", func() bool {
		return slices.Equal(h.S.Info().Ignored, []string{"repo-b"}) && tt.HasText("未包含")
	})
	if err := tt.Click("搜索仓库"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("missing")
	tt.Frame()
	if !tt.HasText("没有匹配的仓库") {
		t.Fatalf("missing empty search state: %q", tt.Texts())
	}
}

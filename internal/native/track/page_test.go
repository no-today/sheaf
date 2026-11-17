package track

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

type harness struct {
	*viewtest.Harness
	p *Page
}

// fixture is viewtest.Fixture with a feature branch in repo-a, checked out there: "YT1 work" is on it, "second in a" came
// to master after the branch was cut.
func fixture(t *testing.T) string {
	root := viewtest.Fixture(t)
	a := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	main := a.Git("rev-parse", "--abbrev-ref", "HEAD")
	a.Git("checkout", "-q", "-b", "feat-YT1", main+"~1")
	a.Commit("YT1 work", map[string]string{"y.txt": "1\n"})
	return root
}

// open shows the requirement result containing "YT1 work".
func open(t *testing.T, locale string) *harness {
	t.Helper()
	return openAt(t, fixture(t), locale)
}

// openAt is open on an already-made workspace.
func openAt(t *testing.T, root, locale string) *harness {
	t.Helper()
	h := viewtest.Open(t, root)
	if err := h.S.SetMergeFlow([]session.MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.S.SetRequirement(`YT\d+`); err != nil {
		t.Fatal(err)
	}
	h.DockOf = Dock()
	viewtest.TrackAll(t, h.S)
	hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
	x := &harness{hr, hr.V.(*Page)}
	x.p.chooseReq(session.Requirement{ID: "YT1"})
	x.Until("the filter's rows", func() bool { return x.p.res != nil && len(x.p.rows) > 0 && x.TT.HasText("feat-YT1") })
	return x
}

func TestPageShowsTheFilterAsAStream(t *testing.T) {
	h := open(t, "zh-Hans")
	h.Until("the stream", func() bool { return h.p.stream.Loaded() > 0 })
	for _, s := range []string{"需求", "未提交", "repo-a", "feat-YT1", "y.txt", "创建 MR", "更新分支", "查看", "全部改动", "对比分支"} {
		if !h.TT.HasText(s) {
			t.Errorf("missing %q; texts %q", s, h.TT.Texts())
		}
	}
	for _, label := range []string{"已保存", "保存到筛选器", "提交信息", "日期", "在日志中查看", "对比"} {
		if _, ok := h.TT.Find(label); ok {
			t.Errorf("removed filter control %q is still present", label)
		}
	}
	if !h.H.Shown {
		t.Error("not marked painted")
	}
	if len(h.p.rows) != 1 || h.p.rows[0].Repo != "repo-a" || h.p.rows[0].Branch != "feat-YT1" || !h.p.checked[h.p.rows[0].Key] {
		t.Errorf("rows %+v, checked %v", h.p.rows, h.p.checked)
	}
	// No 在 IDEA / 终端 打开 on the page.
	for _, s := range []string{"IDEA", "终端", "合并主分支…", "放弃合并…", "解决…"} {
		if h.TT.HasText(s) {
			t.Errorf("page has %q", s)
		}
	}
}

func TestEnglish(t *testing.T) {
	h := open(t, "en-US")
	for _, s := range []string{"Requirements", "Uncommitted", "View", "Commits", "Create MR", "Update Branches", "Refresh Remotes"} {
		if !h.TT.HasText(s) {
			t.Errorf("missing %q; texts %q", s, h.TT.Texts())
		}
	}
	for _, label := range []string{"Merge main…", "Abort merge…", "Resolve…"} {
		if h.TT.HasText(label) {
			t.Errorf("page still has removed action %q", label)
		}
	}
}

func TestFailedRowsShowTheirReasonInline(t *testing.T) {
	h := open(t, "zh-Hans")
	key := h.p.rows[0].Key
	h.p.mrRun = &session.MRRun{At: 5, Rows: []session.MRRow{{Key: key, Repo: "repo-a", Outcome: "failed", Error: "glab mr create: exit status 1: fatal: no such project"}}}
	h.p.mrFor = h.p.filterKey()
	h.Until("the result", func() bool { return h.TT.HasText("✕ 失败") })
	if !h.TT.HasText("fatal: no such project") {
		t.Errorf("texts %q", h.TT.Texts())
	}
	if !h.TT.HasText("MR 已处理") {
		t.Errorf("no summary bar; texts %q", h.TT.Texts())
	}
}

func TestPickingTheCompareTargetSavesIt(t *testing.T) {
	h := open(t, "zh-Hans")
	h.p.setBase("origin/uat")
	h.Until("the new chips", func() bool { return h.TT.HasText("origin/uat") })
	if inf := h.H.S.Info(); inf.Master != "origin/uat" {
		t.Errorf("saved %q", inf.Master)
	}
	if h.p.input == nil || h.p.input.Base != "origin/uat" || h.p.input.DiffMode != session.DiffBase {
		t.Errorf("input %+v", h.p.input)
	}
	h.p.setFavs(toggleFav(h.p.favs, "origin/uat"))
	if f := h.H.S.Favs(); len(f) != 1 || f[0] != "origin/uat" {
		t.Errorf("favs %v", f)
	}
}

func TestUncommittedPage(t *testing.T) {
	h := open(t, "zh-Hans")
	// The worktree of repo-a gets an uncommitted file.
	var dir string
	for _, r := range h.H.Info().Repos {
		if r.Name == "repo-a" {
			dir = r.Worktrees[0].Path
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.H.S.RefreshDirty(context.Background(), nil, true)
	untrackForDirty(t, h)
	h.p.choose(wcID)
	h.Until("the dirty directory", func() bool { return h.p.dirty.view != nil && len(h.p.dirty.dirs()) > 0 && h.TT.HasText("scratch.txt") })
	for _, s := range []string{"刚刚", "未提交", "文件 1", "改动 1"} {
		if !h.TT.HasText(s) {
			t.Errorf("missing %q; texts %q", s, h.TT.Texts())
		}
	}
}

func TestTempIntentShowsABranchAsAnUnsavedFilter(t *testing.T) {
	h := open(t, "zh-Hans")
	branch := h.H.Info().Repos[0].Worktrees[0].Branch
	h.p.Open(view.TempIntent{Branch: branch})
	h.Until("the temporary filter", func() bool { return h.p.picked() == tmpID && h.p.res != nil && h.TT.HasText("临时") })
	if h.p.input.Cond == nil || len(h.p.input.Cond.Branches) != 1 {
		t.Errorf("input %+v", h.p.input)
	}
}

func TestTempFilterResultsAndPicksDoNotCarryOverToAnotherBranch(t *testing.T) {
	h := open(t, "zh-Hans")
	h.p.Open(view.TempIntent{Branch: "feat-YT1"})
	h.Until("filter A", func() bool { return h.p.picked() == tmpID && h.p.res != nil && len(h.p.rows) > 0 })
	h.p.mrRun = &session.MRRun{Rows: []session.MRRow{{Key: "k"}}}
	h.p.mrFor = h.p.filterKey()
	if h.p.mrResults() == nil {
		t.Fatal("results of the filter they were made for must show")
	}
	h.p.checked = map[string]bool{"x": true}
	h.p.sheet = &sheet{open: true}
	h.p.Open(view.TempIntent{Branch: "other"})
	if h.p.mrResults() != nil {
		t.Error("results of temp filter A show on temp filter B")
	}
	if h.p.sheet != nil || h.p.checked["x"] {
		t.Errorf("sheet %v / checks %v survive a filter change", h.p.sheet, h.p.checked)
	}
}

func TestSheetIsDroppedWhenAnotherFilterComesIn(t *testing.T) {
	h := open(t, "zh-Hans")
	in := *h.p.input
	h.p.sheet = &sheet{open: true, in: in, rows: []sheetRow{{key: "k"}}}
	h.p.Open(view.TempIntent{Branch: "other"})
	if h.p.sheet != nil {
		t.Error("the confirm sheet of the old filter is still open")
	}
}

// railAt is the middle of the rail's entry named name: the page's title can show the same name.
func (h *harness) railAt(t *testing.T, name string) (float32, float32) {
	t.Helper()
	h.TT.Frame()
	for _, a := range h.TT.AccessAll() {
		if b := a.Bounds; a.Name == name && b.X < railWidth && b.W >= railWidth-40 {
			return b.X + b.W/2, b.Y + b.H/2
		}
	}
	t.Fatalf("no rail entry %q: %q", name, h.TT.Texts())
	return 0, 0
}

func (h *harness) railClick(t *testing.T, name string) {
	t.Helper()
	h.TT.ClickAt(h.railAt(t, name))
}

func (h *harness) railRightClick(t *testing.T, name string) {
	t.Helper()
	h.TT.RightClickAt(h.railAt(t, name))
}

// mainClick clicks the first element named name right of the rail.
func (h *harness) mainClick(t *testing.T, name string) {
	t.Helper()
	h.TT.Frame()
	for _, a := range h.TT.AccessAll() {
		if b := a.Bounds; a.Name == name && b.X >= railWidth {
			h.TT.ClickAt(b.X+b.W/2, b.Y+b.H/2)
			h.TT.Frame()
			return
		}
	}
	t.Fatalf("no %q right of the rail: %q", name, h.TT.Texts())
}

// wordsOf is the words of the page's locale in the last frame.
func wordsOf(h *harness) *words { return h.p.w }

package track

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestDiffModeSwitchKeepsTheSelectedRequirementAndFlow(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) { checkDiffModeSwitch(t, locale) })
	}
}

func checkDiffModeSwitch(t *testing.T, locale string) {
	t.Helper()
	root := fixture(t)
	repo := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	repo.Git("branch", "release", "master")
	if err := os.WriteFile(filepath.Join(repo.Dir, "y.txt"), []byte("1\ndirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := openAt(t, root, locale)
	words := tr.For(locale)
	h.Until("baseline diff", func() bool {
		return h.p.input != nil && h.p.input.DiffMode == session.DiffBase && !h.p.filesBusy && h.p.stream.Loaded() > 0
	})
	flow := h.H.S.MergeFlow()
	for _, mode := range []struct{ label, value string }{
		{words.DiffWorktree, session.DiffWorktree},
		{stageLabel(words, flow[0]), session.DiffStage},
		{words.DiffBase, session.DiffBase},
	} {
		if err := h.TT.Click(words.DiffLabel); err != nil {
			t.Fatal(err)
		}
		if err := h.TT.Click(words.DiffLabel + " · " + mode.label); err != nil {
			t.Fatal(err)
		}
		h.Until(mode.label, func() bool {
			_, _, counted := h.p.totals()
			return h.p.input.DiffMode == mode.value && h.p.filesIn.DiffMode == mode.value &&
				!h.p.filesBusy && h.p.stream.Loaded() > 0 && counted
		})
		if h.p.shownReq() != "YT1" || !slices.Equal(h.H.S.MergeFlow(), flow) || h.H.S.Info().Master != "" {
			t.Fatal("viewing changed requirement, flow or base")
		}
		add, del, ready := h.p.totals()
		if !ready || del != 0 || add != 1 {
			t.Fatalf("%s counts: +%d -%d, ready=%v", mode.label, add, del, ready)
		}
		h.p.review(h.p.shownFiles()[0])
		opened := h.H.Wins.Diffs()
		if len(opened) == 0 || opened[len(opened)-1].Agg.DiffMode != mode.value {
			t.Fatalf("diff window lost %s mode: %+v", mode.label, opened)
		}
		h.Shot("track-diff-mode-" + mode.value + "-" + locale)
	}
	if err := h.TT.Click(words.DiffLabel); err != nil {
		t.Fatal(err)
	}
	h.TT.Key(0, ui.KeyEscape)
	if h.p.pop.kind != "" {
		t.Fatal("Escape did not close the mode picker")
	}
}

// TestDiffStagePickerFollowsTheConfiguredFlow: every merge flow step is its own entry of 查看改动, named by the flow,
// and the entries follow the flow when it is edited.
func TestDiffStagePickerFollowsTheConfiguredFlow(t *testing.T) {
	root := fixture(t)
	repo := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	repo.Git("branch", "release", "master")
	h := openAt(t, root, "zh-Hans")
	words := tr.For("zh-Hans")
	flow := h.H.S.MergeFlow()
	if err := h.TT.Click(words.DiffLabel); err != nil {
		t.Fatal(err)
	}
	for _, step := range flow {
		if _, ok := h.TT.Find(words.DiffLabel + " · " + stageLabel(words, step)); !ok {
			t.Fatalf("no entry for step %q; texts %q", step.Name, h.TT.Texts())
		}
	}
	h.Shot("track-diff-mode-stages-zh-Hans")
	if err := h.TT.Click(words.DiffLabel + " · " + stageLabel(words, flow[1])); err != nil {
		t.Fatal(err)
	}
	h.Until("selected stage", func() bool {
		return h.p.filesIn.DiffMode == session.DiffStage && h.p.filesIn.DiffStage == 1 && !h.p.filesBusy
	})
	if !h.TT.HasText(stageLabel(words, flow[1])) {
		t.Errorf("the picker does not show the stage picked; texts %q", h.TT.Texts())
	}
	if err := h.H.S.SetMergeFlow([]session.MergeStep{flow[1], flow[0]}); err != nil {
		t.Fatal(err)
	}
	h.Until("reordered stage", func() bool {
		return h.p.flow[0] == flow[1] && h.p.diffStage == 0 && h.p.filesIn.DiffStage == 0 && !h.p.filesBusy
	})
	if h.p.shownReq() != "YT1" || h.H.S.Info().Master != "" {
		t.Fatal("stage selection changed requirement or base")
	}
}

func TestDeletedRequirementBranchHasNoDiff(t *testing.T) {
	root := fixture(t)
	repo := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	repo.Git("checkout", "-q", "master")
	repo.Merge("feat-YT1", "Merge branch 'feat-YT1'")
	repo.Git("branch", "-D", "feat-YT1")
	host := viewtest.Open(t, root)
	if err := host.S.SetRequirement(`YT\d+`); err != nil {
		t.Fatal(err)
	}
	viewtest.TrackAll(t, host.S)
	host.DockOf = Dock()
	mounted := viewtest.Mount(t, host, "zh-Hans", func(host view.Host) view.View { return New(host) })
	h := &harness{mounted, mounted.V.(*Page)}
	h.p.chooseReq(session.Requirement{ID: "YT1"})
	for _, mode := range []struct{ value, label string }{
		{session.DiffBase, "全部改动"},
		{session.DiffWorktree, "未提交"},
		{session.DiffStage, "未合并"},
	} {
		if mode.value != session.DiffBase {
			// Every mode is empty here, so the picker greys them out; switch underneath it.
			h.Until("the counts of every mode", func() bool { return h.p.avail != nil })
			if !h.p.diffEmpty(mode.value, 0) {
				t.Fatalf("%s is not marked empty: %+v", mode.value, h.p.avail)
			}
			h.p.diffMode = mode.value
			h.p.apply(false)
		}
		h.Until("deleted branch "+mode.value, func() bool {
			_, _, counted := h.p.totals()
			return h.p.filesIn.DiffMode == mode.value && !h.p.filesBusy && len(h.p.rows) == 1 && counted
		})
		add, del, _ := h.p.totals()
		if row := h.p.rows[0]; row.Stage != session.StageLanded || row.Branch != "" || len(h.p.files) != 0 ||
			len(h.p.stream.Files()) != 0 || h.p.filesErr != "" || add != 0 || del != 0 || h.TT.HasText("y.txt") {
			t.Fatalf("%s still includes a deleted branch: row=%+v files=%+v counts=+%d -%d error=%q", mode.value, row, h.p.files, add, del, h.p.filesErr)
		}
	}
	h.Shot("track-deleted-branch-zh-Hans")
}

// TestEmptyDiffModesCannotBePicked: a way of viewing that shows no file is greyed out in the picker, marked 无改动,
// before anyone picks it.
func TestEmptyDiffModesCannotBePicked(t *testing.T) {
	h := open(t, "zh-Hans") // repo-a's worktree is clean: 未提交 shows nothing
	words := tr.For("zh-Hans")
	h.Until("the counts of every mode", func() bool { return h.p.avail != nil })
	if a := h.p.avail; a.Worktree != 0 || a.Base == 0 {
		t.Fatalf("avail %+v", a)
	}
	if err := h.TT.Click(words.DiffLabel); err != nil {
		t.Fatal(err)
	}
	if !h.TT.HasText(words.DiffNone) {
		t.Fatalf("the empty mode is not marked; texts %q", h.TT.Texts())
	}
	h.Shot("track-diff-mode-empty-zh-Hans")
	_ = h.TT.Click(words.DiffLabel + " · " + words.DiffWorktree)
	h.Until("a frame", func() bool { return true })
	if h.p.diffMode != session.DiffBase {
		t.Errorf("an empty mode was picked: %q", h.p.diffMode)
	}
}

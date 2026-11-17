package track

import (
	"path/filepath"
	"testing"

	"sheaf/internal/testutil"
)

// remergeFixture is fixture(t) plus a merge of master into the feature branch that a hand touched: master's a.txt came
// in clean, and the merge edits it again and adds during-merge.txt while at it — what gitcli.RemergeFiles reports as
// the merge's own files (the clean part stays hidden).
func remergeFixture(t *testing.T) string {
	t.Helper()
	root := fixture(t)
	a := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	a.Git("checkout", "-q", "master")
	a.Commit("master changes a", map[string]string{"a.txt": "master\n"})
	a.Git("checkout", "-q", "feat-YT1")
	a.Git("merge", "-q", "--no-ff", "--no-commit", "master")
	a.Commit("YT1 merge master", map[string]string{"a.txt": "resolved\n", "during-merge.txt": "made while merging\n"})
	return root
}

// remergeShots is the conflict-resolution scenario of the files and commits tabs: the merge the page hid so far shows
// as 冲突解决 once the background classification lands, and its files join the file list.
func remergeShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h := openAt(t, remergeFixture(t), locale)
		h.Until("the lists", func() bool { return h.p.res != nil && h.p.commits != nil && !h.p.filesBusy })
		h.Until("the classification", func() bool { return len(h.p.shownFiles()) == 3 && h.p.commits != nil && !h.p.filesBusy })
		h.Shot("track-remerge-files-" + locale)
		h.p.tab = 1
		h.TT.Frame()
		h.Shot("track-remerge-commits-" + locale)
	}
}

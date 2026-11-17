package track

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"

	"sheaf/internal/native/view"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestReviewDefersWorkingDirectoryChanges(t *testing.T) {
	for _, entry := range []string{"requirement", "uncommitted"} {
		t.Run(entry, func(t *testing.T) {
			root := fixture(t)
			dir := filepath.Join(root, "repo-a")
			file := filepath.Join(dir, "pending.txt")
			shotWrite(t, file, "before\n", 0o644)
			h := openAt(t, root, "zh-Hans")
			if entry == "uncommitted" {
				h.p.choose(wcID)
			}
			stream := h.p.stream
			if entry == "uncommitted" {
				stream = h.p.dirty.stream
			}
			h.Until("initial review", func() bool {
				return stream.Loaded() == len(stream.Files()) && stream.Loaded() > 0 && !h.TT.FramePending()
			})
			target := -1
			for index, file := range stream.Files() {
				if file.Path == "pending.txt" {
					target = index
				}
			}
			if target < 0 {
				t.Fatal("missing pending file")
			}
			reread, dirtySeq, requested := h.p.reread, h.p.dirty.seq, stream.Requested
			shotWrite(t, file, "after\nmore\nlatest\n", 0o644)
			h.H.S.RefreshDirty(t.Context(), []string{dir}, true)
			h.Until("update reminder", func() bool { return h.TT.HasText("内容已变化 · 刷新") && !h.TT.FramePending() })
			shotWrite(t, file, "after\nmore\nlatest\nagain\n", 0o644)
			h.H.S.RefreshDirty(t.Context(), []string{dir}, true)
			h.Until("repeated update reminder", func() bool { return h.TT.HasText("内容已变化 · 刷新") && !h.TT.FramePending() })
			if h.TT.HasText("实时") {
				t.Fatal("deferred review is still labelled live")
			}
			h.Shot("review-changed-" + entry)
			if h.p.reread != reread || h.p.dirty.seq != dirtySeq || stream.Requested != requested {
				t.Fatal("visible review reloaded automatically")
			}
			if add, _, ok := stream.Stats(target); !ok || add != 1 {
				t.Fatalf("visible diff changed before refresh: add=%d loaded=%v", add, ok)
			}
			if err := h.TT.Click("内容已变化 · 刷新"); err != nil {
				t.Fatal(err)
			}
			h.Until("manually refreshed diff", func() bool {
				add, _, ok := stream.Stats(target)
				return ok && add == 4 && !h.p.pending && !h.TT.FramePending()
			})
			if h.TT.HasText("已是最新") {
				t.Fatal("up-to-date status should be hidden")
			}
		})
	}
}

func TestReviewReminderFits(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := open(t, locale)
			h.Until("initial diff", func() bool { return h.p.stream.Loaded() > 0 && !h.TT.FramePending() })
			h.p.Event(view.IndexChanged{Update: session.IndexUpdate{ChangedRepos: []string{"repo-a"}}})
			w := tr.For(locale)
			for _, width := range []int{820, 1180, 1440} {
				h.TT.SetSize(width, 900)
				h.Until("reminder settled", func() bool { return !h.TT.FramePending() })
				h.Shot("review-reminder-" + strconv.Itoa(width) + "-" + locale)
				status, statusOK := h.TT.Find(w.ReviewChanged)
				picker, pickerOK := h.TT.Find(w.DiffLabel + " " + w.DiffBase)
				if !statusOK || !pickerOK || status.W <= 0 || picker.W <= 0 ||
					status.X+status.W > picker.X || picker.X+picker.W > float32(width) {
					t.Errorf("review controls overlap or clip at %d: status=%+v picker=%+v", width, status, picker)
				}
			}
		})
	}
}

func TestReviewIndexChangesOnlyAffectShownRepositories(t *testing.T) {
	for _, tc := range []struct {
		name        string
		uncommitted bool
		repos       []string
		wantPending bool
	}{
		{"unrelated repository", false, []string{"repo-b"}, false},
		{"shown repository", false, []string{"repo-a"}, true},
		{"mixed repositories", false, []string{"repo-b", "repo-a"}, true},
		{"empty update", false, nil, false},
		{"uncommitted ignores index", true, []string{"repo-a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			for i := range 30 {
				shotWrite(t, filepath.Join(root, "repo-a", fmt.Sprintf("pending-%02d.txt", i)), "one\ntwo\nthree\n", 0o644)
			}
			h := openAt(t, root, "zh-Hans")
			stream := h.p.stream
			if tc.uncommitted {
				h.p.choose(wcID)
				stream = h.p.dirty.stream
			}
			h.Until("initial visible files", func() bool { return stream.Loaded() > 0 && !h.TT.FramePending() })
			last := len(stream.Files()) - 1
			if _, _, loaded := stream.Stats(last); loaded {
				t.Fatal("last file must be outside the initial read range")
			}
			reread, dirtySeq := h.p.reread, h.p.dirty.seq
			h.p.Event(view.IndexChanged{Update: session.IndexUpdate{ChangedRepos: tc.repos}})
			h.Until("index update settled", func() bool { return !h.TT.FramePending() })
			if h.p.pending != tc.wantPending {
				t.Fatalf("pending = %v, want %v", h.p.pending, tc.wantPending)
			}
			if h.p.reread != reread || h.p.dirty.seq != dirtySeq {
				t.Fatal("index update reloaded the visible diff")
			}
			requested := stream.Requested
			stream.ScrollToFile(last)
			if tc.wantPending {
				h.Until("paused file reminder", func() bool { return h.TT.HasText("内容已变化 · 刷新") && !h.TT.FramePending() })
				if stream.Requested != requested {
					t.Fatal("affected review continued loading")
				}
			} else {
				h.Until("unaffected review keeps loading", func() bool {
					_, _, loaded := stream.Stats(last)
					return loaded && stream.Requested > requested && !h.TT.FramePending()
				})
				if h.TT.HasText("内容已变化 · 刷新") {
					t.Fatal("unrelated index update interrupted review")
				}
			}
			h.Shot("review-index-" + tc.name)
		})
	}
}

func TestReviewRefreshesAfterLeavingDiffTab(t *testing.T) {
	root := fixture(t)
	dir := filepath.Join(root, "repo-a")
	file := filepath.Join(dir, "pending.txt")
	shotWrite(t, file, "before\n", 0o644)
	h := openAt(t, root, "zh-Hans")
	h.Until("initial diff", func() bool { return h.p.stream.Loaded() == 2 && !h.TT.FramePending() })
	shotWrite(t, file, "after\nmore\n", 0o644)
	h.H.S.RefreshDirty(t.Context(), []string{dir}, true)
	h.Until("pending changes", func() bool { return h.TT.HasText("内容已变化 · 刷新") })
	if err := h.TT.Click("提交 1"); err != nil {
		t.Fatal(err)
	}
	h.Until("background refresh", func() bool {
		return !h.p.aggBusy && !h.p.filesBusy && !h.TT.HasText("内容已变化 · 刷新") && !h.TT.FramePending()
	})
	if err := h.TT.Click("改动 1"); err != nil {
		t.Fatal(err)
	}
	h.Until("latest diff on return", func() bool {
		for index, f := range h.p.stream.Files() {
			if f.Path == "pending.txt" {
				add, _, loaded := h.p.stream.Stats(index)
				return loaded && add == 2
			}
		}
		return false
	})
}

func TestReviewIgnoresUnrelatedWorkingDirectory(t *testing.T) {
	h := open(t, "zh-Hans")
	h.Until("initial diff", func() bool { return h.p.stream.Loaded() > 0 && !h.TT.FramePending() })
	dir := filepath.Join(h.H.S.Root, "repo-b")
	shotWrite(t, filepath.Join(dir, "unrelated.txt"), "other\n", 0o644)
	h.H.S.RefreshDirty(t.Context(), []string{dir}, true)
	h.Until("unrelated update settled", func() bool { return !h.TT.FramePending() })
	if h.TT.HasText("内容已变化 · 刷新") {
		t.Fatal("unrelated repo interrupted review")
	}
}

func TestReviewRefreshesWhenHidden(t *testing.T) {
	root := fixture(t)
	dir := filepath.Join(root, "repo-a")
	file := filepath.Join(dir, "pending.txt")
	shotWrite(t, file, "before\n", 0o644)
	h := openAt(t, root, "zh-Hans")
	h.Until("initial diff", func() bool { return h.p.stream.Loaded() == 2 && !h.TT.FramePending() })
	h.TT.SetWindowActive(false)
	shotWrite(t, file, "after\nmore\n", 0o644)
	h.H.S.RefreshDirty(t.Context(), []string{dir}, true)
	h.Until("visible inactive window holds review", func() bool { return h.TT.HasText("内容已变化 · 刷新") })
	h.p.Event(view.VisibilityChanged{Visible: false})
	h.Until("hidden refresh", func() bool { return !h.p.aggBusy && !h.p.filesBusy && !h.p.pending && !h.TT.FramePending() })
	shotWrite(t, file, "after\nmore\nlatest\n", 0o644)
	h.H.S.RefreshDirty(t.Context(), []string{dir}, true)
	h.Until("further changes update while hidden", func() bool {
		return !h.p.aggBusy && !h.p.filesBusy && !h.p.pending && !h.TT.FramePending()
	})
	h.p.Event(view.VisibilityChanged{Visible: true})
	h.Until("latest diff after returning", func() bool {
		for index, f := range h.p.stream.Files() {
			if f.Path == "pending.txt" {
				add, _, loaded := h.p.stream.Stats(index)
				return loaded && add == 3
			}
		}
		return false
	})
}

func TestEmptyUncommittedViewUpdatesWithoutManualRefresh(t *testing.T) {
	h := open(t, "zh-Hans")
	h.p.choose(wcID)
	h.Until("empty uncommitted view", func() bool { return h.TT.HasText("都已提交") })
	dir := filepath.Join(h.H.S.Root, "repo-a")
	shotWrite(t, filepath.Join(dir, "new.txt"), "new file\n", 0o644)
	h.H.S.RefreshDirty(t.Context(), []string{dir}, true)
	h.Until("new work is shown", func() bool {
		return h.TT.HasText("new.txt") && !h.p.dirty.busy && !h.TT.FramePending()
	})
}

func TestReviewReadFailureCanBeRetried(t *testing.T) {
	for _, entry := range []string{"requirement", "uncommitted"} {
		t.Run(entry, func(t *testing.T) {
			h := open(t, "zh-Hans")
			if entry == "uncommitted" {
				h.p.choose(wcID)
				h.Until("uncommitted view", func() bool { return h.p.dirty.view != nil && !h.p.dirty.busy && !h.TT.FramePending() })
				h.p.dirty.view, h.p.dirty.err = nil, "git unavailable"
			} else {
				h.Until("requirement loaded", func() bool { return h.p.settled() && !h.TT.FramePending() })
				h.p.aggErr = "git unavailable"
			}
			h.TT.Frame()
			if err := h.TT.Click("读取失败 · 重试"); err != nil {
				t.Fatal(err)
			}
			h.Until("retry succeeded", func() bool {
				if entry == "uncommitted" {
					return h.p.dirty.view != nil && h.p.dirty.err == "" && !h.p.dirty.busy
				}
				return h.p.aggErr == "" && h.p.settled()
			})
		})
	}
}

func TestReviewNoticesNewUncommittedRepository(t *testing.T) {
	root := fixture(t)
	dir := filepath.Join(root, "repo-b")
	repo := testutil.NewRepoAt(t, dir)
	repo.Git("checkout", "-q", "-b", "feat-YT1")
	h := openAt(t, root, "zh-Hans")
	h.Until("initial requirement", func() bool { return h.p.stream.Loaded() == 1 && !h.TT.FramePending() })
	shotWrite(t, filepath.Join(dir, "new.txt"), "new file\n", 0o644)
	h.H.S.RefreshDirty(t.Context(), []string{dir}, true)
	h.Until("new repo reminder", func() bool { return h.TT.HasText("内容已变化 · 刷新") })
	if err := h.TT.Click("内容已变化 · 刷新"); err != nil {
		t.Fatal(err)
	}
	h.Until("new repo appears after refresh", func() bool {
		return len(h.p.stream.Files()) == 2 && h.TT.HasText("new.txt") && !h.p.pending
	})
}

func TestReviewDefersExternalCommit(t *testing.T) {
	root := fixture(t)
	h := openAt(t, root, "zh-Hans")
	h.Until("initial diff", func() bool { return h.p.stream.Loaded() == 1 && !h.TT.FramePending() })
	reread, requested := h.p.reread, h.p.stream.Requested
	repo := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	repo.Commit("YT1 external change", map[string]string{"y.txt": "1\n2\n3\n"})
	if err := h.H.S.UpdateRepos(t.Context(), []string{"repo-a"}); err != nil {
		t.Fatal(err)
	}
	h.Until("external commit reminder", func() bool { return h.TT.HasText("内容已变化 · 刷新") && !h.TT.FramePending() })
	if h.p.reread != reread || h.p.stream.Requested != requested {
		t.Fatal("external commit replaced the visible review")
	}
	if add, _, loaded := h.p.stream.Stats(0); !loaded || add != 1 {
		t.Fatalf("old diff lost: add=%d loaded=%v", add, loaded)
	}
	if err := h.TT.Click("内容已变化 · 刷新"); err != nil {
		t.Fatal(err)
	}
	h.Until("latest committed diff", func() bool {
		add, _, loaded := h.p.stream.Stats(0)
		return loaded && add == 3 && !h.p.pending
	})
}

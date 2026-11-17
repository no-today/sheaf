package track

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestUncommittedShowsDirectoriesAndChangeTabsTogether(t *testing.T) {
	h := openAt(t, reviewFixture(t), "zh-Hans")
	untrackForDirty(t, h)
	h.p.choose(wcID)
	h.Until("uncommitted directories", func() bool { return h.p.dirty.view != nil })
	for _, text := range []string{"刚刚", "文件 2", "改动 2"} {
		if !h.TT.HasText(text) {
			t.Errorf("missing %q: %q", text, h.TT.Texts())
		}
	}
	for _, a := range h.TT.AccessAll() {
		if strings.HasPrefix(a.Name, "outline ") || a.Name == "工作目录 1" {
			t.Errorf("old navigation remains: %+v", a)
		}
	}
	shotClick(t, h, "mobile-YT1")
	h.Until("directory changes", func() bool {
		return h.p.dirty.focus != "" && h.TT.HasText("scratch.txt") && h.TT.HasText("只在 mobile-YT1")
	})
}

func openDirty(t *testing.T, locale string) *harness {
	t.Helper()
	root := reviewFixture(t)
	shotWrite(t, filepath.Join(root, "mobile", "a.txt"), "main directory edit\n", 0o644)
	api := testutil.NewRepoAt(t, filepath.Join(root, "api-YT1"))
	shotWrite(t, filepath.Join(api.Dir, "feature.txt"), "staged edit\n", 0o644)
	api.Git("add", "feature.txt")
	shotWrite(t, filepath.Join(api.Dir, "feature.txt"), "final edit\n", 0o644)
	h := openAt(t, root, locale)
	untrackForDirty(t, h)
	h.TT.SetSize(1440, 900)
	h.p.choose(wcID)
	h.Until("uncommitted stream", func() bool {
		return h.p.dirty.view != nil && h.p.dirty.stream.Loaded() > 0
	})
	return h
}

func untrackForDirty(t *testing.T, h *harness) {
	t.Helper()
	if err := h.H.S.Untrack("YT1"); err != nil {
		t.Fatal(err)
	}
	h.H.Inf = h.H.S.Info()
	h.p.loadReqs()
}

func TestUncommittedDirectoryListShowsRequirement(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := openDirty(t, locale)
			for _, a := range h.TT.AccessAll() {
				switch {
				case a.Role == ui.RoleColumnHeader && a.Bounds.Y < h.p.dirty.area.Bounds(nil).Y+h.p.dirty.topHeight:
					t.Errorf("directory list still has column headers: %+v", a)
				case a.Role == ui.RoleCheckBox:
					t.Errorf("directory list has a checkbox: %+v", a)
				case strings.Contains(a.Name, "Assign") || strings.Contains(a.Name, "归属到") || a.Name == "…":
					t.Errorf("directory assignment control remains: %+v", a)
				}
			}
			for _, dir := range h.p.dirty.dirs() {
				if dir.Requirement != "" && !h.TT.HasText(dir.Requirement) {
					t.Errorf("directory %s does not show requirement %s", dir.Path, dir.Requirement)
				}
			}
			for _, text := range h.TT.Texts() {
				if strings.ContainsAny(text, "↑↓") {
					t.Errorf("base comparison remains: %q", text)
				}
			}
		})
	}
}

func TestUnassignedStreamOmitsUncommittedBadges(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := openDirty(t, locale)
			w := wordsOf(h)
			if slices.Contains(h.TT.Texts(), w.WcTag) {
				t.Errorf("redundant uncommitted badge: %q", h.TT.Texts())
			}
			if !h.TT.HasText(w.NewFile) {
				t.Error("the untracked file lost its new-file badge")
			}
			if !h.TT.HasText(w.MainDir) || !h.TT.HasText(w.OnMain) || !h.TT.HasText(fill(w.WcFileCount, "n", 2)) {
				t.Errorf("directory header metadata missing: %q", h.TT.Texts())
			}
		})
	}
}

func TestUncommittedEmptyWhenDirectoriesAreClean(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := openWithoutFilters(t, locale)
			h.p.choose(wcID)
			h.Until("no dirty changes", func() bool { return h.p.dirty.view != nil })
			want := "都已提交"
			entry := "未提交"
			if locale == "en-US" {
				want, entry = "All committed", "Uncommitted"
			}
			for _, text := range []string{wordsOf(h).TabChanges + " 0", wordsOf(h).WcDirs + " 0", fill(wordsOf(h).WcMeta, "repos", 0, "dirs", 0, "files", 0)} {
				if h.TT.HasText(text) {
					t.Errorf("redundant empty-page metadata %q", text)
				}
			}
			for _, a := range h.TT.AccessAll() {
				if a.Role == ui.RoleTable || a.Role == ui.RoleSplitter {
					t.Errorf("empty page has table or splitter: %+v", a)
				}
			}
			if !slices.Contains(h.TT.Texts(), want) || !h.TT.HasText(entry) || h.p.dirtyDirs() != 0 {
				t.Errorf("empty unassigned page: count %d, texts %q", h.p.dirtyDirs(), h.TT.Texts())
			}
		})
	}
}

func TestUncommittedScopeCanBeCancelledAndFollowsTracking(t *testing.T) {
	h := openDirty(t, "zh-Hans")
	d := &h.p.dirty
	w := wordsOf(h)
	shotClick(t, h, "mobile-YT1")
	h.Until("one directory's two files", func() bool { return d.focus != "" && len(d.stream.Files()) == 2 })
	shotClick(t, h, w.TabFiles+" 2")
	if !h.TT.HasText(w.TabChanges+" 2") || h.TT.HasText("a.txt") {
		t.Fatalf("file tab did not follow directory scope: %q", h.TT.Texts())
	}
	shotClick(t, h, "mobile-YT1")
	h.Until("same row cancels scope", func() bool { return d.focus == "" && len(d.stream.Files()) == 4 })
	shotClick(t, h, "mobile-YT1")
	h.Until("scope selected again", func() bool { return d.focus != "" && len(d.stream.Files()) == 2 })
	h.TT.Key(0, ui.KeyEscape)
	h.Until("Escape cancels scope", func() bool { return d.focus == "" && len(d.stream.Files()) == 4 })
	shotClick(t, h, "mobile-YT1")
	h.Until("scope before cancel button", func() bool { return d.focus != "" })
	shotClick(t, h, w.Unfocus)
	h.Until("all unassigned files", func() bool { return d.focus == "" && len(d.stream.Files()) == 4 })
	shotClick(t, h, "mobile-YT1")
	focus := d.focus
	for _, tracked := range []bool{true, false} {
		var err error
		if tracked {
			err = h.H.S.Track("YT1")
		} else {
			err = h.H.S.Untrack("YT1")
		}
		if err != nil {
			t.Fatal(err)
		}
		h.H.Inf = h.H.S.Info()
		h.p.Event(view.ConfigChanged{})
		h.Until("tracking change preserves directory scope", func() bool {
			dir, ok := dirOf(d.dirs(), focus)
			return ok && (dir.Requirement == "YT1") == tracked && d.focus == focus && len(d.dirs()) == 3 && len(d.stream.Files()) == 2 && h.p.dirtyDirs() == 3
		})
	}
	shotWrite(t, filepath.Join(focus, "feature.txt"), "mobile\n", 0o644)
	if err := os.Remove(filepath.Join(focus, "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	h.H.S.RefreshDirty(t.Context(), []string{focus}, true)
	h.H.Inf = h.H.S.Info()
	d.load(h.p)
	h.Until("clean directory cancels scope", func() bool {
		return d.focus == "" && len(d.dirs()) == 2 && len(d.stream.Files()) == 2 && h.p.dirtyDirs() == 2
	})
}

func TestUncommittedStreamKeepsWorktreesSeparateAndReadsHead(t *testing.T) {
	h := openDirty(t, "zh-Hans")
	d := &h.p.dirty
	h.TT.SetSize(1440, 420)
	if len(d.dirs()) != 3 || len(d.stream.Files()) != 4 {
		t.Fatalf("directories %+v, files %+v", d.dirs(), d.stream.Files())
	}
	for index, file := range d.stream.Files() {
		d.stream.ScrollToFile(index)
		h.Until("file diff", func() bool {
			_, _, ok := d.stream.Stats(index)
			return ok && d.stream.Current() == index
		})
		add, del, _ := d.stream.Stats(index)
		wantDel := 1
		if file.Path == "scratch.txt" {
			wantDel = 0
		}
		if add != 1 || del != wantDel {
			t.Errorf("%s/%s: +%d -%d, want +1 -%d", filepath.Base(file.Group), file.Path, add, del, wantDel)
		}
		if file.Path != "scratch.txt" {
			continue
		}
		for range 2 {
			shotClick(t, h, "mobile/scratch.txt")
		}
		diffs := h.H.Wins.Diffs()
		if len(diffs) != 1 || diffs[0].Worktree != file.Group || diffs[0].Area != "untracked" {
			t.Fatalf("opened wrong untracked diff: %+v", diffs)
		}
	}
	shotClick(t, h, "改动 4")
	h.TT.SetSize(1440, 420)
	d.stream.ScrollToTop()
	h.Until("first file", func() bool { return d.stream.Current() == 0 })
	h.TT.Key(ui.Alt, ui.KeyDown)
	h.Until("next file", func() bool { return d.stream.Current() == 1 })
}

func TestUncommittedRefreshClearsAClosedDirectoryScope(t *testing.T) {
	h := openDirty(t, "zh-Hans")
	d := &h.p.dirty
	for _, dir := range d.dirs() {
		if filepath.Base(dir.Path) == "mobile" {
			d.focus = dir.Path
		}
	}
	h.Until("focused directory", func() bool { return len(d.stream.Files()) == 1 })
	if err := os.WriteFile(filepath.Join(d.focus, "a.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.H.S.RefreshDirty(t.Context(), []string{d.focus}, true)
	h.H.Inf = h.H.S.Info()
	d.load(h.p)
	h.Until("remaining dirty directories", func() bool {
		return d.focus == "" && len(d.dirs()) == 2 && len(d.stream.Files()) == 3
	})
}

// TestDiscardUncommittedFile: a file's menu, in the file list or the stream, discards its changes (删除文件 for a new
// file) only after the user confirms, and the page reads the directories again.
func TestDiscardUncommittedFile(t *testing.T) {
	h := openDirty(t, "zh-Hans")
	w := tr.For("zh-Hans")
	var feature, scratch session.AggFile
	for _, f := range h.p.dirty.view.Files {
		switch f.Path {
		case "feature.txt":
			feature = f
		case "scratch.txt":
			scratch = f
		}
	}
	if feature.Key == "" || !scratch.Untracked {
		t.Fatalf("fixture files: %+v", h.p.dirty.view.Files)
	}
	rightClick := func(name string) {
		t.Helper()
		h.TT.Frame()
		for _, a := range h.TT.AccessAll() {
			if a.Name == name && a.Bounds.X >= railWidth {
				h.TT.RightClickAt(a.Bounds.X+a.Bounds.W/2, a.Bounds.Y+a.Bounds.H/2)
				h.TT.Frame()
				return
			}
		}
		t.Fatalf("no %q: %q", name, h.TT.Texts())
	}

	// The file list: cancelling leaves the file as it is.
	before, _ := os.ReadFile(filepath.Join(feature.Key, "feature.txt"))
	h.p.dirty.tab = 1
	rightClick("file " + feature.Key + "/feature.txt")
	if err := h.TT.ChooseMenuItem(w.DiscardFile); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	if !h.TT.HasText(fill(w.DiscardTitle, "name", "feature.txt")) {
		t.Fatalf("no confirmation; texts %q", h.TT.Texts())
	}
	h.Shot("uncommitted-discard-confirm-zh-Hans")
	if err := h.TT.Click("取消"); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	if b, _ := os.ReadFile(filepath.Join(feature.Key, "feature.txt")); string(b) != string(before) {
		t.Fatalf("cancel changed the file: %q", b)
	}
	rightClick("file " + feature.Key + "/feature.txt")
	if err := h.TT.ChooseMenuItem(w.DiscardFile); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	if err := h.TT.Click(w.DiscardOK); err != nil {
		t.Fatal(err)
	}
	h.Until("feature.txt discarded", func() bool {
		return !slices.ContainsFunc(h.p.dirty.view.Files, func(f session.AggFile) bool { return f.Key == feature.Key && f.Path == "feature.txt" })
	})
	if out := testutil.NewRepoAt(t, feature.Key).Git("status", "--porcelain", "--", "feature.txt"); out != "" {
		t.Errorf("feature.txt still uncommitted: %q", out)
	}

	// The stream: a new file is deleted.
	h.p.dirty.tab = 0
	h.p.dirty.focus = scratch.Key
	h.Until("scratch.txt in the stream", func() bool { return h.TT.HasText("scratch.txt") })
	rightClick(scratch.Repo + "/scratch.txt")
	if err := h.TT.ChooseMenuItem(w.DeleteFile); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	if err := h.TT.Click(w.DeleteOK); err != nil {
		t.Fatal(err)
	}
	h.Until("scratch.txt deleted", func() bool {
		_, err := os.Lstat(filepath.Join(scratch.Key, "scratch.txt"))
		return os.IsNotExist(err) && !slices.ContainsFunc(h.p.dirty.view.Files, func(f session.AggFile) bool { return f.Key == scratch.Key && f.Path == "scratch.txt" })
	})
}

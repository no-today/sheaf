package track

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"sheaf/internal/native/diffview"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestRepoHeaderWithLongNames(t *testing.T) {
	const project = "hospital-platform-integration-service-医院平台集成服务"
	const worktree = "hospital-platform-integration-service-YT1-cross-repository-uncommitted-changes"
	const branch = "feature/YT1-hospital-platform-integration-cross-repository-uncommitted-changes"
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			root := fixture(t)
			repo := testutil.NewRepo(t, filepath.Join(root, project))
			repo.Commit("init", map[string]string{"base.txt": "base\n"})
			dir := filepath.Join(root, "worktrees", worktree)
			repo.Git("worktree", "add", "-q", "-b", branch, dir)
			shotWrite(t, filepath.Join(dir, "pending.txt"), strings.Repeat("pending line\n", 80), 0o644)
			h := openAt(t, root, locale)
			h.Until("long-name worktree changes", func() bool {
				return !h.p.filesBusy && len(h.p.stream.Files()) == 2 && !h.TT.FramePending()
			})
			files := h.p.stream.Files()
			target := slices.IndexFunc(files, func(file diffview.StreamFile) bool {
				return file.Source.(session.AggFile).Repo == project
			})
			if target < 0 {
				t.Fatal("long-name worktree is missing")
			}
			for _, width := range []int{820, 1180, 1440} {
				t.Run(strconv.Itoa(width), func(t *testing.T) {
					h.TT.SetSize(width, 900)
					h.p.stream.ScrollToFile(target)
					h.Until("long-name header", func() bool {
						return h.p.stream.Current() == target && !h.TT.FramePending()
					})
					h.Shot("repo-long-names-" + strconv.Itoa(width) + "-" + locale)
					header, ok := h.TT.Find("stream repo " + files[target].Group)
					if !ok || header.H <= 0 {
						t.Fatalf("missing repo header: %+v", header)
					}
					row, ok := h.p.rowOf(files[target].Source.(session.AggFile).Key)
					if !ok {
						t.Fatal("missing repo row")
					}
					w := tr.For(locale)
					labels := []string{
						rowLabel(row.Key, project),
						fill(w.WorktreeSection, "location", worktree, "n", 1),
						worktree,
						branch,
						w.StageDev,
						"↑0", "↓0",
					}
					lastRight := header.X
					for _, label := range labels {
						found := false
						for _, entry := range h.TT.AccessAll() {
							b := entry.Bounds
							if entry.Name != label || b.H <= 0 || b.Y < header.Y || b.Y+b.H > header.Y+header.H {
								continue
							}
							minWidth := float32(1)
							if label == labels[0] || label == labels[1] || label == worktree || label == branch {
								minWidth = 24
							}
							if b.W < minWidth || b.X < header.X || b.X+b.W > header.X+header.W {
								t.Errorf("%q is squeezed or outside header: item=%+v header=%+v", label, b, header)
							}
							if b.X < lastRight-0.5 {
								t.Errorf("%q overlaps preceding item: %+v, previous right=%v", label, b, lastRight)
							}
							lastRight = b.X + b.W
							if (label == labels[0] || label == branch || label == worktree) && !strings.Contains(entry.Description, label) {
								t.Errorf("%q has no complete name in tooltip: %q", label, entry.Description)
							}
							found = true
							break
						}
						if !found {
							t.Errorf("%q is missing from header %+v", label, header)
						}
					}
				})
			}
		})
	}
}

func openUncommittedRepos(t *testing.T, locale string) *harness {
	t.Helper()
	root := fixture(t)
	for _, name := range []string{"repo-c", "repo-d"} {
		repo := testutil.NewRepo(t, filepath.Join(root, name))
		repo.Commit("init", map[string]string{"base.txt": "base\n"})
		repo.Git("checkout", "-q", "-b", "feat-YT1")
		shotWrite(t, filepath.Join(repo.Dir, "pending.txt"), strings.Repeat("pending line\n", 80), 0o644)
	}
	h := openAt(t, root, locale)
	h.TT.SetSize(1440, 900)
	h.Until("committed and uncommitted repos", func() bool {
		return !h.p.filesBusy && len(h.p.stream.Files()) == 3 && h.p.stream.Current() >= 0 && !h.TT.FramePending()
	})
	return h
}

func TestUncommittedRepoRailJumpsToPreview(t *testing.T) {
	for _, mode := range []string{session.DiffBase, session.DiffWorktree} {
		t.Run(mode, func(t *testing.T) {
			h := openUncommittedRepos(t, "zh-Hans")
			if mode != session.DiffBase {
				h.p.diffMode = mode
				h.p.apply(true)
				h.Until("uncommitted mode", func() bool {
					return !h.p.filesBusy && h.p.filesIn.DiffMode == mode && h.p.stream.Current() >= 0 && !h.TT.FramePending()
				})
			}
			t.Cleanup(func() { h.Shot("uncommitted-repo-jump-" + mode) })
			for _, repo := range []string{"repo-d", "repo-c"} {
				if err := h.TT.Click(repo + " 开发中"); err != nil {
					t.Fatal(err)
				}
				h.Until("preview positioned at "+repo, func() bool {
					current := h.p.stream.Current()
					return current >= 0 && h.p.stream.Files()[current].Source.(session.AggFile).Repo == repo && !h.TT.FramePending()
				})
			}
		})
	}
}

func TestUncommittedRepoHeaderStaysPinned(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := openUncommittedRepos(t, locale)
			files := h.p.stream.Files()
			target := slices.IndexFunc(files, func(file diffview.StreamFile) bool {
				return file.Source.(session.AggFile).Repo == "repo-c"
			})
			for _, width := range []int{1440, 1180, 820} {
				h.TT.SetSize(width, 900)
				h.p.stream.ScrollToFile(target)
				h.Until("uncommitted file loaded", func() bool {
					_, _, loaded := h.p.stream.Stats(target)
					return loaded && h.p.stream.Current() == target && !h.TT.FramePending()
				})
				h.TT.Scroll(600, 300, 0, 450)
				h.Until("uncommitted file scrolled", func() bool { return !h.TT.FramePending() })
				h.Shot("uncommitted-repo-pinned-" + itoa(width) + "-" + locale)
				header, ok := h.TT.Find("stream repo " + files[target].Group)
				if !ok || header.Y < 0 || header.H <= 0 {
					t.Fatalf("repo header is not pinned: %+v, %v", header, ok)
				}
				w := tr.For(locale)
				markerInHeader := false
				for _, entry := range h.TT.AccessAll() {
					if entry.Name == fill(w.WorktreeSection, "location", w.MainDir, "n", 1) &&
						entry.Bounds.H > 0 && entry.Bounds.Y >= header.Y && entry.Bounds.Y+entry.Bounds.H <= header.Y+header.H {
						markerInHeader = true
					}
				}
				if !markerInHeader {
					t.Fatalf("uncommitted marker is not in the repo header: %+v", header)
				}
				file, ok := h.TT.Find("repo-c/pending.txt")
				if !ok || file.Y < header.Y+header.H-0.5 {
					t.Fatalf("file header overlaps repo header: repo=%+v file=%+v", header, file)
				}
			}
		})
	}
}

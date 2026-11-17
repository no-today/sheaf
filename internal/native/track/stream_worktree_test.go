package track

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sheaf/internal/session"
)

func TestRequirementStreamSeparatesWorktreeSections(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			root := fixture(t)
			for path, text := range map[string]string{"y.txt": "1\ndirty\nmore\n", "scratch.txt": "scratch\n"} {
				if err := os.WriteFile(filepath.Join(root, "repo-a", path), []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
			}
			h := openAt(t, root, locale)
			h.TT.SetSize(1440, 900)
			p, w := h.p, tr.For(locale)
			h.Until("separate working directory files", func() bool { return !p.filesBusy && len(p.stream.Files()) == 3 && p.stream.Loaded() == 3 })
			fs := p.stream.Files()
			if fs[0].Source.(session.AggFile).Worktree || !fs[1].Source.(session.AggFile).Worktree || fs[1].Group != fs[0].Group+"\x00worktree" || fs[2].Group != fs[1].Group {
				t.Fatalf("groups: %+v", fs)
			}
			if fs[0].Key == fs[2].Key {
				t.Fatal("committed and worktree file share a cache key")
			}
			head := fill(w.WorktreeSection, "location", w.MainDir, "n", 2)
			if _, ok := h.TT.Find(head); !ok || !h.TT.HasText(w.DiffWorktree) || !h.TT.HasText(w.TabChanges+" 1") {
				t.Fatalf("missing section, outline or tab: %q", h.TT.Texts())
			}
			header, ok := h.TT.Find("stream repo " + fs[1].Group)
			marker, marked := h.TT.Find(head)
			if !ok || !marked || marker.Y < header.Y || marker.Y+marker.H > header.Y+header.H {
				t.Fatalf("worktree marker is outside repo header: header=%+v marker=%+v", header, marker)
			}
			if slices.Contains(h.TT.Texts(), w.WcTag) {
				t.Fatalf("redundant uncommitted badge: %q", h.TT.Texts())
			}
			add, del, ready := p.totals()
			if !ready || add != 1 || del != 0 {
				t.Fatalf("totals include worktree: +%d -%d %v", add, del, ready)
			}
			for _, f := range fs {
				af := f.Source.(session.AggFile)
				if af.Worktree {
					p.review(af)
					opened := h.H.Wins.Diffs()
					src := opened[len(opened)-1]
					if src.Kind != "agg" || src.Agg.DiffMode != session.DiffWorktree || !src.Only || src.Path != af.Path {
						t.Fatalf("wrong source: %+v", src)
					}
					break
				}
			}
			if err := h.TT.Click("outline " + fs[2].Group + "/y.txt"); err != nil {
				t.Fatal(err)
			}
			h.Until("worktree outline settled", func() bool { return !h.TT.FramePending() })
			if add, del, ok := p.fileStats(2); !ok || add != 2 || del != 0 {
				t.Fatalf("worktree reused committed line counts: +%d -%d %v", add, del, ok)
			}
			p.stream.ScrollToTop()
			h.Until("top", func() bool { return p.stream.Current() == 0 })
			h.TT.Frame()
			if parent, ok := h.TT.Access("stream repo " + fs[0].Group); !ok || parent.Bounds.H == 0 {
				t.Fatal("missing parent repo header")
			}
			for _, width := range []int{820, 1180, 1440} {
				h.TT.SetSize(width, 900)
				h.TT.Frame()
				if _, ok := h.TT.Find(head); !ok {
					t.Fatalf("section missing at %d: %q", width, h.TT.Texts())
				}
			}
		})
	}
}

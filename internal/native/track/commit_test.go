package track

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestResolvedCommitOpensOnlyItsRemergeDiff(t *testing.T) {
	root := remergeFixture(t)
	r := testutil.NewRepoAt(t, filepath.Join(root, "repo-a"))
	hash := r.Git("rev-parse", "HEAD")
	r.Commit("YT1 later work", map[string]string{"later.txt": "later\n"})
	if err := os.WriteFile(filepath.Join(r.Dir, "a.txt"), []byte("pending\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := openAt(t, root, "zh-Hans")
	h.p.tab = 1
	h.Until("the resolved commit", func() bool {
		if h.p.commits == nil {
			return false
		}
		for i := range h.p.commits.Total {
			if row, ok := h.p.commitRow(h.p.commits, i); ok && row.Hash == hash && row.Resolved {
				return h.TT.HasText("YT1 merge master")
			}
		}
		return false
	})
	for range 2 {
		if err := h.TT.Click("YT1 merge master"); err != nil {
			t.Fatal(err)
		}
	}
	h.Until("the commit diff window", func() bool { return len(h.H.Wins.Diffs()) == 1 })
	src := h.H.Wins.Diffs()[0]
	if src.Kind != "agg" || src.Hash != hash || src.Agg == nil || len(h.H.Went) != 0 {
		t.Fatalf("resolved source: %+v, navigation %+v", src, h.H.Went)
	}
	ctx := context.Background()
	files, err := h.H.S.TagAggFiles(ctx, *src.Agg)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("the commit diff includes branch or worktree files: %+v", files)
	}
	for _, f := range files {
		if f.Uncommitted || f.Path != "a.txt" && f.Path != "during-merge.txt" {
			t.Fatalf("unrelated file: %+v", f)
		}
		patch, err := h.H.S.TagAggPatch(ctx, *src.Agg, src.Row, f.Path, "", false, f.Worktree, false)
		if err != nil || patch.Mode != "commits" || len(patch.Parts) != 1 || patch.Parts[0].Hash != hash {
			t.Fatalf("not the selected commit's remerge diff: %+v, %v", patch, err)
		}
		if f.Path == "a.txt" {
			var old, added []string
			for _, hunk := range patch.Parts[0].Patch.Hunks {
				for _, line := range hunk.Lines {
					switch line.Kind {
					case "-":
						old = append(old, line.Text)
					case "+":
						added = append(added, line.Text)
					}
				}
			}
			if strings.TrimSpace(strings.Join(old, "\n")) != "master" || strings.TrimSpace(strings.Join(added, "\n")) != "resolved" {
				t.Fatalf("expected remerge master -> resolved, got %q -> %q", old, added)
			}
		}
	}
}

func TestResolvedCommitWithoutAPerCommitSourceReturnsAnError(t *testing.T) {
	h := open(t, "zh-Hans")
	_, err := resolvedCommitSource(context.Background(), h.H.S, session.Row{Repo: "repo-a", Hash: "missing", Resolved: true}, []string{"master"})
	if err == nil {
		t.Fatal("a missing resolved commit fell back to a first-parent diff")
	}
}

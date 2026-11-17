package session

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

// Landed rows omit automatic merges from their parts, including the landing merge; edited merges keep their own diff.
func TestTagAggLandedPatchMerges(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "branch kept", true: "branch deleted"}[deleted], func(t *testing.T) {
			testutil.Isolate(t)
			ctx := context.Background()
			root := t.TempDir()
			r := testutil.NewRepo(t, filepath.Join(root, "repo"))
			base := "feature: old\n1\n2\n3\n4\n5\n6\n7\n8\nbase: old\n"
			r.Commit("init", map[string]string{"a.txt": base, "main.txt": "base\n"})
			r.Git("checkout", "-q", "-b", "feat-YT5")
			feature := strings.Replace(base, "feature: old", "feature: new", 1)
			own := r.Commit("YT5 feature", map[string]string{"a.txt": feature})
			r.Git("checkout", "-q", "master")
			r.Commit("base update", map[string]string{"a.txt": strings.Replace(base, "base: old", "base: new", 1)})
			r.Git("checkout", "-q", "feat-YT5")
			r.Merge("master", "YT5 sync master (clean)")
			r.Git("checkout", "-q", "master")
			r.Commit("base again", map[string]string{"main.txt": "base again\n"})
			r.Git("checkout", "-q", "feat-YT5")
			r.Git("merge", "-q", "--no-ff", "--no-commit", "master")
			resolved := strings.Replace(strings.Replace(feature, "feature: new", "feature: resolved", 1), "base: old", "base: new", 1)
			edited := r.Commit("YT5 sync master (edited)", map[string]string{"a.txt": resolved, "extra.txt": "while merging\n"})
			r.Git("checkout", "-q", "master")
			r.Merge("feat-YT5", "Merge branch 'feat-YT5'")
			cond := &query.Conditions{Q: "YT5", Authors: []string{}}
			if deleted {
				r.Git("branch", "-D", "feat-YT5")
			} else {
				cond.Branches = []string{"feat-YT5"}
			}
			s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			in := TagAggInput{Cond: cond, Base: "master"}
			res, err := s.TagAgg(ctx, in)
			if err != nil || len(res.Repos) != 1 || res.Repos[0].BranchFrom != FromMerged {
				t.Fatalf("landed overview: %+v, %v", res, err)
			}
			if changed, err := s.TagAggRemerge(ctx, in); err != nil || !changed {
				t.Fatalf("classify landed merges: %v, %v", changed, err)
			}
			patch, err := s.TagAggPatch(ctx, in, "repo", "a.txt", "", false, false, false)
			if err != nil || patch.Mode != "commits" || len(patch.Parts) != 2 || patch.Parts[0].Hash != own || patch.Parts[1].Hash != edited || patch.More != 0 {
				t.Fatalf("only the feature and edited merge should be parts: %+v, %v", patch, err)
			}
			if !hasLine(patch.Parts[1].Patch, "+", "feature: resolved") || hasLine(patch.Parts[1].Patch, "+", "base: new") {
				t.Errorf("edited merge must use its remerge diff: %+v", patch.Parts[1].Patch)
			}
			patch, err = s.TagAggPatch(ctx, in, "repo", "extra.txt", "", false, false, false)
			if err != nil || len(patch.Parts) != 1 || patch.Parts[0].Hash != edited || !hasLine(patch.Parts[0].Patch, "+", "while merging") {
				t.Errorf("file added while merging: %+v, %v", patch, err)
			}
		})
	}
}

// A branch that merged master twice: the first merge was automatic, the second was edited while merging. The page reads the
// overview first; TagAggRemerge classifies, and only then does the edited merge show in the commits and its files in the list.
func TestTagAggRemerge(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "w")
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	r.Git("branch", "-M", "master")
	r.Git("checkout", "-q", "-b", "feat")
	r.Commit("YT5 feat", map[string]string{"a.txt": "feat\n"})
	r.Git("checkout", "-q", "master")
	r.Commit("main one", map[string]string{"b.txt": "main one\n"})
	r.Git("checkout", "-q", "feat")
	r.Merge("master", "YT5 merge master (clean)")
	r.Git("checkout", "-q", "master")
	r.Commit("main two", map[string]string{"b.txt": "main two\n"})
	r.Git("checkout", "-q", "feat")
	r.Git("merge", "-q", "--no-ff", "--no-commit", "master")
	edited := r.Commit("YT5 merge master (edited)", map[string]string{"a.txt": "resolved\n", "extra.txt": "while merging\n"})

	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBases(map[string]string{"r": "master"}); err != nil {
		t.Fatal(err)
	}
	in := TagAggInput{Branches: map[string]string{"r": "feat"}, Cond: &query.Conditions{Q: "YT5", Authors: []string{}}}
	paths := func() []string {
		files, err := s.TagAggFiles(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, f := range files {
			out = append(out, f.Path)
		}
		return out
	}
	commits := func(merges bool) AggCommits {
		c, err := s.TagAggCommits(in, "", merges)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	if got := paths(); !slices.Equal(got, []string{"a.txt"}) {
		t.Errorf("before classification: files %v, want only a.txt", got)
	}
	if c := commits(false); c.Total != 1 || c.Merges != 2 {
		t.Errorf("before classification: %+v, want 1 commit and 2 hidden merges", c)
	}

	changed, err := s.TagAggRemerge(ctx, in)
	if err != nil || !changed {
		t.Fatalf("classification: changed %v, %v", changed, err)
	}
	if changed, err := s.TagAggRemerge(ctx, in); err != nil || changed {
		t.Errorf("second classification: changed %v, %v; the cache should leave nothing to find", changed, err)
	}
	res, err := s.TagAgg(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(); !slices.Equal(got, []string{"a.txt", "extra.txt"}) || res.Repos[0].Files != 2 {
		t.Errorf("files %v, count %d; want a.txt and extra.txt", got, res.Repos[0].Files)
	}
	c := commits(false)
	if c.Total != 2 || c.Merges != 1 {
		t.Errorf("commits: %+v, want the edited merge shown and the clean one hidden", c)
	}
	rows, err := s.Rows(c.ResultID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if want := row.Hash == edited; row.Resolved != want {
			t.Errorf("%s: Resolved %v, want %v", row.Subject, row.Resolved, want)
		}
	}
	if all := commits(true); all.Total != 3 || all.Merges != 0 {
		t.Errorf("merges shown: %+v, want all 3", all)
	}

	p, err := s.TagAggPatch(ctx, in, res.Repos[0].Key, "a.txt", "", false, false, false)
	if err != nil || len(p.Parts) != 2 || p.Parts[1].Hash != edited {
		t.Fatalf("a.txt: %+v %v; want the range diff and the edited merge's part", p, err)
	}
	if !hasLine(p.Parts[1].Patch, "+", "resolved") {
		t.Errorf("a.txt: the merge's part is not its own edit: %+v", p.Parts[1].Patch)
	}
	p, err = s.TagAggPatch(ctx, in, res.Repos[0].Key, "extra.txt", "", false, false, false)
	if err != nil || len(p.Parts) != 2 || !hasLine(p.Parts[1].Patch, "+", "while merging") {
		t.Errorf("extra.txt: %+v %v", p, err)
	}
}

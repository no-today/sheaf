package gitcli

import (
	"context"
	"path/filepath"
	"testing"

	"sheaf/internal/testutil"
)

func TestPatchIDsAndCandidates(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	r.Git("checkout", "-q", "-b", "feat")
	orig := r.Commit("change", map[string]string{"b.txt": "b\n"})
	r.Git("checkout", "-q", "master")
	r.Commit("other", map[string]string{"c.txt": "c\n"})
	pick := r.Git("cherry-pick", orig)
	_ = pick
	head := r.Git("rev-parse", "HEAD")
	merge := r.Merge("feat", "Merge feat")

	o := func(s string) Oid {
		x, err := ParseOid(s)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	ids, err := PatchIDs(ctx, r.Dir, []Oid{o(orig), o(head), o(merge)})
	if err != nil {
		t.Fatal(err)
	}
	if ids[o(orig)] == "" || ids[o(orig)] != ids[o(head)] {
		t.Errorf("a cherry-pick keeps the patch-id: %v", ids)
	}
	if _, ok := ids[o(merge)]; ok {
		t.Errorf("a merge has no patch-id: %v", ids)
	}
	cand, err := LineCandidates(ctx, r.Dir, o(merge), 0, []string{"b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cand) != 2 {
		t.Errorf("commits that touched b.txt: %v", cand)
	}
}

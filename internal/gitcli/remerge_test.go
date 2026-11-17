package gitcli

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"sheaf/internal/testutil"
)

func TestRemergeFiles(t *testing.T) {
	testutil.Isolate(t)
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	r.Commit("init", map[string]string{"a.txt": "one\n", "b.txt": "one\n"})
	r.Git("switch", "-q", "-c", "feat")
	r.Commit("feat", map[string]string{"a.txt": "feat\n"})
	r.Git("switch", "-q", "master")
	r.Commit("main 1", map[string]string{"b.txt": "two\n"})
	r.Git("switch", "-q", "feat")
	clean := oid(t, r.Merge("master", "Merge branch 'master' into feat"))
	r.Git("switch", "-q", "master")
	r.Commit("main 2", map[string]string{"b.txt": "three\n"})
	r.Git("switch", "-q", "feat")
	r.Git("merge", "-q", "--no-ff", "--no-commit", "master")
	edited := oid(t, r.Commit("Merge branch 'master' into feat", map[string]string{"a.txt": "feat, fixed while merging\n", "new file.txt": "x\n"}))

	got, err := RemergeFiles(context.Background(), r.Dir, []Oid{clean, edited})
	if err != nil {
		t.Fatal(err)
	}
	if change, ok := got[clean]; !ok || len(change.Files) != 0 || change.Own {
		t.Errorf("clean merge: changes %+v, listed %v", change, ok)
	}
	if want := []string{"a.txt", "new file.txt"}; !slices.Equal(got[edited].Files, want) || !got[edited].Own {
		t.Errorf("edited merge: changes %+v, want files %q and own edits", got[edited], want)
	}
	p, err := RemergePatch(context.Background(), r.Dir, edited, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hunks) != 1 {
		t.Errorf("edited merge: %d hunks in a.txt, want 1", len(p.Hunks))
	}
	if none, err := RemergeFiles(context.Background(), r.Dir, nil); err != nil || len(none) != 0 {
		t.Errorf("no merges: %v, %v", none, err)
	}
}

func oid(t *testing.T, s string) Oid {
	t.Helper()
	o, err := ParseOid(s)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

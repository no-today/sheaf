package gitview

import (
	"context"
	"path/filepath"
	"testing"

	"sheaf/internal/discover"
	"sheaf/internal/index"
	"sheaf/internal/testutil"
)

func TestFilesPatchMessage(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "r"))
	r.Commit("init", map[string]string{"a.txt": "1\n2\n"})
	r.Commit("YT03374 second\n\nbody line", map[string]string{"a.txt": "1\n2\n3\n", "b.txt": "b\n"})
	r.Commit("third", map[string]string{"a.txt": "1\n3\n"})
	repos, _ := discover.Scan(ctx, root)
	ix, err := index.Build(ctx, repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := ix.Repos[0]
	head, _ := repo.Ref("master")
	second := repo.Parents(head)[0]

	files, err := CommitFiles(ctx, ix, repo, second)
	if err != nil || len(files) != 2 || files[0].Path != "a.txt" || files[0].Add != 1 || files[0].Status != "M" {
		t.Fatalf("files: %+v %v", files, err)
	}
	u, _ := UnionFiles(ctx, ix, []index.CommitRef{{Repo: 0, I: head}, {Repo: 0, I: second}})
	if len(u) != 2 || u[0].Path != "a.txt" || u[0].Commits != 2 || u[0].Add != 1 || u[0].Del != 1 {
		t.Fatalf("union: %+v", u)
	}
	// Latest is the newest commit that changed the file, whatever the pick order.
	u, _ = UnionFiles(ctx, ix, []index.CommitRef{{Repo: 0, I: second}, {Repo: 0, I: head}})
	if u[0].Latest != repo.Oid(head).String() || u[1].Path != "b.txt" || u[1].Latest != repo.Oid(second).String() {
		t.Errorf("latest: %+v", u)
	}
	p, err := Patch(ctx, repo, second, "a.txt", "")
	if err != nil || len(p.Hunks) != 1 {
		t.Fatalf("patch: %+v %v", p, err)
	}
	subj, body, _ := Message(ctx, repo, second)
	if subj != "YT03374 second" || body != "body line" {
		t.Errorf("message %q / %q", subj, body)
	}
}

func TestUnionFilesRepos(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	a := testutil.NewRepo(t, filepath.Join(root, "a"))
	a.Commit("a1", map[string]string{"x.txt": "1\n", "z.txt": "z\n"})
	b := testutil.NewRepo(t, filepath.Join(root, "b"))
	b.Commit("b1", map[string]string{"x.txt": "1\n2\n"})
	repos, _ := discover.Scan(ctx, root)
	ix, err := index.Build(ctx, repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	var refs []index.CommitRef
	for id, r := range ix.Repos {
		head, _ := r.Ref("master")
		refs = append(refs, index.CommitRef{Repo: index.RepoID(id), I: head})
	}
	u, err := UnionFiles(ctx, ix, refs)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range u {
		got = append(got, f.Repo+":"+f.Path)
		if f.Commits != 1 {
			t.Errorf("%s:%s commits = %d; same path in two repos must not merge", f.Repo, f.Path, f.Commits)
		}
	}
	if len(got) != 3 || got[0] != "a:x.txt" || got[1] != "a:z.txt" || got[2] != "b:x.txt" { // grouped by repo, then path
		t.Errorf("union order: %v", got)
	}
	if u, err := UnionFiles(ctx, ix, nil); err != nil || len(u) != 0 {
		t.Errorf("empty: %v %v", u, err)
	}
}

package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"sheaf/internal/index"
	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

// Changing the ignore list never scans the workspace (a repo created after the first Refresh stays unseen) and reads git only
// for a repo that comes back.
func TestSetIgnoredWithoutScan(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	a := testutil.NewRepo(t, filepath.Join(root, "repo-a"))
	a.Commit("in a", map[string]string{"a": "1\n"})
	inner := testutil.NewRepo(t, filepath.Join(root, "repo-a", "inner")) // goes with repo-a
	inner.Commit("in inner", map[string]string{"i": "1\n"})
	b := testutil.NewRepo(t, filepath.Join(root, "repo-b"))
	b.Commit("in b", map[string]string{"b": "1\n"})
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	c := testutil.NewRepo(t, filepath.Join(root, "repo-c")) // a scan would find it
	c.Commit("in c", map[string]string{"c": "1\n"})

	var pushed int
	s.opts.OnIndex = func(IndexUpdate) { pushed++ }
	if err := s.SetIgnored([]string{"repo-a"}); err != nil {
		t.Fatal(err)
	}
	if pushed != 1 {
		t.Errorf("index pushes = %d, want 1", pushed)
	}
	if len(s.repos) != 1 || s.repos[0].Name != "repo-b" {
		t.Fatalf("repos = %+v, want only repo-b (no rescan)", s.repos)
	}
	if got := s.Info().State.Commits; got != 1 {
		t.Errorf("commits = %d, want 1", got)
	}
	if w := s.Info().IgnoredWith; !reflect.DeepEqual(w, map[string][]string{"repo-a": {"repo-a/inner"}}) {
		t.Errorf("IgnoredWith = %#v", w)
	}
	qr, err := s.Query(query.Default())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Rows(qr.ID, 0, 100)
	if err != nil || len(rows) != 1 || rows[0].Repo != "repo-b" {
		t.Fatalf("rows = %+v, %v", rows, err)
	}

	// tracked again: read from the last scan, at once
	if err := s.SetIgnored(nil); err != nil {
		t.Fatal(err)
	}
	if pushed != 2 || len(s.repos) != 3 || s.Info().State.Commits != 3 || len(s.Info().IgnoredWith) != 0 {
		t.Errorf("after restore: pushes=%d repos=%d commits=%d, want 2, 3, 3 and no IgnoredWith (repo-c unseen)", pushed, len(s.repos), s.Info().State.Commits)
	}
	rows, err = s.Rows(mustQuery(t, s, query.Default()).ID, 0, 100)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows after restore = %+v, %v", rows, err)
	}
}

// Two index writes at once share one temporary file; written one after the other, the snapshot always loads.
func TestSaveIndexConcurrent(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	a := testutil.NewRepo(t, filepath.Join(root, "repo-a"))
	a.Commit("in a", map[string]string{"a": "1\n"})
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	ix := s.eng.Index()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.mu.Lock()
			s.indexGen++ // as an index update does
			s.mu.Unlock()
			s.saveIndex()
		}()
	}
	wg.Wait()
	got, err := index.Load(s.indexPath)
	if err != nil || got.Commits() != ix.Commits() {
		t.Fatalf("load: %v, %v", got, err)
	}
	// nothing changed since the last write: the file is left alone
	os.Remove(s.indexPath)
	s.saveIndex()
	if _, err := os.Stat(s.indexPath); err == nil {
		t.Error("an unchanged index was written again")
	}
}

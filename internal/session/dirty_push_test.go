package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

// Counts must reach the views while a large refresh is still running, not only once every worktree was read.
func TestDirtyPushBatches(t *testing.T) {
	clock := time.Unix(1000, 0)
	var got []WorktreeStatus
	b := &dirtyPush{ctx: context.Background(), every: 200 * time.Millisecond, now: func() time.Time { return clock },
		push: func(ws WorktreeStatus) { got = append(got, ws) }}

	b.add("a", false) // first result: at once
	clock = clock.Add(50 * time.Millisecond)
	b.add("b", true) // too soon: held
	b.add("c", false)
	clock = clock.Add(200 * time.Millisecond)
	b.add("d", false) // interval passed: b, c and d together
	clock = clock.Add(10 * time.Millisecond)
	b.add("e", true)
	b.flush() // end of the refresh: the rest
	b.flush() // nothing left: no empty push

	want := []WorktreeStatus{
		{Read: []string{"a"}},
		{Worktrees: []string{"b"}, Read: []string{"c", "d"}},
		{Worktrees: []string{"e"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pushes:\n got %+v\nwant %+v", got, want)
	}
}

func TestDirtyPushCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pushed := 0
	b := &dirtyPush{ctx: ctx, every: time.Second, now: time.Now, push: func(WorktreeStatus) { pushed++ }}
	cancel()
	b.add("a", true)
	b.flush()
	if pushed != 0 {
		t.Errorf("a cancelled refresh pushed %d times", pushed)
	}
}

// Opening a workspace scans it again, which takes seconds on a big one: the uncommitted counts of the last scan's
// worktrees must not wait for it.
func TestDirtyBeforeScan(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	r := testutil.NewRepo(t, filepath.Join(root, "app"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	data, cache := t.TempDir(), t.TempDir()
	s, err := Open(ctx, root, Options{DataDir: data, CacheDir: cache, NoAutoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil { // keeps the scan for the next open
		t.Fatal(err)
	}
	s.Close()
	os.WriteFile(filepath.Join(root, "app", "a.txt"), []byte("b\n"), 0o644)

	scanning := make(chan struct{}) // the open's scan waits here until the test is done
	var once sync.Once
	pushed := make(chan WorktreeStatus, 8)
	s, err = Open(ctx, root, Options{DataDir: data, CacheDir: cache,
		OnState: func(st State) {
			if st.Indexing {
				once.Do(func() { <-scanning })
			}
		},
		OnWorktrees: func(ws WorktreeStatus) { pushed <- ws }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	defer close(scanning)
	select {
	case ws := <-pushed:
		if len(ws.Read) != 1 {
			t.Errorf("push: %+v", ws)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no uncommitted count before the scan ended")
	}
	if n := s.dirty.get(filepath.Join(root, "app")); n != 1 {
		t.Errorf("count: %d", n)
	}
}

// Until a requirement branch's uncommitted files are counted, its stage is being read: the commits alone would show
// "awaiting master" for a requirement that is still in development.
func TestStageIsReadingUntilUncommittedCounted(t *testing.T) {
	testutil.Isolate(t)
	ctx := t.Context()
	root := filepath.Join(t.TempDir(), "ws")
	dirty := testutil.NewRepo(t, filepath.Join(root, "dirty"))
	dirty.Commit("init", map[string]string{"a.txt": "a\n"})
	dirty.Git("checkout", "-q", "-b", "feat-YT1")
	dirty.Commit("YT1 work", map[string]string{"a.txt": "b\n"})
	os.WriteFile(filepath.Join(dirty.Dir, "a.txt"), []byte("wip\n"), 0o644)
	clean := testutil.NewRepo(t, filepath.Join(root, "clean"))
	clean.Commit("init", map[string]string{"c.txt": "c\n"})
	clean.Git("checkout", "-q", "-b", "feat-YT2")
	clean.Commit("YT2 work", map[string]string{"c.txt": "d\n"})

	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"YT1", "YT2"} {
		if err := s.Track(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetRequirement(`YT\d+`); err != nil {
		t.Fatal(err)
	}
	stages := func() (req map[string]string, row map[string]string) {
		t.Helper()
		list, err := s.Requirements(ctx)
		if err != nil {
			t.Fatal(err)
		}
		req, row = map[string]string{}, map[string]string{}
		for _, r := range list {
			req[r.ID] = r.Stage
			res, err := s.TagAgg(ctx, TagAggInput{Cond: &query.Conditions{Q: r.ID, Authors: []string{}}, Requirement: r.ID})
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range res.Repos {
				row[r.ID] = a.Stage
			}
		}
		return req, row
	}
	req, row := stages()
	want := map[string]string{"YT1": StageReading, "YT2": StageReading}
	if !reflect.DeepEqual(req, want) || !reflect.DeepEqual(row, want) {
		t.Errorf("before the counts: requirements %v, rows %v", req, row)
	}
	s.RefreshDirty(ctx, nil, false)
	req, row = stages()
	want = map[string]string{"YT1": StageDev, "YT2": MergeStage(0)}
	if !reflect.DeepEqual(req, want) || !reflect.DeepEqual(row, want) {
		t.Errorf("after the counts: requirements %v, rows %v", req, row)
	}
}

// A failed read must not leave a directory unknown for good, nor overwrite a count another run stored meanwhile.
func TestSettleUnread(t *testing.T) {
	var d dirtyTracker
	if !d.settleUnread("a", d.ticket()) || d.get("a") != 0 {
		t.Errorf("unknown after a failed read: %d", d.get("a"))
	}
	g := d.ticket()
	d.store("b", d.ticket(), 3, [32]byte{1})
	if d.settleUnread("b", g) || d.get("b") != 3 {
		t.Errorf("a failed read overwrote a count: %d", d.get("b"))
	}
}

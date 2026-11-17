package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"sheaf/internal/index"
	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestReuseScan(t *testing.T) {
	for _, change := range []string{"removed repo", "new worktree", "missing cache", "new clone", "empty scan"} {
		t.Run(change, func(t *testing.T) {
			testutil.Isolate(t)
			root := t.TempDir()
			r := testutil.NewRepo(t, filepath.Join(root, "repo"))
			r.Commit("init", map[string]string{"f": "one\n"})
			opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true, ReuseScan: true}
			s, err := Open(t.Context(), root, opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Refresh(t.Context()); err != nil {
				t.Fatal(err)
			}
			s.Close()
			want := 1
			linked := filepath.Join(t.TempDir(), "linked")
			switch change {
			case "removed repo":
				if err := os.RemoveAll(r.Dir); err != nil {
					t.Fatal(err)
				}
				want = 0
			case "new worktree":
				r.Git("worktree", "add", "-b", "PAY-142", linked)
			case "missing cache":
				if err := os.Remove(s.scanPath); err != nil {
					t.Fatal(err)
				}
				fallthrough
			case "new clone":
				testutil.NewRepo(t, filepath.Join(root, "new")).Commit("new", map[string]string{"f": "two\n"})
				if change == "missing cache" {
					want = 2
				}
			case "empty scan":
				if err := os.WriteFile(s.scanPath, []byte("[]"), 0o644); err != nil {
					t.Fatal(err)
				}
				want = 0
			}
			s, err = Open(t.Context(), root, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.Refresh(t.Context()); err != nil {
				t.Fatal(err)
			}
			info := s.Info()
			if len(info.Repos) != want {
				t.Fatalf("repos = %+v, want %d", info.Repos, want)
			}
			if change == "new worktree" {
				canonical, err := filepath.EvalSymlinks(linked)
				if err != nil || !s.worktreeKnown(canonical) {
					t.Fatalf("cached scan missed a new linked worktree: %s, %v", canonical, err)
				}
			}
		})
	}
}

func TestNoAutoRefreshDirtyOnce(t *testing.T) {
	root, r := reportFixture(t)
	linked := filepath.Join(t.TempDir(), "linked")
	r.Git("worktree", "add", "-b", "PAY-142", linked)
	linked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	s := openReport(t, root)
	if err := s.Track("PAY-142"); err != nil {
		t.Fatal(err)
	}
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	counts := map[string]int{}
	for _, event := range s.rec.Events() {
		if event.Source == "dirtyCount" {
			counts[event.Dir]++
		}
	}
	if len(counts) != 1 || counts[linked] != 1 || counts[r.Dir] != 0 {
		t.Fatalf("dirty reads: %v; want linked %s exactly once and main %s never", counts, linked, r.Dir)
	}
}

func TestNoAutoRefresh(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	testutil.NewRepo(t, filepath.Join(root, "repo")).Commit("init", map[string]string{"f": "1\n"})
	s, err := Open(t.Context(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.bg.Wait()
	if s.scanSeq != 0 || s.Info().State.Ready || len(s.Info().Repos) != 0 {
		t.Fatal("Open refreshed despite NoAutoRefresh")
	}
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if info := s.Info(); !info.State.Ready || info.State.Commits != 1 || len(info.Repos) != 1 {
		t.Fatalf("explicit refresh failed: %+v", info)
	}
	if filepath.Base(s.DataDir()) != WorkspaceID(s.Root) {
		t.Fatal("exported workspace ID disagrees with session data path")
	}
}

func TestCacheWrite(t *testing.T) {
	for _, mode := range []string{"direct", "allowed", "skipped"} {
		for _, seeded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/seeded=%v", mode, seeded), func(t *testing.T) {
				testutil.Isolate(t)
				root := t.TempDir()
				repo := testutil.NewRepo(t, filepath.Join(root, "repo"))
				repo.Commit("init", map[string]string{"f": "1\n"})
				opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true}
				var beforeIndex, beforeScan []byte
				if seeded {
					seed, err := Open(t.Context(), root, opts)
					if err != nil {
						t.Fatal(err)
					}
					if err := seed.Refresh(t.Context()); err != nil {
						seed.Close()
						t.Fatal(err)
					}
					seed.Close()
					beforeIndex, err = os.ReadFile(seed.indexPath)
					if err != nil {
						t.Fatal(err)
					}
					beforeScan, err = os.ReadFile(seed.scanPath)
					if err != nil {
						t.Fatal(err)
					}
				}
				repo.Commit("next", map[string]string{"f": "2\n"})
				allowed, calls := mode != "skipped", 0
				if mode != "direct" {
					opts.CacheWrite = func(write func()) {
						calls++
						if allowed {
							write()
						}
					}
				}
				s, err := Open(t.Context(), root, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				if seeded && (!s.Info().State.Ready || s.Info().State.Commits != 1) {
					t.Fatal("Open did not load existing cache")
				}
				if err := s.Refresh(t.Context()); err != nil {
					t.Fatal(err)
				}
				if st := s.Info().State; !st.Ready || st.Commits != 2 {
					t.Fatalf("cache policy blocked in-memory refresh: %+v", st)
				}
				if mode != "direct" && calls != 2 {
					t.Fatalf("CacheWrite calls = %d, want scan and index", calls)
				}
				if !allowed {
					for path, before := range map[string][]byte{s.indexPath: beforeIndex, s.scanPath: beforeScan} {
						got, err := os.ReadFile(path)
						if seeded {
							if err != nil || !bytes.Equal(got, before) {
								t.Fatalf("skipped cache write changed %s: %v", path, err)
							}
						} else if !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("skipped write created %s: %v", path, err)
						}
					}
					allowed = true
					if err := s.saveIndex(); err != nil {
						t.Fatal(err)
					}
				}
				ix, err := index.Load(s.indexPath)
				if err != nil {
					t.Fatal(err)
				}
				if ix.Commits() != 2 {
					t.Fatal("index write lost after skipping cache")
				}
				if mode != "skipped" {
					if repos, ok := loadScan(s.scanPath); !ok || len(repos) != 1 {
						t.Fatal("scan cache was not written")
					}
				}
			})
		}
	}
}

func TestSessionEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "yt")
	a := testutil.NewRepo(t, filepath.Join(root, "yt-med"))
	a.Commit("init", map[string]string{"README.md": "1\n"})
	a.Git("checkout", "-q", "-b", "feat-YT03374")
	a.Commit("YT03374 处方审核增加超量用药校验\n\n超量阈值读取药品字典配置。", map[string]string{"src/audit/Audit.java": "a\n"})
	a.Git("checkout", "-q", "master")
	b := testutil.NewRepo(t, filepath.Join(root, "hs-ytd"))
	b.Commit("YT03374 医生站展示超量提示", map[string]string{"web/Dialog.vue": "v\n"}, testutil.Author{Name: "Li", Email: "li@example.com"})

	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_000_000, 0).Add(24 * time.Hour) }}
	s, err := Open(ctx, root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	info := s.Info()
	if !info.State.Ready || info.State.Commits != 3 || len(info.Repos) != 2 || info.Me[0] != "me@example.com" {
		t.Fatalf("info: %+v", info)
	}

	c := query.Default()
	c.Me, c.Hours = true, 720
	c.Q = "YT03374 | 超量"
	qr, err := s.Query(c)
	if err != nil || qr.Error != nil {
		t.Fatal(err, qr.Error)
	}
	if qr.Total != 1 || qr.Explain == nil || qr.ByRepo["yt-med"] != 1 {
		t.Fatalf("query: %+v", qr)
	}
	rows, err := s.Rows(qr.ID, 0, 100)
	if err != nil || len(rows) != 1 {
		t.Fatal(err, rows)
	}
	r := rows[0]
	if r.Repo != "yt-med" || r.Hit == nil || r.Hit[0] != 0 || r.Hit[1] != 7 || len(r.Refs) != 1 || r.Refs[0].Name != "feat-YT03374" {
		t.Errorf("row: %+v", r)
	}

	// regex error keeps the previous result on screen (ID 0)
	bad, _ := s.Query(query.Conditions{Q: "(", Regex: true})
	if bad.ID != 0 || bad.Error == nil || bad.Error.Code != "paren" {
		t.Errorf("regex error: %+v", bad)
	}
	// empty result offers relaxations
	none, _ := s.Query(query.Conditions{Q: "医生站", Me: true})
	if none.Total != 0 || len(none.Relax) < 2 || none.Relax[0].Kind != "authors" || none.Relax[0].N != 1 {
		t.Errorf("relax: %+v", none.Relax)
	}

	key := CommitKey{Repo: r.Repo, Hash: r.Hash}
	d, err := s.Commit(ctx, key)
	if err != nil || d.Body != "超量阈值读取药品字典配置。" || len(d.Parents) != 1 {
		t.Errorf("commit: %+v %v", d, err)
	}
	files, err := s.Files(ctx, []CommitKey{key})
	if err != nil || len(files) != 1 || files[0].Add != 1 {
		t.Fatalf("files: %+v %v", files, err)
	}

	// A branch option counts the repos that have the name.
	branches, _ := s.Options(query.Conditions{}, KeyBranch)
	if want := []Option{{Value: "feat-YT03374", Count: 1}, {Value: "master", Count: 2}}; !reflect.DeepEqual(branches, want) {
		t.Errorf("branch options: %+v", branches)
	}
	if err := s.SetBase("yt-med", "master"); err != nil {
		t.Fatal(err)
	}
	fb, _ := s.Query(query.Conditions{Branches: []string{"feat-YT03374"}})
	if fb.Total != 1 {
		t.Errorf("branch with base: %d", fb.Total)
	}
	if err := s.SetMe([]string{"not-an-email"}); err == nil {
		t.Error("invalid email accepted")
	}

	// reopen: the snapshot makes it ready before any refresh
	s2, err := Open(ctx, root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if st := s2.Info().State; !st.Ready || st.Commits != 3 {
		t.Errorf("reopened state: %+v", st)
	}
	s.Close()
	s2.Close()
}

func mustQuery(t *testing.T, s *Session, c query.Conditions) QueryResult {
	qr, err := s.Query(c)
	if err != nil {
		t.Fatal(err)
	}
	return qr
}

func TestFavs(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := t.TempDir()
	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir()}
	s, err := Open(ctx, root, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		in   []string
		want []string
	}{
		{nil, []string{}},
		{[]string{"origin/uat", " origin/master ", ""}, []string{"origin/uat", "origin/master"}},
		{[]string{"b", "a", "b"}, []string{"b", "a"}},
	} {
		if err := s.SetFavs(tc.in); err != nil {
			t.Fatal(err)
		}
		if got := s.Favs(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SetFavs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	s2, _ := Open(ctx, root, opts)
	defer s2.Close()
	if got := s2.Favs(); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("favs not persisted: %q", got)
	}
}

// The repo list must reach the UI as soon as the scan is done, not when the index is: the state push after the
// scan has to find every repo already in Info.
func TestRepoListBeforeIndex(t *testing.T) {
	testutil.Isolate(t)
	root := filepath.Join(t.TempDir(), "ws")
	for _, n := range []string{"a", "b", "c"} {
		testutil.NewRepo(t, filepath.Join(root, n)).Commit("init", map[string]string{"f": "1\n"})
	}
	var s *Session
	seen := make(chan int, 16)
	opened := make(chan struct{})
	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now, OnState: func(st State) {
		if st.Scanned > 0 && !st.Ready {
			<-opened
			seen <- len(s.Info().Repos)
		}
	}}
	var err error
	if s, err = Open(context.Background(), root, opts); err != nil {
		t.Fatal(err)
	}
	close(opened)
	defer s.Close()
	select {
	case n := <-seen:
		if n != 3 {
			t.Fatalf("repos in Info when scanned is pushed: %d", n)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no scanned state before the index was ready")
	}
}

func TestSettersTellOnConfig(t *testing.T) {
	testutil.Isolate(t)
	root := filepath.Join(t.TempDir(), "ws")
	testutil.NewRepo(t, filepath.Join(root, "a")).Commit("init", map[string]string{"f": "1\n"})
	saved := make(chan struct{}, 16)
	s, err := Open(context.Background(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: time.Now,
		OnConfig: func() { saved <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for name, set := range map[string]func() error{
		"SetCompareTargets": func() error { return s.SetCompareTargets("rel", "main") },
		"SetRequirement":    func() error { return s.SetRequirement(`REQ-\d+`) },
		"SetBase":           func() error { return s.SetBase("a", "develop") },
		"SetFavs":           func() error { return s.SetFavs([]string{"x"}) },
	} {
		if err := set(); err != nil {
			t.Fatal(name, err)
		}
		select {
		case <-saved:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: OnConfig not called", name)
		}
	}
	if err := s.SetRequirement("("); err == nil {
		t.Fatal("bad pattern saved")
	}
	select {
	case <-saved:
		t.Fatal("OnConfig called for a refused setting")
	case <-time.After(50 * time.Millisecond):
	}
}

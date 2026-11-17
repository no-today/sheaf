package session

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"sheaf/internal/gitcli"
)

func TestDerivedRoundTrip(t *testing.T) {
	root, _ := reportFixture(t)
	s := openReport(t, root)
	oid, err := gitcli.ParseOid("1234567890123456789012345678901234567890")
	if err != nil {
		t.Fatal(err)
	}
	merge := remergeKey{root, oid}
	id := idKey{root, oid}
	used, stale := coverKey{root, oid, oid}, coverKey{root, gitcli.Oid{}, oid}
	s.remerge.put(merge, gitcli.MergeChanges{Files: []string{"resolved"}, Own: true})
	s.cover.ids = map[idKey]string{id: "patch"}
	s.cover.cover = map[coverKey]bool{used: false, stale: true}
	s.cover.get(used)
	s.saveDerived()
	before, err := os.ReadFile(s.derivedPath)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(s.derivedPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	s.saveDerived()
	st, err := os.Stat(s.derivedPath)
	if err != nil || !st.ModTime().Equal(stamp) {
		t.Fatalf("unchanged cache rewritten: %v %v", st, err)
	}
	loaded := openReport(t, root)
	if !reflect.DeepEqual(loaded.remerge.m, s.remerge.m) || !reflect.DeepEqual(loaded.cover.ids, s.cover.ids) {
		t.Fatal("merge or patch IDs did not round-trip")
	}
	if v, ok := loaded.cover.get(used); !ok || v {
		t.Fatal("cached negative answer lost")
	}
	if _, ok := loaded.cover.get(stale); ok {
		t.Fatal("unused line tip persisted")
	}
	loaded.saveDerived()
	after, err := os.ReadFile(s.derivedPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("same answers rewritten: %v", err)
	}
	// Another process must not discard a recently used line tip just because it queried something else.
	pruned := openReport(t, root)
	pruned.saveDerived()
	again := openReport(t, root)
	if len(again.cover.cover) != 1 || len(again.cover.ids) != 1 || len(again.remerge.m) != 1 {
		t.Fatal("unrelated session discarded shared facts")
	}
	pruned.opts.Now = func() time.Time { return s.opts.Now().Add((derivedCoverDays + 1) * 24 * time.Hour) }
	pruned.saveDerived()
	expired := openReport(t, root)
	if len(expired.cover.cover) != 0 || len(expired.cover.ids) != 1 || len(expired.remerge.m) != 1 {
		t.Fatal("old line tips did not expire independently of immutable results")
	}
}

func TestDerivedInvalid(t *testing.T) {
	for _, kind := range []string{"corrupt", "version", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := reportFixture(t)
			s := openReport(t, root)
			var b bytes.Buffer
			if kind == "corrupt" {
				b.WriteString("not a gob")
			} else {
				version := derivedVersion
				if kind == "version" {
					version++
				}
				if err := gob.NewEncoder(&b).Encode(derivedData{Version: version,
					IDs: map[derivedCommit]string{{Dir: root}: "ignored"}}); err != nil {
					t.Fatal(err)
				}
				if kind == "truncated" {
					b.Truncate(b.Len() - 1)
				}
			}
			if err := os.MkdirAll(filepath.Dir(s.derivedPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.derivedPath, b.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			loaded := openReport(t, root)
			if len(loaded.cover.ids) != 0 || loaded.derivedSaved != nil {
				t.Fatal("invalid cache was loaded")
			}
			if problems := loaded.SettleIndex(t.Context()); len(problems) != 0 {
				t.Fatal(problems)
			}
		})
	}
}

func TestDerivedLegacyRetention(t *testing.T) {
	for _, used := range []bool{false, true} {
		t.Run(fmt.Sprintf("used=%v", used), func(t *testing.T) {
			stamp := time.Unix(1_700_000_000, 0)
			path := filepath.Join(t.TempDir(), "derived.bin")
			key := derivedCover{Dir: "repo"}
			var encoded bytes.Buffer
			legacy := struct {
				Version int
				Cover   map[derivedCover]bool
			}{derivedVersion, map[derivedCover]bool{key: false}}
			if err := gob.NewEncoder(&encoded).Encode(legacy); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, encoded.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			s := &Session{derivedPath: path, opts: Options{Now: func() time.Time { return stamp }}}
			s.loadDerived()
			s.saveDerived()
			data := readDerived(path)
			if data == nil || len(data.Cover) != 1 {
				t.Fatal("legacy cache was not retained")
			}
			if used {
				if covered, ok := s.cover.get(coverKey{dir: "repo"}); !ok || covered {
					t.Fatal("legacy negative answer was lost")
				}
			}
			s.opts.Now = func() time.Time { return stamp.Add((derivedCoverDays + 1) * 24 * time.Hour) }
			s.saveDerived()
			data = readDerived(path)
			_, kept := data.Cover[key]
			if kept != used {
				t.Fatalf("expired legacy comparison retained=%v, current session used=%v", kept, used)
			}
		})
	}
}

func TestDerivedCacheWrite(t *testing.T) {
	root, _ := reportFixture(t)
	s := openReport(t, root)
	s.opts.CacheWrite = func(func()) {}
	s.remerge.put(remergeKey{dir: root}, gitcli.MergeChanges{Own: true})
	if problems := s.SettleIndex(t.Context()); len(problems) != 0 {
		t.Fatal(problems)
	}
	for _, path := range []string{s.indexPath, s.scanPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("skipped write created %s: %v", path, err)
		}
	}
	data := readDerived(s.derivedPath)
	if data == nil || !data.Merges[derivedCommit{Dir: root}].Own {
		t.Fatal("workspace cache policy blocked derived facts")
	}
	s.remerge.put(remergeKey{}, gitcli.MergeChanges{})
	s.Close()
	data = readDerived(s.derivedPath)
	if data == nil || len(data.Merges) != 2 {
		t.Fatal("Close did not save new derived facts while workspace writes were blocked")
	}
}

func TestDerivedReusedByNextSession(t *testing.T) {
	root, r := reportFixture(t)
	r.Git("checkout", "-qb", "PAY-142")
	sha := r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
	r.Git("checkout", "-q", "master")
	r.Commit("advance", map[string]string{"advance": "yes\n"})
	r.Git("checkout", "-q", "PAY-142")
	r.Merge("master", "PAY-142 sync master")
	r.Git("checkout", "-q", "master")
	r.Git("cherry-pick", "--no-commit", sha)
	r.Commit("applied change", nil)
	s := openReport(t, root)
	if err := s.Track("PAY-142"); err != nil {
		t.Fatal(err)
	}
	if problems := s.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	seen := map[string]bool{}
	for _, event := range s.rec.Events() {
		seen[event.Source] = true
	}
	if !seen["remerge"] || !seen["releaseCheck"] {
		t.Fatal("fixture did not exercise both derived caches:", seen)
	}
	before := mustReport(t, s, "PAY-142")
	s.Close()
	loaded := openReport(t, root)
	if problems := loaded.Settle(t.Context(), nil); len(problems) != 0 {
		t.Fatal(problems)
	}
	after := mustReport(t, loaded, "PAY-142")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("report changed after reopening:\nbefore %+v\nafter %+v", before, after)
	}
	for _, event := range loaded.rec.Events() {
		if event.Source == "remerge" || event.Source == "releaseCheck" {
			t.Fatalf("reopened session recomputed derived result: %+v", event)
		}
	}
}

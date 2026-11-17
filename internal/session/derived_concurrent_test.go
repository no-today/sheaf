package session

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"sheaf/internal/gitcli"
)

func addDerivedFacts(s *Session, dir string) {
	s.remerge.put(remergeKey{dir: dir}, gitcli.MergeChanges{Own: true})
	s.cover.mu.Lock()
	defer s.cover.mu.Unlock()
	if s.cover.ids == nil {
		s.cover.ids = map[idKey]string{}
		s.cover.cover = map[coverKey]bool{}
	}
	s.cover.ids[idKey{dir: dir}] = dir
	s.cover.cover[coverKey{dir: dir}] = false
	s.cover.use(coverKey{dir: dir})
}

func assertDerivedUnion(t *testing.T, path string, count int) {
	t.Helper()
	data := readDerived(path)
	if data == nil || len(data.Merges) != count || len(data.IDs) != count || len(data.Cover) != count {
		t.Fatalf("derived facts lost: want %d entries of each kind, got %+v", count, data)
	}
	for k, covered := range data.Cover {
		if covered || data.IDs[derivedCommit{Dir: k.Dir}] != k.Dir || !data.Merges[derivedCommit{Dir: k.Dir}].Own {
			t.Fatalf("shared facts changed: %+v", data)
		}
	}
}

func TestDerivedSessionsUnion(t *testing.T) {
	root, _ := reportFixture(t)
	first, second := openReport(t, root), openReport(t, root)
	addDerivedFacts(first, "first")
	addDerivedFacts(second, "second")
	first.saveDerived()
	second.saveDerived()
	assertDerivedUnion(t, first.derivedPath, 2)
	addDerivedFacts(first, "third")
	first.saveDerived()
	assertDerivedUnion(t, first.derivedPath, 3)
}

func TestDerivedWriterHelper(t *testing.T) {
	path := os.Getenv("SHEAF_DERIVED_TEST_PATH")
	if path == "" {
		t.Skip("subprocess helper")
	}
	s := &Session{derivedPath: path, opts: Options{Now: time.Now}}
	s.loadDerived()
	addDerivedFacts(s, os.Getenv("SHEAF_DERIVED_TEST_KEY"))
	fmt.Println("ready")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	s.saveDerived()
}

func TestDerivedConcurrentProcessesUnion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "derived.bin")
	type writer struct {
		cmd   *exec.Cmd
		start func()
	}
	writers := []writer{}
	for i := range 8 {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDerivedWriterHelper$")
		cmd.Env = append(os.Environ(), "SHEAF_DERIVED_TEST_PATH="+path, fmt.Sprintf("SHEAF_DERIVED_TEST_KEY=writer-%d", i))
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stdin.Close(); cmd.Process.Kill() })
		if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
			t.Fatalf("writer did not initialize: %q %v", line, err)
		}
		writers = append(writers, writer{cmd, func() { fmt.Fprintln(stdin, "save"); stdin.Close() }})
	}
	for _, w := range writers {
		w.start()
	}
	for _, w := range writers {
		if err := w.cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	assertDerivedUnion(t, path, len(writers))
}

func TestDerivedSavedBeforeClose(t *testing.T) {
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
	s.derivedDelay = 20 * time.Millisecond
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Report exercises the app's read path without Settle's synchronous flush or Close.
	mustReport(t, s, "PAY-142")
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		s.derivedMu.Lock()
		pending := s.derivedTimer != nil
		data := readDerived(s.derivedPath)
		s.derivedMu.Unlock()
		if !pending && data != nil && len(data.Merges) > 0 && len(data.IDs) > 0 && len(data.Cover) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("app's new derived facts were not saved before Close")
		case <-tick.C:
		}
	}
	before, err := os.Stat(s.derivedPath)
	if err != nil {
		t.Fatal(err)
	}
	mustReport(t, s, "PAY-142")
	s.derivedMu.Lock()
	pending := s.derivedTimer != nil
	s.derivedMu.Unlock()
	if pending {
		t.Fatal("cached report scheduled another save")
	}
	s.Close()
	after, err := os.Stat(s.derivedPath)
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("unchanged facts rewritten on Close: %v", err)
	}
}

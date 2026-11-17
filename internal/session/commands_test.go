package session

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

// Every git command the session runs is recorded, named by what asked for it and for which workspace; the status bar is told.
func TestCommandLog(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	a := testutil.NewRepo(t, filepath.Join(root, "app"))
	head := a.Commit("init", map[string]string{"a.txt": "a\n"})
	os.WriteFile(filepath.Join(root, "app", "a.txt"), []byte("b\n"), 0o644)

	var mu sync.Mutex
	var pulses []CmdPulse
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), OnCommands: func(p CmdPulse) {
		mu.Lock()
		pulses = append(pulses, p)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	s.bg.Wait() // the refresh Open started, and its dirty counts
	appDir := filepath.Join(root, "app")
	if _, err := s.Uncommitted(ctx, appDir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitTexts(ctx, CommitKey{Repo: "app", Hash: head}, "nope.txt", "", "A"); err == nil {
		t.Fatal("no error for a file that is not there")
	}

	events := s.rec.Events()
	var sources []string
	failed := 0
	for _, e := range events {
		sources = append(sources, e.Source)
		if e.Workspace != root || e.Kind == "" || e.Args == nil {
			t.Errorf("event %+v", e)
		}
		if e.Result == "fail" {
			failed++
			if e.Source != "commitTexts" || e.Exit == 0 || e.Stderr == "" || e.Dir != appDir {
				t.Errorf("failed %+v", e)
			}
		}
	}
	for _, want := range []string{"open", "refresh", "uncommitted", "commitTexts"} {
		if !slices.Contains(sources, want) {
			t.Errorf("no command from %q", want)
		}
	}
	if failed != 1 {
		t.Errorf("%d failed", failed)
	}
	if s.CommandsRunning() != 0 || s.CommandLogPath() != filepath.Join(s.opts.DataDir, "workspaces", workspaceID(root), "commands.jsonl") {
		t.Errorf("running %d, log %q", s.CommandsRunning(), s.CommandLogPath())
	}

	time.Sleep(2 * pulseEvery)
	mu.Lock()
	defer mu.Unlock()
	if len(pulses) == 0 || pulses[len(pulses)-1].Last != int(s.rec.LastID()) || pulses[len(pulses)-1].Running != 0 {
		t.Errorf("pulses %+v", pulses)
	}
}

// Every source key the session names its commands by reads in words in the command log file.
func TestSourceTexts(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	key := regexp.MustCompile(`(?:s\.git\([^,()]+(?:\([^()]*\))?, |afterWrite\()"(\w+)"`)
	var keys []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range key.FindAllStringSubmatch(string(b), -1) {
			keys = append(keys, m[1])
		}
	}
	if len(keys) < 20 {
		t.Fatalf("found only %d source keys: %v", len(keys), keys)
	}
	for _, k := range keys {
		if sourceText(k) == "" {
			t.Errorf("source %q has no text", k)
		}
	}
}

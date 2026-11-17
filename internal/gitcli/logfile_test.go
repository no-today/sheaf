package gitcli

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sheaf/internal/testutil"
)

// Every write (git or glab) leaves one JSON line in the log file that says what ran, for whom, how it ended and what it printed.
func TestLogFileRecord(t *testing.T) {
	testutil.Isolate(t)
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	r.Commit("init", map[string]string{"a.txt": "a\n"})
	long := strings.Repeat("x", 3*maxCapture)

	tests := []struct {
		name     string
		tool     string
		run      func(ctx context.Context, dir string) error
		fail     bool
		exit     int
		stdout   string // must be contained
		stderr   string // must be contained
		stdoutLn int    // exact length when > 0
		env      map[string]string
	}{
		{"git write ok", "", func(ctx context.Context, _ string) error {
			return FastForward(ctx, r.Dir, "HEAD")
		}, false, 0, "Already up to date.", "", 0, nil},
		{"git write fails", "", func(ctx context.Context, _ string) error {
			return FastForward(ctx, r.Dir, "refs/heads/missing")
		}, true, 1, "", "not something we can merge", 0, nil},
		{"glab write ok", ToolGlab, func(ctx context.Context, dir string) error {
			_, err := GlabMRCreate(ctx, dir, proj, "feat", "master")
			return err
		}, false, 0, "merge_requests/3", "", 0, nil},
		{"glab write fails", ToolGlab, func(ctx context.Context, dir string) error {
			_, err := GlabMRCreate(ctx, dir, proj, "feat", "master")
			return err
		}, true, 1, "Creating merge request", "target branch does not exist", 0, map[string]string{"FAKE_GLAB_EXIT": "1", "FAKE_GLAB_ERR": "target branch does not exist"}},
		{"glab output is capped", ToolGlab, func(ctx context.Context, dir string) error {
			_, err := GlabMRCreate(ctx, dir, proj, "feat", "master")
			return err
		}, false, 0, "Creating", "xxx", maxCapture, map[string]string{"FAKE_GLAB_ERR": long, "FAKE_GLAB_OUT": "https://h/g/p/-/merge_requests/3 " + long}},
		{"gh write ok", ToolGh, func(ctx context.Context, dir string) error {
			_, err := GhPRCreate(ctx, dir, "github.com/g/p", "feat", "main")
			return err
		}, false, 0, "pull/3", "", 0, nil},
		{"gh write fails", ToolGh, func(ctx context.Context, dir string) error {
			_, err := GhPRCreate(ctx, dir, "github.com/g/p", "feat", "main")
			return err
		}, true, 1, "pull/3", "base branch does not exist", 0, map[string]string{"FAKE_GH_EXIT": "1", "FAKE_GH_ERR": "base branch does not exist"}},
		{"gh output is capped", ToolGh, func(ctx context.Context, dir string) error {
			_, err := GhPRCreate(ctx, dir, "github.com/g/p", "feat", "main")
			return err
		}, false, 0, "pull/3", "xxx", maxCapture, map[string]string{"FAKE_GH_ERR": long, "FAKE_GH_OUT": "https://github.com/g/p/pull/3 " + long}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeGlab(t)
			fakeGh(t)
			t.Setenv("FAKE_GH_OUT", "https://github.com/g/p/pull/3")
			t.Setenv("FAKE_GLAB_OUT", "https://gitlab.example.com/g/p/-/merge_requests/3")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			path := filepath.Join(t.TempDir(), "commands.jsonl")
			rec := &Recorder{WriteLog: path, Workspace: "/work/space", Describe: func(s string) string { return "在测试中：" + s }}
			ctx := WithSource(WithRecorder(context.Background(), rec), "test · "+tc.name)
			dir := t.TempDir()
			if err := tc.run(ctx, dir); (err != nil) != tc.fail {
				t.Fatalf("err %v", err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var lines []Event
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var e Event
				if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
					t.Fatal(err)
				}
				lines = append(lines, e)
			}
			if len(lines) != 1 {
				t.Fatalf("%d lines in the log file", len(lines))
			}
			e := lines[0]
			if e.Kind != KindWrite || e.Tool != tc.tool || e.Exit != tc.exit || (e.Result == Failed) != tc.fail {
				t.Errorf("kind/tool/exit/result: %+v", e)
			}
			if e.Source != "test · "+tc.name || e.SourceText != "在测试中：test · "+tc.name || e.Workspace != "/work/space" || e.Dir == "" || len(e.Args) == 0 {
				t.Errorf("source/text/workspace/dir/args: %+v", e)
			}
			if e.Dur <= 0 || e.Start.IsZero() {
				t.Errorf("duration %v, start %v", e.Dur, e.Start)
			}
			if !strings.Contains(e.Stdout, tc.stdout) || !strings.Contains(e.Stderr, tc.stderr) {
				t.Errorf("stdout %q, stderr %q", e.Stdout, e.Stderr)
			}
			if len(e.Stdout) > maxCapture || len(e.Stderr) > maxCapture {
				t.Errorf("not capped: %d, %d", len(e.Stdout), len(e.Stderr))
			}
			if tc.stdoutLn > 0 && (len(e.Stdout) != tc.stdoutLn || len(e.Stderr) != tc.stdoutLn) {
				t.Errorf("capped to %d / %d, want %d", len(e.Stdout), len(e.Stderr), tc.stdoutLn)
			}
		})
	}
}

// A cut that lands inside a multi-byte character drops the character, so the line stays valid UTF-8.
func TestHeadKeepsUTF8(t *testing.T) {
	s := strings.Repeat("a", maxCapture-1) + "错"
	if got := head(s); got != strings.Repeat("a", maxCapture-1) {
		t.Errorf("head kept %d bytes", len(got))
	}
}

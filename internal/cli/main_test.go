package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sheaf/internal/appdir"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

type response struct {
	Version      int               `json:"version"`
	Workspace    string            `json:"workspace"`
	Scope        scope             `json:"scope"`
	Complete     bool              `json:"complete"`
	Problems     []session.Problem `json:"problems"`
	Result       json.RawMessage   `json:"result"`
	Error        *commandError     `json:"error"`
	DataDir      string            `json:"dataDir"`
	ConfigFile   string            `json:"configFile"`
	NextCommands [][]string        `json:"nextCommands"`
}

func invoke(t *testing.T, want int, args ...string) response {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args = append([]string{"sheaf", "--json"}, args...)
	if exit := Main(args, &stdout, &stderr); exit != want {
		t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, exit, want, &stdout, &stderr)
	}
	var out response
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, &stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout contains extra output: %v", err)
	}
	if out.Version != 1 || out.Problems == nil {
		t.Fatalf("invalid envelope: %+v", out)
	}
	return out
}

func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return out
}

func setup(t *testing.T) (string, *testutil.Repo) {
	t.Helper()
	testutil.Isolate(t)
	t.Setenv("SHEAF_HOME", t.TempDir())
	root := t.TempDir()
	r := testutil.NewRepo(t, filepath.Join(root, "repo"))
	r.Commit("init", map[string]string{"base": "base\n"})
	r.Git("branch", "release")
	configure(t, root, session.Config{MergeFlow: []session.MergeStep{{Name: "release", Branch: "release"}, {Name: "production", Branch: "master"}}})
	return root, r
}

func configure(t *testing.T, root string, cfg session.Config) {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	invoke(t, 0, "--workspace", root, "config", "apply", file)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRescanDiscoversNewRepositories(t *testing.T) {
	root, _ := setup(t)
	before := invoke(t, 0, "--workspace", root, "status")
	if len(before.Scope.Repositories) != 1 {
		t.Fatal(before.Scope.Repositories)
	}
	testutil.NewRepo(t, filepath.Join(root, "new")).Commit("init", map[string]string{"f": "new\n"})
	cached := invoke(t, 0, "--workspace", root, "status")
	if len(cached.Scope.Repositories) != 1 {
		t.Fatalf("default query did a full scan: %v", cached.Scope.Repositories)
	}
	rescanned := invoke(t, 0, "--workspace", root, "status", "--rescan")
	if len(rescanned.Scope.Repositories) != 2 {
		t.Fatalf("rescan missed new clone: %v", rescanned.Scope.Repositories)
	}
	for _, args := range [][]string{
		{"--rescan", "status"}, {"show", "PAY-142", "--rescan"},
		{"commits", "PAY-142", "--rescan"}, {"check", "--rescan"},
		{"archive", "PAY-142", "--rescan"}, {"config", "show", "--rescan"},
	} {
		o, err := parse(args)
		if err != nil || !o.rescan {
			t.Errorf("global rescan %v: %+v, %v", args, o, err)
		}
	}
}

func TestA01StandaloneConfigurationAndRoundTrip(t *testing.T) {
	root, r := setup(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 refund", map[string]string{"refund": "yes\n"})
	invoke(t, 0, "track", "pay-142", "--workspace", root)
	show := invoke(t, 0, "config", "show", "--workspace", root)
	if show.DataDir != os.Getenv("SHEAF_HOME") || show.ConfigFile == "" {
		t.Fatal(show)
	}
	cfg := decode[session.Config](t, show.Result)
	if cfg.Root != root || !slices.Contains(cfg.Tracked, "PAY-142") {
		t.Fatal(cfg)
	}
	wrapped, err := json.Marshal(show)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(file, wrapped, 0o600); err != nil {
		t.Fatal(err)
	}
	invoke(t, 0, "--workspace", root, "config", "apply", file)
	before := readFile(t, show.ConfigFile)
	if err := os.WriteFile(file, []byte(`{"mergeFlow":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	invoke(t, 3, "--workspace", root, "config", "apply", file)
	if !bytes.Equal(before, readFile(t, show.ConfigFile)) {
		t.Fatal("invalid config changed saved config")
	}
	result := invoke(t, 0, "--workspace", root, "show", "PAY-142")
	if !result.Complete || !decode[session.ReqReport](t, result.Result).Found {
		t.Fatal(result)
	}
	if len(result.NextCommands) != 1 {
		t.Fatal("missing commits command", result)
	}
	invoke(t, 0, result.NextCommands[0][1:]...)
	// Explicit replacement can recover a malformed config; queries must report it.
	if err := os.WriteFile(show.ConfigFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if bad := invoke(t, 2, "--workspace", root, "config", "show"); bad.Error.Code != "config_read" {
		t.Fatal(bad)
	}
	if err := os.WriteFile(file, wrapped, 0o600); err != nil {
		t.Fatal(err)
	}
	invoke(t, 0, "--workspace", root, "config", "apply", file)
	// Auto-location uses the same root persisted by config apply.
	t.Chdir(r.Dir)
	if out := invoke(t, 0, "status"); out.Workspace != root {
		t.Fatal(out)
	}
}

func TestA02A03A04OverviewScopesAndProgress(t *testing.T) {
	root, r := setup(t)
	r.Commit("PAY-100 finished", map[string]string{"done": "done\n"})
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 refund", map[string]string{"refund": "yes\n"})
	for _, id := range []string{"PAY-100", "PAY-142"} {
		invoke(t, 0, "--workspace", root, "track", id)
	}
	out := invoke(t, 0, "--workspace", root, "status")
	items := decode[statusResult](t, out.Result).Items
	if len(items) != 2 || items[0].ID != "PAY-142" || len(items[0].Gaps) != 1 || items[0].Gaps[0].Next != "release" || items[0].Gaps[0].Missing != 1 || items[1].Progress != session.StageLanded || items[1].Archived {
		t.Fatalf("%+v", items)
	}
	invoke(t, 1, "--workspace", root, "check")
	invoke(t, 0, "--workspace", root, "check", "PAY-100")
	invoke(t, 0, "--workspace", root, "archive", "PAY-142")
	for _, tc := range []struct {
		args []string
		ids  []string
	}{
		{nil, []string{"PAY-100"}},
		{[]string{"--archived"}, []string{"PAY-142"}},
		{[]string{"--all"}, []string{"PAY-142", "PAY-100"}},
		{[]string{"--archived", "--search", "REFUND"}, []string{"PAY-142"}},
		{[]string{"--all", "--search", "PAY-14"}, []string{}},
	} {
		out := invoke(t, 0, append([]string{"--workspace", root, "status"}, tc.args...)...)
		var ids []string
		for _, r := range decode[statusResult](t, out.Result).Items {
			ids = append(ids, r.ID)
		}
		if !slices.Equal(ids, tc.ids) {
			t.Fatalf("%v: got %v want %v", tc.args, ids, tc.ids)
		}
	}
	invoke(t, 3, "--workspace", root, "status", "--all", "--archived")
	invoke(t, 0, "--workspace", root, "show", "PAY-142")
	invoke(t, 0, "--workspace", root, "commits", "PAY-142")
}

func TestShowFiles(t *testing.T) {
	root, r := setup(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 files", map[string]string{"a-staged": "initial\n", "b-unstaged": "initial\n", "old-name": "rename me\n"})
	for _, path := range []string{"a-staged", "b-unstaged", "c-untracked"} {
		if err := os.WriteFile(filepath.Join(r.Dir, path), []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.Git("add", "a-staged")
	r.Git("mv", "old-name", "new-name")
	plain := invoke(t, 0, "--workspace", root, "show", "PAY-142")
	plainReport := decode[session.ReqReport](t, plain.Result)
	if len(plainReport.Rows) != 1 || plainReport.Rows[0].UncommittedFiles != nil || bytes.Contains(plain.Result, []byte(`"uncommittedFiles"`)) {
		t.Fatalf("default JSON changed: %s", plain.Result)
	}
	out := invoke(t, 0, "--workspace", root, "show", "PAY-142", "--files")
	report := decode[session.ReqReport](t, out.Result)
	want := []session.UncommittedFile{
		{Worktree: r.Dir, Path: "a-staged", Area: "staged", Status: "M"},
		{Worktree: r.Dir, Path: "b-unstaged", Area: "unstaged", Status: "M"},
		{Worktree: r.Dir, Path: "c-untracked", Area: "untracked", Status: "A"},
		{Worktree: r.Dir, Path: "new-name", OldPath: "old-name", Area: "staged", Status: "R"},
	}
	if len(report.Rows) != 1 || report.Rows[0].UncommittedFiles == nil || !slices.Equal(*report.Rows[0].UncommittedFiles, want) {
		t.Fatalf("files JSON: %s", out.Result)
	}
	report.Rows[0].UncommittedFiles = nil
	report.ObservedAt = plainReport.ObservedAt
	gotJSON, _ := json.Marshal(report)
	wantJSON, _ := json.Marshal(plainReport)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("files changed existing JSON fields:\n%s\n%s", gotJSON, wantJSON)
	}
	text := human(t, 0, "--workspace", root, "show", "PAY-142", "--files")
	for _, line := range []string{"    M staged a-staged\n", "    M unstaged b-unstaged\n", "    A untracked c-untracked\n", "    R staged old-name -> new-name\n"} {
		if !strings.Contains(text, line) {
			t.Fatalf("missing %q:\n%s", line, text)
		}
	}
	if strings.Contains(text, "run with --files") {
		t.Fatalf("files output includes hint:\n%s", text)
	}
	text = human(t, 0, "--workspace", root, "show", "PAY-142")
	if !strings.Contains(text, "    run with --files to list them\n") || strings.Contains(text, "M staged") {
		t.Fatalf("default human output: %s", text)
	}
	for _, args := range [][]string{{"status", "--files"}, {"check", "--files"}, {"check", "PAY-142", "--files"}} {
		out := invoke(t, 3, args...)
		if out.Error == nil || out.Error.Code != "invalid_input" || !strings.Contains(out.Error.Message, "--files is not supported") {
			t.Fatalf("%v: %+v", args, out)
		}
		human(t, 3, args...)
	}
}

func TestShowFilesMultipleWorktrees(t *testing.T) {
	root, r := setup(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 work", map[string]string{"work": "original\n"})
	wt := filepath.Join(t.TempDir(), "linked")
	r.Git("worktree", "add", "-qb", "PAY-142-second", wt)
	canonicalWT, err := filepath.EvalSymlinks(wt)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{r.Dir, canonicalWT} {
		if err := os.WriteFile(filepath.Join(dir, "work"), []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plain := decode[session.ReqReport](t, invoke(t, 0, "--workspace", root, "show", "PAY-142").Result)
	files := decode[session.ReqReport](t, invoke(t, 0, "--workspace", root, "show", "PAY-142", "--files").Result)
	if len(plain.Rows) != 1 || len(files.Rows) != len(plain.Rows) {
		t.Fatalf("--files changed rows: %+v, %+v", plain.Rows, files.Rows)
	}
	row := files.Rows[0]
	if row.UncommittedFiles == nil || len(*row.UncommittedFiles) != 2 || len(*row.UncommittedFiles) != row.Uncommitted {
		t.Fatalf("files/count: %+v", row)
	}
	want := map[string]bool{r.Dir: true, canonicalWT: true}
	for _, file := range *row.UncommittedFiles {
		if !want[file.Worktree] || file.Path != "work" || file.Area != "unstaged" || file.Status != "M" {
			t.Fatalf("wrong file worktree: %+v", file)
		}
		delete(want, file.Worktree)
	}
	files.Rows[0].UncommittedFiles = nil
	gotJSON, _ := json.Marshal(files.Rows)
	wantJSON, _ := json.Marshal(plain.Rows)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("--files changed row fields:\n%s\n%s", gotJSON, wantJSON)
	}
	text := human(t, 0, "--workspace", root, "show", "PAY-142", "--files")
	for _, dir := range []string{r.Dir, canonicalWT} {
		group := "    " + dir + "\n      M unstaged work\n"
		if !strings.Contains(text, group) {
			t.Fatalf("missing worktree group %q:\n%s", group, text)
		}
	}
	if strings.Count(text, " | deleted:") != 1 || strings.Contains(text, "run with --files") {
		t.Fatalf("wrong rows or hint in human output:\n%s", text)
	}
}

func recordStatusCalls(t *testing.T) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "status-calls")
	wrapper := `#!/bin/sh
for arg do
    if [ "$arg" = status ]; then
        printf '%s\n' "$*" >> "$SHEAF_TEST_STATUS_LOG"
    fi
done
exec "$SHEAF_TEST_REAL_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEAF_TEST_REAL_GIT", realGit)
	t.Setenv("SHEAF_TEST_STATUS_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestStatusEmptyScopeSkipsDirty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tracked bool
		archive bool
		flags   []string
	}{
		{name: "no tracked active"},
		{name: "no tracked all", flags: []string{"--all"}},
		{name: "no tracked archived", flags: []string{"--archived"}},
		{name: "only archived", tracked: true, archive: true},
		{name: "only active", tracked: true, flags: []string{"--archived"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, r := setup(t)
			r.Git("checkout", "-qb", "PAY-142")
			r.Commit("PAY-142 work", map[string]string{"work": "yes\n"})
			if tc.tracked {
				invoke(t, 0, "--workspace", root, "track", "PAY-142")
			}
			if tc.archive {
				invoke(t, 0, "--workspace", root, "archive", "PAY-142")
			}
			if err := os.WriteFile(filepath.Join(r.Dir, "pending"), []byte("work\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			log := recordStatusCalls(t)
			args := append([]string{"--workspace", root, "status"}, tc.flags...)
			out := invoke(t, 0, args...)
			if !out.Complete || len(decode[statusResult](t, out.Result).Items) != 0 {
				t.Fatal(out)
			}
			var stdout, stderr bytes.Buffer
			if code := Main(append([]string{"sheaf"}, args...), &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "No requirements") {
				t.Fatalf("exit %d, stdout %s, stderr %s", code, &stdout, &stderr)
			}
			if !tc.tracked || tc.archive {
				out := invoke(t, 2, "--workspace", root, "check")
				if out.Error == nil || out.Error.Code != "no_objects" {
					t.Fatal(out)
				}
			}
			if calls, err := os.ReadFile(log); !os.IsNotExist(err) {
				t.Fatalf("empty scope ran git status: %s, %v", calls, err)
			}
			out = invoke(t, 0, "--workspace", root, "show", "PAY-142")
			if decode[session.ReqReport](t, out.Result).Uncommitted != 1 || len(readFile(t, log)) == 0 {
				t.Fatal("explicit show did not read dirty worktree", out)
			}
		})
	}
}

func TestA17FetchPartialFailureKeepsResults(t *testing.T) {
	root, r := setup(t)
	r.Commit("PAY-142 complete", map[string]string{"done": "yes\n"})
	origin := testutil.NewRepo(t, filepath.Join(t.TempDir(), "origin"))
	origin.Commit("remote init", map[string]string{"remote": "ok\n"})
	r.Git("remote", "add", "origin", origin.Dir)
	bad := testutil.NewRepo(t, filepath.Join(root, "broken"))
	bad.Commit("init", map[string]string{"base": "ok\n"})
	bad.Git("remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	// No query has populated a scan cache before this fetch.
	out := invoke(t, 2, "--workspace", root, "show", "PAY-142", "--fetch")
	if out.Complete || !decode[session.ReqReport](t, out.Result).Found || !slices.ContainsFunc(out.Problems, func(p session.Problem) bool { return p.Code == session.ProblemFetchFailed && p.Repo == "broken" }) {
		t.Fatal(out)
	}
	if r.Git("rev-parse", "refs/remotes/origin/master") == "" {
		t.Fatal("first fetch did not refresh healthy repo")
	}
	out = invoke(t, 2, "--workspace", root, "check", "PAY-142", "--fetch")
	if decode[checkResult](t, out.Result).Passed {
		t.Fatal("partial fetch passed")
	}
}

func TestA19StructuredErrorsAndNoProgressOnStdout(t *testing.T) {
	root, _ := setup(t)
	for _, args := range [][]string{
		{"show"}, {"show", "PAY-142", "--wat"}, {"track", "PAY-142", "--fetch"},
		{"check", "PAY-142", "--stage", "missing"}, {"commits", "PAY-142", "--limit", "0"},
		{"track", "PAY-142-other"}, {"config", "set", "x"}, {"status", "--repo", "repo"},
	} {
		out := invoke(t, 3, append([]string{"--workspace", root}, args...)...)
		if out.Error == nil || out.Complete {
			t.Fatal(out)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"sheaf", "--bad", "--json"}, &stdout, &stderr); code != 3 || !json.Valid(stdout.Bytes()) {
		t.Fatalf("%d %s", code, &stdout)
	}
	stdout.Reset()
	if code := Main([]string{"sheaf", "--workspace", root, "status"}, &stdout, &stderr); code != 0 || strings.Contains(stdout.String(), "\x1b") || !strings.Contains(stdout.String(), "No requirements") {
		t.Fatalf("%d %s", code, &stdout)
	}
}

func TestA20EmptyNotFoundNoEvidenceAndDirty(t *testing.T) {
	root, r := setup(t)
	invoke(t, 0, "--workspace", root, "status")
	out := invoke(t, 2, "--workspace", root, "check")
	if out.Error.Code != "no_objects" || decode[checkResult](t, out.Result).Passed {
		t.Fatal(out)
	}
	for _, cmd := range []string{"show", "check", "commits"} {
		out := invoke(t, 3, "--workspace", root, cmd, "PAY-999")
		if out.Error.Code != "not_found" {
			t.Fatal(out)
		}
	}
	invoke(t, 0, "--workspace", root, "track", "PAY-999")
	for _, cmd := range []string{"show", "check", "commits"} {
		out := invoke(t, 2, "--workspace", root, cmd, "PAY-999")
		if !slices.ContainsFunc(out.Problems, func(p session.Problem) bool { return p.Code == session.ProblemNoEvidence }) {
			t.Fatal(out)
		}
	}
	invoke(t, 0, "--workspace", root, "untrack", "PAY-999")
	r.Git("checkout", "-qb", "PAY-142")
	if err := os.WriteFile(filepath.Join(r.Dir, "pending"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out = invoke(t, 1, "--workspace", root, "check", "PAY-142")
	check := decode[checkResult](t, out.Result)
	if check.Items[0].Uncommitted != 1 || check.Passed || !out.Complete {
		t.Fatal(check, out)
	}
}

func TestA21ManagementIdempotenceAndReadOnlyQueries(t *testing.T) {
	root, r := setup(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 work", map[string]string{"work": "ok\n"})
	invoke(t, 3, "--workspace", root, "archive", "PAY-142")
	invoke(t, 0, "--workspace", root, "track", "PAY-142")
	for _, action := range []string{"archive", "unarchive"} {
		invoke(t, 0, "--workspace", root, action, "PAY-142")
		show := invoke(t, 0, "--workspace", root, "config", "show")
		before := readFile(t, show.ConfigFile)
		stat, err := os.Stat(show.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		out := invoke(t, 0, "--workspace", root, action, "PAY-142")
		if decode[managementResult](t, out.Result).Changed {
			t.Fatal("repeat changed state")
		}
		for _, args := range [][]string{{"status", "--all"}, {"show", "PAY-142"}, {"commits", "PAY-142"}, {"check", "PAY-142"}} {
			code := 0
			if args[0] == "check" {
				code = 1
			}
			invoke(t, code, append([]string{"--workspace", root}, args...)...)
		}
		after, err := os.Stat(show.ConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, readFile(t, show.ConfigFile)) || !stat.ModTime().Equal(after.ModTime()) {
			t.Fatal("repeat or query rewrote configuration")
		}
	}
	invoke(t, 0, "--workspace", root, "untrack", "PAY-142")
	if out := invoke(t, 0, "--workspace", root, "show", "PAY-142"); decode[session.ReqReport](t, out.Result).Tracked {
		t.Fatal("untrack did not remove requirement")
	}
}

func TestLockHelper(t *testing.T) {
	if os.Getenv("SHEAF_CLI_LOCK_HELPER") != "1" {
		return
	}
	unlock, err := appdir.TryLock(context.Background(), os.Getenv("SHEAF_HOME"), os.Getenv("SHEAF_CLI_WORKSPACE_ID"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	fmt.Println("locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestA22SeparateProcessLockQueriesAndCrashRelease(t *testing.T) {
	root, r := setup(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 work", map[string]string{"work": "ok\n"})
	invoke(t, 0, "--workspace", root, "track", "PAY-142")
	show := invoke(t, 0, "--workspace", root, "config", "show")
	before := readFile(t, show.ConfigFile)
	process := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLockHelper$")
	process.Env = append(os.Environ(), "SHEAF_CLI_LOCK_HELPER=1", "SHEAF_CLI_WORKSPACE_ID="+session.WorkspaceID(root))
	pipe, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := process.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	process.Stderr = os.Stderr
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = process.Process.Kill() })
	if line, err := bufio.NewReader(pipe).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("holder: %q %v", line, err)
	}
	cache := filepath.Join(os.Getenv("SHEAF_HOME"), "cache")
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"track", "PAY-200"}, {"untrack", "PAY-142"}, {"archive", "PAY-142"}, {"unarchive", "PAY-142"}, {"config", "apply", show.ConfigFile}} {
		out := invoke(t, 4, append([]string{"--workspace", root}, args...)...)
		if out.Error.Code != "workspace_busy" {
			t.Fatal(out)
		}
	}
	for _, args := range [][]string{{"status"}, {"show", "PAY-142"}, {"commits", "PAY-142"}, {"check", "PAY-142"}, {"config", "show"}, {"status", "--fetch"}} {
		code := 0
		if args[0] == "check" {
			code = 1
		}
		invoke(t, code, append([]string{"--workspace", root}, args...)...)
	}
	if err := filepath.WalkDir(cache, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() != "derived.bin" && entry.Name() != "derived.bin.lock" {
			t.Errorf("locked query wrote workspace cache: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "workspaces", session.WorkspaceID(root), "derived.bin")); err != nil {
		t.Fatalf("locked queries did not persist derived facts: %v", err)
	}
	if !bytes.Equal(before, readFile(t, show.ConfigFile)) {
		t.Fatal("locked command changed config")
	}
	if err := process.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = process.Wait()
	invoke(t, 0, "--workspace", root, "archive", "PAY-142")
}

func TestWorkspaceAmbiguityAndSuggestions(t *testing.T) {
	root, r := setup(t)
	t.Chdir(r.Dir)
	configure(t, r.Dir, session.Config{MergeFlow: []session.MergeStep{{Name: "main", Branch: "master"}}})
	out := invoke(t, 3, "status")
	if len(out.Error.Workspaces) != 2 {
		t.Fatal(out)
	}
	invoke(t, 0, "--workspace", root, "status")
	t.Chdir(t.TempDir())
	out = invoke(t, 3, "status")
	if len(out.Error.Workspaces) != 2 {
		t.Fatal(out)
	}
}

func TestCommitsPagingFullMessagesAndExplicitStage(t *testing.T) {
	root, r := setup(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 first\n\nDetailed body.", map[string]string{"one": "one\n"})
	r.Commit("PAY-142 second", map[string]string{"two": "two\n"})
	out := invoke(t, 0, "--workspace", root, "commits", "PAY-142", "--limit", "1", "--repo", "repo")
	page := decode[commitResult](t, out.Result)
	if page.End || page.Next == "" || len(page.Items) != 1 {
		t.Fatal(page)
	}
	out = invoke(t, 0, "--workspace", root, "commits", "PAY-142", "--limit", "1", "--cursor", page.Next)
	page = decode[commitResult](t, out.Result)
	if !page.End || page.Items[0].Body == nil || !strings.Contains(*page.Items[0].Body, "Detailed body.") || len(page.Items[0].SHA) != 40 {
		t.Fatal(page)
	}
	invoke(t, 3, "--workspace", root, "commits", "PAY-142", "--cursor", "bad")
	invoke(t, 3, "--workspace", root, "commits", "PAY-142", "--repo", "missing")
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"sheaf", "--workspace", root, "commits", "PAY-142", "--full"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Detailed body.") {
		t.Fatalf("%d %s", code, &stdout)
	}
	r.Git("checkout", "master")
	r.Git("merge", "--ff-only", "PAY-142")
	invoke(t, 0, "--workspace", root, "check", "PAY-142")
	out = invoke(t, 1, "--workspace", root, "check", "PAY-142", "--stage", "release")
	check := decode[checkResult](t, out.Result)
	if check.Items[0].Progress != session.StageLanded || len(check.Items[0].Rows[0].Stages[0].Missing) != 2 {
		t.Fatal(check)
	}
}

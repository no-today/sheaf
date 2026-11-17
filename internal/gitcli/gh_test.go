package gitcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGhWhitelist(t *testing.T) {
	create := []string{"pr", "create", "--repo=github.com/g/p", "--base=main", "--head=feat", "--fill"}
	list := []string{"pr", "list", "--repo=github.com/g/p", "--base=main", "--head=feat", "--state=open", "--json=url,headRefName,baseRefName"}
	for _, tc := range []struct {
		args []string
		want Kind
	}{
		{create, KindWrite}, {list, KindRead}, {[]string{"auth", "status", "--hostname=github.com"}, KindRead},
		{nil, ""}, {[]string{"pr"}, ""}, {[]string{"api", "repos"}, ""}, {[]string{"pr", "merge", "1"}, ""},
		{[]string{"auth", "login"}, ""}, {[]string{"auth", "status", "--show-token"}, ""},
	} {
		if got := ghKindOf(tc.args); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.args, got, tc.want)
		}
	}
	for _, flag := range []string{"--repo=github.com/g/p", "--base=main", "--head=feat"} {
		var without []string
		for _, a := range create {
			if a != flag {
				without = append(without, a)
			}
		}
		if ghKindOf(without) != "" {
			t.Errorf("accepted missing %s", flag)
		}
		empty := append(append([]string{}, without...), strings.Split(flag, "=")[0]+"=")
		if ghKindOf(empty) != "" {
			t.Errorf("accepted empty %s", flag)
		}
	}
	for _, a := range []string{"--head=other", "--head=", "--base=other", "--repo=other/p", "--repo=github.example.com/g/p", "--web", "--draft", "--recover=x", "--fill=false", "positional"} {
		args := append(append([]string{}, create...), a)
		if ghKindOf(args) != "" {
			t.Errorf("accepted %v", args)
		}
	}
	for _, tc := range []struct {
		write bool
		args  []string
	}{{false, create}, {true, list}} {
		if _, err := runGh(context.Background(), "", tc.write, tc.args); err == nil || !strings.Contains(err.Error(), "only through") {
			t.Errorf("boundary: %v", err)
		}
	}
	if _, err := ghWrite(context.Background(), "", "pr", "merge"); err == nil || !strings.Contains(err.Error(), "not a command") {
		t.Fatal(err)
	}
}

func fakeGh(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$GH_PROMPT_DISABLED $NO_COLOR $*" >> "$FAKE_GH_LOG"
case "$1 $2" in
"auth status") [ -n "$FAKE_GH_AUTH_ERR" ] && echo "$FAKE_GH_AUTH_ERR" >&2; exit ${FAKE_GH_AUTH_EXIT:-0} ;;
"pr list") printf '%s' "$FAKE_GH_OUT" ;;
"pr create") printf '%s\n' "$FAKE_GH_OUT" ;;
esac
[ -n "$FAKE_GH_ERR" ] && echo "$FAKE_GH_ERR" >&2
exit ${FAKE_GH_EXIT:-0}
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH_LOG", log)
	return log
}

func TestGhPR(t *testing.T) {
	const url = "https://github.com/g/p/pull/12"
	for _, tc := range []struct {
		name, out, stderr, exit, authExit, authErr string
		create                                     bool
		want                                       string
		wantErr                                    error
	}{
		{name: "none", out: "[]"},
		{name: "existing", out: `[{"url":"` + url + `","headRefName":"feat","baseRefName":"main"}]`, want: url},
		{name: "different base", out: `[{"url":"` + url + `","headRefName":"feat","baseRefName":"dev"}]`},
		{name: "different head", out: `[{"url":"` + url + `","headRefName":"other","baseRefName":"main"}]`},
		{name: "invalid json", out: "oops", wantErr: errAny},
		{name: "created", create: true, out: url, want: url},
		{name: "missing URL", create: true, out: "done", wantErr: ErrNoPRAddress},
		{name: "login required", exit: "4", stderr: "authentication required", wantErr: ErrGhAuth},
		{name: "login hint", exit: "1", stderr: "To get started with GitHub CLI, please run: gh auth login", wantErr: ErrGhAuth},
		{name: "bad token", exit: "1", stderr: "HTTP 401: Bad credentials", wantErr: ErrGhAuth},
		{name: "private repo without login", exit: "1", stderr: "GraphQL: Could not resolve to a Repository", authExit: "1", authErr: "You are not logged into any GitHub hosts. Run gh auth login", wantErr: ErrGhAuth},
		{name: "missing repo with login", exit: "1", stderr: "HTTP 404: Not Found", wantErr: errAny},
		{name: "auth check network failure", exit: "1", stderr: "HTTP 404: Not Found", authExit: "1", authErr: "connection refused", wantErr: errAny},
		{name: "branch number is not auth", exit: "1", stderr: "no branch feat-401", wantErr: errAny},
		{name: "server error", exit: "1", stderr: "HTTP 503: service unavailable", wantErr: errAny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := fakeGh(t)
			for k, v := range map[string]string{"FAKE_GH_OUT": tc.out, "FAKE_GH_ERR": tc.stderr, "FAKE_GH_EXIT": tc.exit, "FAKE_GH_AUTH_EXIT": tc.authExit, "FAKE_GH_AUTH_ERR": tc.authErr} {
				t.Setenv(k, v)
			}
			var rec Recorder
			ctx := WithRecorder(context.Background(), &rec)
			dir := t.TempDir()
			var got string
			var err error
			if tc.create {
				got, err = GhPRCreate(ctx, dir, "github.com/g/p", "feat", "main")
			} else {
				got, err = GhPRFind(ctx, dir, "github.com/g/p", "feat", "main")
			}
			if got != tc.want || (err != nil) != (tc.wantErr != nil) {
				t.Fatalf("got %q, %v; want %q, %v", got, err, tc.want, tc.wantErr)
			}
			if tc.wantErr != nil && tc.wantErr != errAny && !errors.Is(err, tc.wantErr) {
				t.Fatalf("error %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == errAny && errors.Is(err, ErrGhAuth) {
				t.Fatalf("misclassified: %v", err)
			}
			raw, _ := os.ReadFile(log)
			if !strings.HasPrefix(string(raw), "1 1 pr ") || !strings.Contains(string(raw), "--repo=github.com/g/p") {
				t.Errorf("command/env: %s", raw)
			}
			events := rec.Events()
			wantKind := KindRead
			if tc.create {
				wantKind = KindWrite
			}
			var found bool
			for _, e := range events {
				if e.Args[0] == "pr" {
					found = true
					if e.Tool != ToolGh || e.Kind != wantKind || e.Dir != dir || (e.Result == Succeeded) != (tc.exit == "") {
						t.Errorf("event %+v", e)
					}
				}
			}
			if !found {
				t.Fatal("missing event")
			}
		})
	}
}

func TestGhWriteKept(t *testing.T) {
	fakeGh(t)
	t.Setenv("FAKE_GH_OUT", "https://github.com/g/p/pull/3")
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	ctx := WithRecorder(context.Background(), &Recorder{WriteLog: path})
	if _, err := GhPRCreate(ctx, t.TempDir(), "github.com/g/p", "feat", "main"); err != nil {
		t.Fatal(err)
	}
	ev := (&Recorder{WriteLog: path}).Events()
	if len(ev) != 1 || ev[0].Tool != ToolGh || ev[0].Kind != KindWrite || ev[0].Args[1] != "create" {
		t.Fatalf("events %+v", ev)
	}
}

func TestFindGh(t *testing.T) {
	empty, notExec, valid := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(notExec, "gh"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(empty, "gh"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := findGh([]string{empty, notExec, ""}); !errors.Is(err, ErrNoGh) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(valid, "gh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if got, err := findGh([]string{empty, notExec, valid}); err != nil || got != filepath.Join(valid, "gh") {
		t.Fatalf("%q %v", got, err)
	}
}

func TestGhLine(t *testing.T) {
	if got := GhLine("/w/a b", []string{"pr", "list", "--head=feat"}); got != "cd '/w/a b' && GH_PROMPT_DISABLED=1 NO_COLOR=1 gh pr list --head=feat" {
		t.Fatal(got)
	}
}

func TestGhCancelled(t *testing.T) {
	fakeGh(t)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := GhPRFind(ctx, t.TempDir(), "github.com/g/p", "feat", "main"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

package gitcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// glab runs only the whitelisted commands with the whitelisted flags; mr create always with --push=false.
func TestGlabWhitelist(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want Kind
	}{
		{[]string{"mr", "list", "--repo=h/g/p", "--source-branch=feat", "--target-branch=master", "--output=json"}, KindRead},
		{[]string{"mr", "list", "--source-branch=feat"}, ""}, // no --repo: glab would pick a remote
		{[]string{"mr", "list", "--repo=", "--source-branch=feat"}, ""},
		{[]string{"auth", "status", "--hostname=gitlab.example.com"}, KindRead},
		{[]string{"auth", "login"}, ""},
		{[]string{"auth", "status", "--show-token"}, ""},
		{[]string{"mr", "create", "--repo=h/g/p", "--source-branch=feat", "--target-branch=master", "--fill", "--yes", "--push=false"}, KindWrite},
		{[]string{"mr", "create", "--source-branch=feat", "--target-branch=master", "--fill", "--yes", "--repo=h/g/p"}, ""}, // glab would push
		{[]string{"mr", "create", "--repo=h/g/p", "--fill", "--yes", "--push=false", "--push"}, ""},
		{[]string{"mr", "create", "--repo=h/g/p", "--push=false", "--remove-source-branch"}, ""},
		{[]string{"mr", "create", "--repo=h/g/p", "--push=false", "--source-branch="}, ""},
		{[]string{"mr", "list", "--source-branch", "feat"}, ""},
		{[]string{"mr", "merge", "1"}, ""},
		{[]string{"mr", "close", "1"}, ""},
		{[]string{"mr", "delete", "1"}, ""},
		{[]string{"api", "projects"}, ""},
		{[]string{"auth", "login"}, ""},
		{[]string{"repo", "delete"}, ""},
		{[]string{"mr"}, ""},
		{nil, ""},
	} {
		if got := glabKindOf(tc.args); got != tc.want {
			t.Errorf("glabKindOf(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
	ctx := context.Background()
	if _, err := glabRead(ctx, "", "mr", "create", "--repo=h/g/p", "--push=false"); err == nil || !strings.Contains(err.Error(), "only through") {
		t.Errorf("a read ran a write: %v", err)
	}
	if _, err := glabWrite(ctx, "", "mr", "list", "--repo=h/g/p"); err == nil || !strings.Contains(err.Error(), "only through") {
		t.Errorf("a write ran a read: %v", err)
	}
	if _, err := glabWrite(ctx, "", "mr", "merge", "1"); err == nil || !strings.Contains(err.Error(), "not a command") {
		t.Errorf("not whitelisted: %v", err)
	}
}

// push sends one branch as a fast-forward and nothing else.
func TestPushPlain(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want Kind
	}{
		{[]string{"push", "--set-upstream", "--end-of-options", "origin", "refs/heads/x:refs/heads/x"}, KindWrite},
		{[]string{"push", "origin", "refs/heads/x:refs/heads/x"}, KindWrite},
		{[]string{"push", "--force", "origin", "refs/heads/x:refs/heads/x"}, ""},
		{[]string{"push", "-f", "origin", "refs/heads/x:refs/heads/x"}, ""},
		{[]string{"push", "--force-with-lease", "origin", "refs/heads/x:refs/heads/x"}, ""},
		{[]string{"push", "--end-of-options", "origin", "+refs/heads/x:refs/heads/x"}, ""},
		{[]string{"push", "origin", ":refs/heads/x"}, ""},
		{[]string{"push", "--delete", "origin", "x"}, ""},
		{[]string{"push", "--all", "origin"}, ""},
		{[]string{"push", "--mirror", "origin"}, ""},
		{[]string{"push", "--tags", "origin"}, ""},
		{[]string{"push", "origin", "x"}, ""},
		{[]string{"push", "origin"}, ""},
		{[]string{"push"}, ""},
	} {
		if got := KindOf(tc.args); got != tc.want {
			t.Errorf("KindOf(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

// fakeGlab puts a glab on PATH that logs its arguments and answers from the environment.
func fakeGlab(t *testing.T) (log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$GLAB_NO_PROMPT $*" >> "$FAKE_GLAB_LOG"
echo "config=${GLAB_CONFIG_DIR-unset}" >> "$FAKE_GLAB_LOG"
case "$1 $2" in
"auth status") exit ${FAKE_GLAB_AUTH_EXIT:-0} ;;
"mr list") printf '%s' "$FAKE_GLAB_OUT" ;;
"mr create") echo "Creating merge request for feat into master"; printf '%s\n' "$FAKE_GLAB_OUT" ;;
esac
[ -n "$FAKE_GLAB_ERR" ] && echo "$FAKE_GLAB_ERR" >&2
exit ${FAKE_GLAB_EXIT:-0}
`
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GLAB_LOG", log)
	return log
}

const proj = "gitlab.example.com/g/p"

func TestGlabMR(t *testing.T) {
	const url = "https://gitlab.example.com/g/p/-/merge_requests/12"
	tests := []struct {
		name, out, stderr, exit string
		authExit                string // exit code of glab auth status
		create                  bool
		want                    string
		err                     error // nil, ErrGlabAuth, or errAny
	}{
		{name: "none open", out: "[]", want: ""},
		{name: "open one", out: `[{"web_url":"` + url + `","source_branch":"feat","target_branch":"master"}]`, want: url},
		{name: "same source, other target", out: `[{"web_url":"` + url + `","source_branch":"feat","target_branch":"dev"}]`, want: ""},
		{name: "other branch only", out: `[{"web_url":"` + url + `","source_branch":"feat-2","target_branch":"master"}]`, want: ""},
		{name: "not json", out: "oops", err: errAny},
		{name: "created", create: true, out: url, want: url},
		{name: "created, old GitLab address", create: true, out: "https://gitlab.example.com/g/p/merge_requests/3", want: "https://gitlab.example.com/g/p/merge_requests/3"},
		{name: "created, no address", create: true, out: "done", err: errAny},
		{name: "not logged in", create: true, exit: "1", stderr: "glab: You are not logged into any GitLab hosts. Run glab auth login to authenticate.", err: ErrGlabAuth},
		{name: "401", exit: "1", stderr: "GET https://gitlab.example.com/api/v4/projects: 401 {message: 401 Unauthorized}", err: ErrGlabAuth},
		{name: "branch named like 401 is not a login problem", exit: "1", stderr: "no merge request found for YT-14012 / !401", err: errAny},
		{name: "404 and not logged in: private project", exit: "1", authExit: "1", stderr: "GET https://gitlab.example.com/api/v4/projects/g%2Fp/merge_requests: 404 {message: 404 Project Not Found}", err: ErrGlabAuth},
		{name: "404 and logged in: really not there", exit: "1", stderr: "GET https://gitlab.example.com/api/v4/projects/g%2Fp/merge_requests: 404 {message: 404 Project Not Found}", err: errAny},
		{name: "other failure", create: true, exit: "1", stderr: "target branch does not exist", err: errAny},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log := fakeGlab(t)
			t.Setenv("FAKE_GLAB_OUT", tc.out)
			t.Setenv("FAKE_GLAB_ERR", tc.stderr)
			t.Setenv("FAKE_GLAB_EXIT", tc.exit)
			t.Setenv("FAKE_GLAB_AUTH_EXIT", tc.authExit)
			var rec Recorder
			ctx := WithRecorder(context.Background(), &rec)
			dir := t.TempDir()
			var got string
			var err error
			if tc.create {
				got, err = GlabMRCreate(ctx, dir, proj, "feat", "master")
			} else {
				got, err = GlabMRFind(ctx, dir, proj, "feat", "master")
			}
			switch {
			case tc.err == nil && err != nil, tc.err != nil && err == nil:
				t.Fatalf("err %v, want %v", err, tc.err)
			case tc.err == ErrGlabAuth && !errors.Is(err, ErrGlabAuth):
				t.Fatalf("err %v, want not logged in", err)
			case tc.err == errAny && errors.Is(err, ErrGlabAuth):
				t.Fatalf("err %v, taken for not logged in", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			raw, _ := os.ReadFile(log)
			if !strings.HasPrefix(string(raw), "1 mr ") {
				t.Errorf("glab ran as %q (GLAB_NO_PROMPT not set?)", raw)
			}
			ev := rec.Events()
			kind := KindRead
			if tc.create {
				kind = KindWrite
			}
			if strings.Contains(tc.name, "404") { // glab auth status was asked too, recorded as a read
				if len(ev) != 2 || ev[0].Args[0] != "auth" || ev[0].Kind != KindRead {
					t.Errorf("recorded %+v", ev)
				}
				return
			}
			if len(ev) != 1 || ev[0].Tool != ToolGlab || ev[0].Kind != kind || ev[0].Dir != dir || (ev[0].Result == Succeeded) != (tc.exit == "") {
				t.Errorf("recorded %+v", ev)
			}
		})
	}
}

var errAny = errors.New("any error")

// A glab write is an audit record like a git write: kept in WriteLog for 30 days.
func TestGlabWriteKept(t *testing.T) {
	fakeGlab(t)
	t.Setenv("FAKE_GLAB_OUT", "https://gitlab.example.com/g/p/-/merge_requests/3")
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	ctx := WithRecorder(context.Background(), &Recorder{WriteLog: path})
	if _, err := GlabMRCreate(ctx, t.TempDir(), proj, "feat", "master"); err != nil {
		t.Fatal(err)
	}
	ev := (&Recorder{WriteLog: path}).Events()
	if len(ev) != 1 || ev[0].Tool != ToolGlab || ev[0].Kind != KindWrite || ev[0].Args[1] != "create" {
		t.Errorf("reloaded %+v", ev)
	}
}

func TestFindGlab(t *testing.T) {
	empty := t.TempDir()
	if _, err := findGlab([]string{empty, ""}); !errors.Is(err, ErrNoGlab) {
		t.Errorf("no glab: %v", err)
	}
	notExec := t.TempDir()
	os.WriteFile(filepath.Join(notExec, "glab"), []byte("x"), 0o644)
	if _, err := findGlab([]string{notExec}); !errors.Is(err, ErrNoGlab) {
		t.Errorf("not executable: %v", err)
	}
	ok := t.TempDir()
	os.WriteFile(filepath.Join(ok, "glab"), []byte("#!/bin/sh\n"), 0o755)
	if p, err := findGlab([]string{empty, ok}); err != nil || p != filepath.Join(ok, "glab") {
		t.Errorf("found %q, %v", p, err)
	}
}

func TestGlabLine(t *testing.T) {
	got := GlabLine("/w/a b", []string{"mr", "list", "--source-branch=feat"})
	if want := "cd '/w/a b' && GLAB_NO_PROMPT=1 NO_COLOR=1 glab mr list --source-branch=feat"; got != want {
		t.Errorf("got %q", got)
	}
}

// The workspace's glab configuration directory: empty leaves glab alone, a value is passed as GLAB_CONFIG_DIR with
// ~ expanded, a missing one is an error before glab starts, and only the fact is recorded.
func TestGlabConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, "gl-work")
	if err := os.Mkdir(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "afile")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, setting, system string
		want                  string // GLAB_CONFIG_DIR glab saw
		fail                  bool
	}{
		{name: "empty: not set", want: "unset"},
		{name: "empty keeps the user's own variable", system: "/somewhere", want: "/somewhere"},
		{name: "absolute", setting: cfg, want: cfg},
		{name: "spaces trimmed", setting: "  " + cfg + " ", want: cfg},
		{name: "tilde", setting: "~/gl-work", want: cfg},
		{name: "workspace wins over the user's variable", setting: cfg, system: "/somewhere", want: cfg},
		{name: "missing", setting: filepath.Join(home, "nope"), fail: true},
		{name: "tilde, missing", setting: "~/nope", fail: true},
		{name: "a file", setting: file, fail: true},
		{name: "relative", setting: "gl-work", fail: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log := fakeGlab(t)
			t.Setenv("FAKE_GLAB_OUT", "[]")
			if tc.system == "" {
				t.Setenv("GLAB_CONFIG_DIR", "")
				os.Unsetenv("GLAB_CONFIG_DIR")
			} else {
				t.Setenv("GLAB_CONFIG_DIR", tc.system)
			}
			var rec Recorder
			ctx := WithGlabConfig(WithRecorder(context.Background(), &rec), tc.setting)
			_, err := GlabMRFind(ctx, t.TempDir(), proj, "feat", "master")
			raw, _ := os.ReadFile(log)
			if tc.fail {
				if !errors.Is(err, ErrGlabConfig) {
					t.Fatalf("err %v, want ErrGlabConfig", err)
				}
				if len(raw) != 0 || len(rec.Events()) != 0 {
					t.Errorf("glab ran (%q) or was recorded", raw)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := "config=" + tc.want; !strings.Contains(string(raw), want+"\n") {
				t.Errorf("glab saw %q, want %q", raw, want)
			}
			ev := rec.Events()
			if len(ev) != 1 || ev[0].GlabConfig != (tc.setting != "") {
				t.Errorf("recorded %+v", ev)
			}
		})
	}
}

func TestNotLoggedIn(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want bool
	}{
		{"GET https://h/api/v4/user: 401 {message: 401 Unauthorized}", true},
		{"You are not logged into any GitLab hosts. Run glab auth login to authenticate.", true},
		{"glab: not logged in", true},
		{"no merge request for YT-14012", false},
		{"fatal: branch !401 not found", false},
		{"push to 401-fix rejected", false},
		{"404 Project Not Found", false},
		{"", false},
	} {
		if got := notLoggedIn(tc.s); got != tc.want {
			t.Errorf("notLoggedIn(%q) = %v", tc.s, got)
		}
	}
}

func TestProjectOf(t *testing.T) {
	for remote, want := range map[string]string{
		"https://gitlab.example.com/g/p.git":              "gitlab.example.com/g/p",
		"https://user:tok@gitlab.example.com/g/sub/p.git": "gitlab.example.com/g/sub/p",
		"http://gitlab.example.com:8080/g/p":              "gitlab.example.com:8080/g/p",
		"git@gitlab.example.com:g/sub/p.git":              "gitlab.example.com/g/sub/p",
		"ssh://git@gitlab.example.com:2222/g/p.git":       "gitlab.example.com/g/p",
		"  https://gitlab.com/g/p/ \n":                    "gitlab.com/g/p",
		"/tmp/origin.git":                                 "",
		"https://gitlab.example.com/":                     "",
		"https://gitlab.example.com/onlyone":              "",
		"":                                                "",
	} {
		if got := ProjectOf(remote); got != want {
			t.Errorf("ProjectOf(%q) = %q, want %q", remote, got, want)
		}
	}
}

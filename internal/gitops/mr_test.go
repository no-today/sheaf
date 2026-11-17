package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sheaf/internal/testutil"
)

// mrFixture: a bare origin with master, a clone with a local branch feat one commit ahead (not pushed), and a glab on PATH
// that logs its calls and answers from FAKE_GLAB_* (it never talks to a server).
const mrWeb = "https://gitlab.example.com/g/repo.git"

func mrFixture(t *testing.T) (repo *testutil.Repo, origin, glabLog string) {
	t.Helper()
	testutil.Isolate(t)
	root := t.TempDir()
	up := testutil.NewRepo(t, filepath.Join(root, "up"))
	up.Commit("init", map[string]string{"a.txt": "a\n"})
	origin = filepath.Join(root, "origin.git")
	up.Git("clone", "-q", "--bare", up.Dir, origin)
	repo = testutil.NewRepoAt(t, filepath.Join(root, "repo"))
	up.Git("clone", "-q", origin, repo.Dir)
	// origin must name a GitLab project; the bare repo stands behind that URL, so pushes still work
	repo.Git("config", "remote.origin.url", mrWeb)
	repo.Git("config", "url."+origin+".insteadOf", mrWeb)
	repo.Git("switch", "-q", "-c", "feat")
	repo.Commit("feat", map[string]string{"b.txt": "b\n"})

	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	glabLog = filepath.Join(root, "glab.log")
	script := `#!/bin/sh
echo "$*" >> "$FAKE_GLAB_LOG"
case "$1 $2" in
"mr list") if [ -f "$FAKE_GLAB_LOG.created" ] && [ -n "$FAKE_GLAB_LIST_AFTER" ]; then printf '%s' "$FAKE_GLAB_LIST_AFTER"; else printf '%s' "${FAKE_GLAB_LIST:-[]}"; fi ;;
"mr create") touch "$FAKE_GLAB_LOG.created"; printf '%s\n' "$FAKE_GLAB_URL" ;;
esac
[ -n "$FAKE_GLAB_ERR" ] && echo "$FAKE_GLAB_ERR" >&2
exit ${FAKE_GLAB_EXIT:-0}
`
	os.WriteFile(filepath.Join(bin, "glab"), []byte(script), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GLAB_LOG", glabLog)
	t.Setenv("FAKE_GLAB_URL", "https://gitlab.example.com/g/repo/-/merge_requests/7")
	return repo, origin, glabLog
}

func TestDecideMR(t *testing.T) {
	repo, _, _ := mrFixture(t)
	bare := testutil.NewRepo(t, filepath.Join(t.TempDir(), "no-origin"))
	noProject := testutil.NewRepo(t, filepath.Join(t.TempDir(), "np"))
	noProject.Git("remote", "add", "origin", "/somewhere/local.git")
	ok := MRRow{Key: "r", Dir: repo.Dir, Source: "feat", Local: true, Ahead: -1, Target: "master"}
	with := func(f func(*MRRow)) MRRow { r := ok; f(&r); return r }
	for _, tc := range []struct {
		name string
		row  MRRow
		skip string
	}{
		{"opens one", ok, ""},
		{"no branch", with(func(r *MRRow) { r.Source = "" }), SkipNoBranch},
		{"no target", with(func(r *MRRow) { r.Target = "" }), SkipNoTarget},
		{"the target itself", with(func(r *MRRow) { r.Source = "master" }), SkipIsTarget},
		{"no origin", with(func(r *MRRow) { r.Dir = bare.Dir }), SkipNoRemote},
		{"diverged", with(func(r *MRRow) { r.Ahead, r.Behind = 2, 1 }), SkipDiverged},
		{"only behind: nothing to push", with(func(r *MRRow) { r.Ahead, r.Behind = 0, 3 }), ""},
		{"only ahead", with(func(r *MRRow) { r.Ahead, r.Behind = 2, 0 }), ""},
		{"remote-only branch is never diverged", with(func(r *MRRow) { r.Local, r.Ahead, r.Behind = false, 2, 1 }), ""},
		{"origin names no GitLab project", with(func(r *MRRow) { r.Dir = noProject.Dir }), SkipNoProject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			skip, err := DecideMR(context.Background(), tc.row)
			if err != nil || skip != tc.skip {
				t.Errorf("got %q %v, want %q", skip, err, tc.skip)
			}
		})
	}
	if c := MRCommands(ok); len(c) != 2 || !strings.HasPrefix(c[0], "git push --set-upstream origin feat") || !strings.Contains(c[1], "--push=false") {
		t.Errorf("commands %q", c)
	}
	if c := MRCommands(with(func(r *MRRow) { r.Ahead = 0 })); len(c) != 1 {
		t.Errorf("pushed already: %q", c)
	}
}

func TestRunMR(t *testing.T) {
	const url = "https://gitlab.example.com/g/repo/-/merge_requests/7"
	tests := []struct {
		name    string
		row     func(dir string) MRRow
		env     map[string]string
		outcome string
		pushed  bool
		auth    bool
		calls   string // glab calls, first words, comma separated
	}{
		{"push then create", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "feat", Local: true, Ahead: -1, Target: "master"}
		}, nil, MRCreated, true, false, "mr list,mr create"},
		{"pushed already: create only", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "feat", Local: true, Ahead: 0, Target: "master"}
		}, nil, MRCreated, false, false, "mr list,mr create"},
		{"remote-only branch: no push", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "feat", Local: false, Ahead: 3, Target: "master"}
		}, nil, MRCreated, false, false, "mr list,mr create"},
		{"one open already", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "feat", Local: true, Ahead: 0, Target: "master"}
		}, map[string]string{"FAKE_GLAB_LIST": `[{"web_url":"` + url + `","source_branch":"feat","target_branch":"master"}]`}, MRExists, false, false, "mr list"},
		{"same source, other target: create", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "feat", Local: true, Ahead: 0, Target: "master"}
		}, map[string]string{"FAKE_GLAB_LIST": `[{"web_url":"` + url + `","source_branch":"feat","target_branch":"dev"}]`}, MRCreated, false, false, "mr list,mr create"},
		{"create prints no address: found by listing", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "feat", Local: true, Ahead: 0, Target: "master"}
		}, map[string]string{"FAKE_GLAB_URL": "created !7", "FAKE_GLAB_LIST_AFTER": `[{"web_url":"` + url + `","source_branch":"feat","target_branch":"master"}]`}, MRCreated, false, false, "mr list,mr create,mr list"},
		{"not logged in", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "feat", Local: true, Ahead: 0, Target: "master"}
		}, map[string]string{"FAKE_GLAB_EXIT": "1", "FAKE_GLAB_ERR": "Run glab auth login to authenticate."}, Failed, false, true, "mr list"},
		{"diverged at run time: origin moved after the plan", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "feat", Local: true, Ahead: -1, Target: "master"}
		}, map[string]string{"OTHER_PUSHED": "1"}, Skipped, false, false, ""},
		{"skipped", func(d string) MRRow {
			return MRRow{Key: "r", Dir: d, Source: "master", Local: true, Target: "master"}
		}, nil, Skipped, false, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, origin, log := mrFixture(t)
			for k, v := range tc.env {
				if k == "OTHER_PUSHED" { // someone else pushed a different feat to origin
					dir := filepath.Join(t.TempDir(), "other")
					repo.Git("clone", "-q", origin, dir)
					other := testutil.NewRepoAt(t, dir)
					other.Git("switch", "-q", "-c", "feat")
					other.Commit("theirs", map[string]string{"t.txt": "t\n"})
					other.Git("push", "-q", "origin", "feat")
					continue
				}
				t.Setenv(k, v)
			}
			res := RunMR(context.Background(), []MRRow{tc.row(repo.Dir)}, Progress{})
			r := res[0]
			if r.Outcome != tc.outcome || r.Pushed != tc.pushed || r.Auth != tc.auth {
				t.Fatalf("result %+v", r)
			}
			if tc.env["OTHER_PUSHED"] != "" && r.Skip != SkipDiverged {
				t.Errorf("push rejected but result is %+v", r)
			}
			if (tc.outcome == MRCreated || tc.outcome == MRExists) && r.URL != url {
				t.Errorf("url %q", r.URL)
			}
			raw, _ := os.ReadFile(log)
			var calls []string
			for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if f := strings.Fields(l); len(f) >= 2 {
					calls = append(calls, f[0]+" "+f[1])
				}
			}
			if got := strings.Join(calls, ","); got != tc.calls {
				t.Errorf("glab calls %q, want %q", got, tc.calls)
			}
			if tc.calls != "" && !strings.Contains(string(raw), "--repo=gitlab.example.com/g/repo ") {
				t.Errorf("glab was not told origin's project: %q", raw)
			}
			onOrigin := testutil.NewRepoAt(t, origin).Git("for-each-ref", "--format=%(refname)", "refs/heads/feat")
			if tc.env["OTHER_PUSHED"] == "" && (onOrigin != "") != tc.pushed {
				t.Errorf("origin has feat: %q", onOrigin)
			}
			if tc.pushed {
				if up := repo.Git("rev-parse", "--abbrev-ref", "feat@{upstream}"); up != "origin/feat" {
					t.Errorf("upstream %q", up)
				}
			}
		})
	}
}

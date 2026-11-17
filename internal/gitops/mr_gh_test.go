package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sheaf/internal/gitcli"
)

func TestRunGitHubPR(t *testing.T) {
	const url = "https://github.com/g/repo/pull/7"
	const found = `[{"url":"https://github.com/g/repo/pull/7","headRefName":"feat","baseRefName":"master"}]`
	for _, tc := range []struct {
		name             string
		push, remoteOnly bool
		env              map[string]string
		outcome          string
		auth             bool
		calls            string
	}{
		{name: "push then create", push: true, outcome: MRCreated, calls: "pr list,pr create"},
		{name: "already pushed", outcome: MRCreated, calls: "pr list,pr create"},
		{name: "remote branch", remoteOnly: true, outcome: MRCreated, calls: "pr list,pr create"},
		{name: "existing", env: map[string]string{"FAKE_GH_LIST": found}, outcome: MRExists, calls: "pr list"},
		{name: "other base", env: map[string]string{"FAKE_GH_LIST": strings.ReplaceAll(found, "master", "dev")}, outcome: MRCreated, calls: "pr list,pr create"},
		{name: "other head", env: map[string]string{"FAKE_GH_LIST": strings.ReplaceAll(found, "feat", "other")}, outcome: MRCreated, calls: "pr list,pr create"},
		{name: "missing address recovers", env: map[string]string{"FAKE_GH_URL": "created", "FAKE_GH_LIST_AFTER": found}, outcome: MRCreated, calls: "pr list,pr create,pr list"},
		{name: "no login", env: map[string]string{"FAKE_GH_EXIT": "4", "FAKE_GH_ERR": "run gh auth login"}, outcome: Failed, auth: true, calls: "pr list"},
		{name: "network failure", env: map[string]string{"FAKE_GH_EXIT": "1", "FAKE_GH_ERR": "connection refused"}, outcome: Failed, calls: "pr list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, origin, glabLog := mrFixture(t)
			web := "https://github.com/g/repo.git"
			repo.Git("config", "remote.origin.url", web)
			repo.Git("config", "url."+origin+".insteadOf", web)
			bin := t.TempDir()
			log := filepath.Join(bin, "gh.log")
			script := `#!/bin/sh
echo "$*" >> "$FAKE_GH_LOG"
case "$1 $2" in
"pr list") if [ -f "$FAKE_GH_LOG.created" ] && [ -n "$FAKE_GH_LIST_AFTER" ]; then printf '%s' "$FAKE_GH_LIST_AFTER"; else printf '%s' "${FAKE_GH_LIST:-[]}"; fi ;;
"pr create") touch "$FAKE_GH_LOG.created"; printf '%s\n' "$FAKE_GH_URL" ;;
esac
[ -n "$FAKE_GH_ERR" ] && echo "$FAKE_GH_ERR" >&2
exit ${FAKE_GH_EXIT:-0}
`
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FAKE_GH_LOG", log)
			t.Setenv("FAKE_GH_URL", url)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			row := MRRow{Key: "r", Dir: repo.Dir, Source: "feat", Target: "master", Local: !tc.remoteOnly}
			if tc.push {
				row.Ahead = -1
			}
			if !tc.push {
				repo.Git("push", "-q", "origin", "feat")
			}
			prepared, err := PrepareMR(context.Background(), row)
			if err != nil || prepared.Tool != "gh" || prepared.Project != "github.com/g/repo" || prepared.Missing {
				t.Fatalf("prepared %+v, %v", prepared, err)
			}
			commands := MRCommands(prepared)
			cmd := commands[len(commands)-1]
			for _, arg := range []string{"gh pr create", "--repo=github.com/g/repo", "--head=feat", "--base=master"} {
				if !strings.Contains(cmd, arg) {
					t.Errorf("missing %s: %s", arg, cmd)
				}
			}
			var rec gitcli.Recorder
			res := RunMR(gitcli.WithRecorder(context.Background(), &rec), []MRRow{row}, Progress{})[0]
			if res.Tool != "gh" || res.Outcome != tc.outcome || res.Pushed != tc.push || res.Auth != tc.auth {
				t.Fatalf("result %+v", res)
			}
			if (res.Outcome == MRCreated || res.Outcome == MRExists) && res.URL != url {
				t.Fatal(res.URL)
			}
			raw, _ := os.ReadFile(log)
			var calls []string
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					calls = append(calls, f[0]+" "+f[1])
				}
				if !strings.Contains(line, "--repo=github.com/g/repo") {
					t.Errorf("missing repo: %s", line)
				}
			}
			if got := strings.Join(calls, ","); got != tc.calls {
				t.Errorf("calls %s, want %s", got, tc.calls)
			}
			if raw, _ := os.ReadFile(glabLog); len(raw) > 0 {
				t.Fatalf("GitHub used glab: %s", raw)
			}
			var pushes int
			for _, ev := range rec.Events() {
				if ev.Tool == "" && ev.Args[0] == "push" {
					pushes++
				}
				if ev.Tool == "glab" {
					t.Error("glab recorded")
				}
			}
			if (pushes == 1) != tc.push {
				t.Errorf("push count %d", pushes)
			}
		})
	}
}

func TestMRProviderHosts(t *testing.T) {
	repo, _, _ := mrFixture(t)
	for _, tc := range []struct{ origin, tool string }{
		{"git@github.com:g/repo.git", "gh"}, {"ssh://git@GITHUB.COM:22/g/repo.git", "gh"}, {"https://user:" + "sample" + "@github.com/g/repo.git", "gh"},
		{"https://github.com:443/g/repo.git", "gh"}, {"https://github.com.evil.test/g/repo.git", "glab"}, {"git@github.example.com:g/repo.git", "glab"}, {mrWeb, "glab"},
	} {
		repo.Git("config", "remote.origin.url", tc.origin)
		r, err := PrepareMR(context.Background(), MRRow{Dir: repo.Dir})
		if err != nil || r.Tool != tc.tool || strings.Contains(r.Project, "sample") || strings.Contains(r.Project, "@") {
			t.Errorf("%s: %+v %v", tc.origin, r, err)
		}
	}
}

package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sheaf/internal/query"
	"sheaf/internal/testutil"
)

func TestCreateMRMixedHosts(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "ws")
	for _, tc := range []struct{ name, host string }{{"lab", "gitlab.example.com"}, {"hub", "github.com"}} {
		r := testutil.NewRepo(t, filepath.Join(root, tc.name))
		r.Commit("init", map[string]string{"a": "a\n"})
		origin := filepath.Join(tmp, tc.name+".git")
		r.Git("clone", "-q", "--bare", r.Dir, origin)
		web := "https://" + tc.host + "/g/" + tc.name + ".git"
		r.Git("remote", "add", "origin", web)
		r.Git("config", "url."+origin+".insteadOf", web)
		r.Git("switch", "-q", "-c", "feat")
		r.Commit("feat", map[string]string{"b": "b\n"})
	}
	bin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	for tool, script := range map[string]string{
		"glab": `#!/bin/sh
case "$1 $2" in
"mr list") printf '[]' ;;
"mr create") echo "https://gitlab.example.com/g/lab/-/merge_requests/1" ;;
esac
`,
		"gh": `#!/bin/sh
case "$1 $2" in
"pr list") printf '[]' ;;
"pr create") echo "https://github.com/g/hub/pull/1" ;;
esac
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	in := MRInput{Agg: TagAggInput{Cond: &query.Conditions{Authors: []string{}, Branches: []string{"feat"}}}, Rows: []string{"lab", "hub"}}
	plan, err := s.MRPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 2 || plan.Open != 2 {
		t.Fatalf("plan %+v", plan)
	}
	for _, r := range plan.Rows {
		tool := "glab"
		if r.Repo == "hub" {
			tool = "gh"
		}
		if r.Tool != tool || r.Missing || !r.Push || r.Skip != "" || len(r.Commands) != 2 || !strings.HasPrefix(r.Commands[1], tool+" ") {
			t.Errorf("plan row %+v", r)
		}
	}
	// An unused GitLab setting must not prevent GitHub-only requests from running.
	if err := s.SetGlabConfig(filepath.Join(tmp, "missing-config")); err != nil {
		t.Fatal(err)
	}
	hubOnly := in
	hubOnly.Rows = []string{"hub"}
	run, err := s.CreateMR(ctx, hubOnly)
	if err != nil || run.Created != 1 || len(run.Rows) != 1 || run.Rows[0].Tool != "gh" || !run.Rows[0].Pushed || run.Rows[0].URL != "https://github.com/g/hub/pull/1" {
		t.Fatalf("GitHub-only run %+v, %v", run, err)
	}
	if err := s.SetGlabConfig(""); err != nil {
		t.Fatal(err)
	}
	run, err = s.CreateMR(ctx, in)
	if err != nil || run.Created != 2 || run.Failed != 0 {
		t.Fatalf("mixed run %+v, %v", run, err)
	}
	for _, r := range run.Rows {
		if r.Repo == "lab" && r.Tool != "glab" || r.Repo == "hub" && r.Tool != "gh" {
			t.Errorf("result row %+v", r)
		}
	}
	var gh, glab int
	for _, e := range s.rec.Events() {
		switch e.Tool {
		case "gh":
			gh++
		case "glab":
			glab++
		}
	}
	if gh != 4 || glab != 2 {
		t.Errorf("recorded gh=%d glab=%d", gh, glab)
	}
}

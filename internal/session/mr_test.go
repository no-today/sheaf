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

func TestMRPlanSkipsRepoWithBrokenConfig(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	var broken *testutil.Repo
	for _, name := range []string{"broken", "healthy"} {
		r := testutil.NewRepo(t, filepath.Join(root, name))
		r.Commit("init", map[string]string{"a.txt": "a\n"})
		r.Git("remote", "add", "origin", "https://gitlab.example.com/g/"+name+".git")
		r.Git("switch", "-q", "-c", "feat")
		r.Commit("feat", map[string]string{"f.txt": "f\n"})
		if name == "broken" {
			broken = r
		}
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "glab"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s, err := Open(t.Context(), root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), NoAutoRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken.Dir, ".git", "config"), []byte("[broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := MRInput{Agg: TagAggInput{Cond: &query.Conditions{Authors: []string{}, Branches: []string{"feat"}}}, Rows: []string{"broken", "healthy"}}
	plan, err := s.MRPlan(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 2 || plan.Open != 1 {
		t.Fatalf("plan = %+v, want both rows and one actionable row", plan)
	}
	if r := plan.Rows[0]; r.Key != "broken" || r.Skip != "no_remote" || r.Push || len(r.Commands) != 0 {
		t.Errorf("broken row = %+v, want skipped without commands", r)
	}
	if r := plan.Rows[1]; r.Key != "healthy" || r.Skip != "" || !r.Push || len(r.Commands) != 2 {
		t.Errorf("healthy row = %+v, want push and create", r)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.MRPlan(ctx, in); err == nil {
		t.Error("canceled plan must return an error")
	}
}

// 创建 MR end to end, with a glab on PATH that answers like the real one and never talks to a server.
func TestCreateMR(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(tmp, "w")
	remotes := filepath.Join(tmp, "remotes")
	// clone: a repo cloned from a bare origin, on feat with one commit; pushed: origin has feat as it is locally.
	clone := func(name string, pushed bool) *testutil.Repo {
		up := testutil.NewRepo(t, filepath.Join(tmp, "up", name))
		up.Commit("init", map[string]string{"a.txt": "a\n"})
		origin := filepath.Join(remotes, name+".git")
		up.Git("clone", "-q", "--bare", up.Dir, origin)
		r := testutil.NewRepoAt(t, filepath.Join(root, name))
		up.Git("clone", "-q", origin, r.Dir)
		// origin must name a GitLab project; the bare repo stands behind that URL, so pushes still work
		web := "https://gitlab.example.com/g/" + name + ".git"
		r.Git("config", "remote.origin.url", web)
		r.Git("config", "url."+origin+".insteadOf", web)
		r.Git("switch", "-q", "-c", "feat")
		r.Commit("feat work", map[string]string{"f.txt": name + "\n"})
		if pushed {
			r.Git("push", "-q", "origin", "feat")
		}
		return r
	}
	clone("synced", true)
	ahead := clone("ahead", false)
	lone := testutil.NewRepo(t, filepath.Join(root, "lone"))
	lone.Commit("init", map[string]string{"a.txt": "a\n"})
	lone.Git("switch", "-q", "-c", "feat")
	lone.Commit("feat work", map[string]string{"f.txt": "f\n"})

	bin := filepath.Join(tmp, "bin")
	os.MkdirAll(bin, 0o755)
	log := filepath.Join(tmp, "glab.log")
	os.WriteFile(filepath.Join(bin, "glab"), []byte(`#!/bin/sh
echo "$PWD $*" >> "$FAKE_GLAB_LOG"
case "$1 $2" in
"mr list") printf '[]' ;;
"mr create") echo "https://gitlab.example.com/g/$(basename "$PWD")/-/merge_requests/1" ;;
esac
`), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GLAB_LOG", log)

	s, err := Open(ctx, root, Options{DataDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	in := MRInput{Agg: TagAggInput{Cond: &query.Conditions{Authors: []string{}, Branches: []string{"feat"}}}, Rows: []string{"synced", "ahead", "lone"}}

	plan, err := s.MRPlan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	wantPlan := map[string]string{"synced": "open", "ahead": "push open", "lone": "skip no_remote"}
	for _, r := range plan.Rows {
		got := "open"
		if r.Push {
			got = "push open"
		}
		if r.Skip != "" {
			got = "skip " + r.Skip
		}
		if got != wantPlan[r.Key] || r.Branch != "feat" || r.Target != "master" || (r.Skip == "") != (len(r.Commands) > 0) {
			t.Errorf("plan %s: %+v", r.Key, r)
		}
	}
	if len(plan.Rows) != 3 || plan.Open != 2 {
		t.Errorf("plan %+v", plan)
	}

	run, err := s.CreateMR(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"synced": "created", "ahead": "created pushed", "lone": "skipped"}
	for _, r := range run.Rows {
		got := r.Outcome
		if r.Pushed {
			got += " pushed"
		}
		if got != want[r.Key] || (r.Outcome == "created") != strings.HasSuffix(r.URL, "/g/"+r.Key+"/-/merge_requests/1") {
			t.Errorf("run %s: %+v", r.Key, r)
		}
	}
	if run.Created != 2 || run.Skipped != 1 || run.Failed != 0 {
		t.Errorf("run %+v", run)
	}
	if up := ahead.Git("rev-parse", "--abbrev-ref", "feat@{upstream}"); up != "origin/feat" {
		t.Errorf("ahead not pushed with upstream: %q", up)
	}
	// The command log has both glab calls of a row and the push, glab's with its own line.
	var glab, push int
	for _, e := range s.rec.Events() {
		switch {
		case e.Tool == "glab":
			glab++
			if len(e.Args) < 2 || e.Args[0] != "mr" || e.Workspace != s.Root {
				t.Errorf("glab event %+v", e)
			}
		case e.Tool == "" && len(e.Args) > 0 && e.Args[0] == "push":
			push++
		}
	}
	if glab != 4 || push != 1 {
		t.Errorf("recorded %d glab, %d push", glab, push)
	}
}

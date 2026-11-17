package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

func TestMeDerivedAndAdded(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "ws")
	a := testutil.NewRepo(t, filepath.Join(root, "a"))
	a.Commit("init", map[string]string{"f": "1\n"})
	a.Git("config", "user.email", "Work@Corp.com")
	b := testutil.NewRepo(t, filepath.Join(root, "b"))
	b.Commit("init", map[string]string{"f": "1\n"})
	b.Git("config", "user.email", "work@corp.com")
	c := testutil.NewRepo(t, filepath.Join(root, "c")) // no own email: the global one applies
	c.Commit("init", map[string]string{"f": "1\n"})

	opts := Options{DataDir: t.TempDir(), CacheDir: t.TempDir(), Now: func() time.Time { return time.Unix(1_700_000_000, 0) }}
	s, err := Open(ctx, root, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close) // the open-time refresh writes into the temp dirs: wait for it before they are removed
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	info := s.Info()
	if !reflect.DeepEqual(info.Me, []string{"me@example.com", "work@corp.com"}) || len(info.MeManual) != 0 {
		t.Fatalf("auto: %+v", info)
	}
	want := []MeFrom{
		{Email: "me@example.com", Global: true, Repos: []string{}},
		{Email: "work@corp.com", Repos: []string{"a", "b"}},
	}
	if !reflect.DeepEqual(info.MeAuto, want) {
		t.Fatalf("meAuto: %+v", info.MeAuto)
	}

	// added by hand: joins the derived ones (an already derived address is not listed twice), also after a refresh and a reopen
	if err := s.SetMe([]string{"Old@example.com", "work@corp.com"}); err != nil {
		t.Fatal(err)
	}
	s.Refresh(ctx)
	info = s.Info()
	if !reflect.DeepEqual(info.Me, []string{"me@example.com", "work@corp.com", "old@example.com"}) || !reflect.DeepEqual(info.MeManual, []string{"old@example.com"}) || !reflect.DeepEqual(info.MeAuto, want) {
		t.Fatalf("manual: %+v", info)
	}
	s2, _ := Open(ctx, root, opts)
	t.Cleanup(s2.Close)
	s2.Refresh(ctx)
	if got := s2.Info(); !reflect.DeepEqual(got.MeManual, []string{"old@example.com"}) || len(got.Me) != 3 {
		t.Fatalf("reopened: %+v", got)
	}

	// removing the manual ones leaves the derived; a bad address is refused
	if err := s2.SetMe([]string{"nope"}); err == nil {
		t.Error("invalid email accepted")
	}
	if err := s2.SetMe(nil); err != nil {
		t.Fatal(err)
	}
	if got := s2.Info(); len(got.MeManual) != 0 || len(got.Me) != 2 {
		t.Fatalf("emptied: %+v", got)
	}

	// a file from before: meSet true = the whole list was chosen, so it is the manual list; meSet false = a derived copy, dropped
	for _, c := range []struct {
		json string
		want []string
	}{
		{`{"me":["x@example.com"],"meSet":true}`, []string{"x@example.com"}},
		{`{"me":["me@example.com","work@corp.com"]}`, nil},
	} {
		dir := t.TempDir()
		o := opts
		o.DataDir = dir
		cfg := filepath.Join(dir, "workspaces", workspaceID(root), "workspace.json")
		os.MkdirAll(filepath.Dir(cfg), 0o755)
		os.WriteFile(cfg, []byte(c.json), 0o644)
		s4, _ := Open(ctx, root, o)
		t.Cleanup(s4.Close)
		if !reflect.DeepEqual(s4.cfg.MeManual, c.want) || s4.cfg.OldMeSet {
			t.Errorf("%s: cfg.MeManual = %v", c.json, s4.cfg.MeManual)
		}
	}

}

func TestStatePhase(t *testing.T) {
	for _, c := range []struct {
		st   State
		want string
	}{
		{State{}, "loading"},
		{State{Indexing: true}, "loading"},
		{State{Indexing: true, Total: 5}, "indexing"},
		{State{Ready: true, Indexing: true, Total: 5}, "updating"},
		{State{Ready: true}, "ready"},
	} {
		c.st.setPhase()
		if c.st.Phase != c.want {
			t.Errorf("%+v: phase %q, want %q", c.st, c.st.Phase, c.want)
		}
	}
}

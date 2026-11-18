package index

import (
	"context"
	"os"
	"testing"
)

func TestUpdateReportsUnreadableRepo(t *testing.T) {
	f := newFixture(t)
	repos := scan(t, f.root)
	old, err := Build(context.Background(), repos, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repos[1].GitDir); err != nil {
		t.Fatal(err)
	}
	for _, prev := range []*Index{old, nil} {
		ix, delta, err := Update(context.Background(), prev, repos, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(delta.Failed) != 1 || delta.Failed[0].Repo != "b" || delta.Failed[0].Err == nil {
			t.Fatalf("failures: %+v", delta.Failed)
		}
		if prev == nil {
			if len(ix.Repos) != 1 || ix.Repos[0].Name != "a" {
				t.Fatal("expected only healthy repo on first build")
			}
		} else if ix.Commits() != old.Commits() || ix.Repos[1] != old.Repos[1] {
			t.Fatal("lost old history")
		}
	}
}

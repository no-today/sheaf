package session

import (
	"path/filepath"
	"reflect"
	"testing"

	"sheaf/internal/discover"
	"sheaf/internal/gitcli"
)

func TestScanCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w", "scan.json")
	if _, ok := loadScan(path); ok {
		t.Fatal("no file is no list")
	}
	want := []discover.Repo{{Name: "a", Dir: "/r/a", GitDir: "/r/a/.git", Kind: discover.KindRepo,
		Worktrees: []discover.Worktree{{Path: "/r/a", Branch: "refs/heads/master", Head: gitcli.Oid{1, 2}, Main: true}}}}
	saveScan(path, want)
	got, ok := loadScan(path)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v %+v", ok, got)
	}
	saveScan(path, nil) // an empty scan is not kept as the list to show
	if _, ok := loadScan(path); ok {
		t.Error("empty list loaded")
	}
}

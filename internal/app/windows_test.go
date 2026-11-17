package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestClaim(t *testing.T) {
	m := newWindows(nil, nil, nil)
	if w, _, ok := m.claim("/a"); w != nil || !ok {
		t.Fatal("first claim refused")
	}
	if _, _, ok := m.claim("/a"); ok {
		t.Error("a root being opened was claimed twice")
	}
	if _, _, ok := m.claim("/b"); !ok {
		t.Error("another root refused")
	}
	m.release("/a")
	if _, _, ok := m.claim("/a"); !ok {
		t.Error("released root refused")
	}
}

func TestColdStart(t *testing.T) {
	for _, tc := range []struct {
		name     string
		explicit []string
		open     []string
		recent   []string // newest first
		failed   []string
		want     []string
		choose   int
	}{
		{name: "restore all last open workspaces", open: []string{"a", "b"}, recent: []string{"c", "b", "a"}, want: []string{"a", "b"}},
		{name: "only recents opens the newest", recent: []string{"b", "a"}, want: []string{"b"}},
		{name: "no state chooses a folder", choose: 1},
		{name: "missing open folder falls back to recent", open: []string{"gone"}, recent: []string{"b", "a"}, want: []string{"b"}},
		{name: "skip missing recent folder", recent: []string{"gone", "a"}, want: []string{"a"}},
		{name: "all folders missing chooses a folder", open: []string{"gone"}, recent: []string{"gone"}, choose: 1},
		{name: "failed restore tries the recent folder", open: []string{"a"}, recent: []string{"a", "b"}, failed: []string{"a"}, want: []string{"a", "b"}},
		{name: "all opens fail chooses a folder", open: []string{"a"}, recent: []string{"a", "b"}, failed: []string{"a", "b"}, want: []string{"a", "b"}, choose: 1},
		{name: "explicit roots override restoration", explicit: []string{"c"}, open: []string{"a"}, recent: []string{"b"}, want: []string{"c"}},
		{name: "failed explicit root chooses a folder", explicit: []string{"a"}, recent: []string{"b"}, failed: []string{"a"}, want: []string{"a"}, choose: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"a", "b", "c"} {
				if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "recent.json")
			recents := openRecents(path, time.Now)
			for i := len(tc.recent) - 1; i >= 0; i-- {
				if err := recents.Touch(filepath.Join(dir, tc.recent[i]), tc.recent[i]); err != nil {
					t.Fatal(err)
				}
			}
			for _, root := range tc.open {
				if err := recents.MarkOpen(filepath.Join(dir, root)); err != nil {
					t.Fatal(err)
				}
			}
			recents.Freeze()
			for _, root := range tc.open {
				if err := recents.MarkClosed(filepath.Join(dir, root)); err != nil {
					t.Fatal(err)
				}
			}
			m := newWindows(nil, nil, openRecents(path, time.Now))
			var explicit, got []string
			for _, root := range tc.explicit {
				explicit = append(explicit, filepath.Join(dir, root))
			}
			chosen := 0
			m.start(explicit, func(root string) error {
				name := filepath.Base(root)
				got = append(got, name)
				for _, failed := range tc.failed {
					if name == failed {
						return errors.New("unavailable workspace")
					}
				}
				return nil
			}, func() { chosen++ })
			if !reflect.DeepEqual(got, tc.want) || chosen != tc.choose {
				t.Fatalf("opened %v, chose folder %d times; want %v, %d", got, chosen, tc.want, tc.choose)
			}
		})
	}
}

func TestClosingLastWorkspaceLeavesNoWindow(t *testing.T) {
	m := newWindows(nil, nil, nil)
	win := &mygo.Window{}
	m.byRoot["/a"] = win
	m.workspaceClosed("/a", win)
	if len(m.byRoot) != 0 || len(m.opening) != 0 {
		t.Fatal("closing the last workspace opened another window")
	}
}

package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { c.t = c.t.Add(time.Minute); return c.t }

func roots(l []Recent) []string {
	out := []string{}
	for _, r := range l {
		out = append(out, r.Root)
	}
	return out
}

func TestRecentOrderAndPersistence(t *testing.T) {
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c")
	os.Mkdir(a, 0o755)
	os.Mkdir(b, 0o755)
	path := filepath.Join(dir, "state", "recent.json")
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openRecents(path, clk.now)
	if l := s.List(); len(l) != 0 {
		t.Fatalf("empty recent list: %#v", l)
	}
	for _, root := range []string{a, b, c, a} {
		if err := s.Touch(root, filepath.Base(root)); err != nil {
			t.Fatal(err)
		}
	}
	l := s.List()
	if !reflect.DeepEqual(roots(l), []string{a, c, b}) {
		t.Fatalf("order: %+v", l)
	}
	if !l[1].Missing || l[0].Missing {
		t.Errorf("missing flag: %+v", l)
	}
	if got := openRecents(path, clk.now).List(); !reflect.DeepEqual(got, l) {
		t.Errorf("reloaded: %+v, want %+v", got, l)
	}
	if err := os.WriteFile(c, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.List()[1].Missing {
		t.Error("a regular file is not an available workspace")
	}
}

func TestRecentCap(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openRecents(filepath.Join(t.TempDir(), "recent.json"), clk.now)
	for i := 0; i < maxRecents+5; i++ {
		s.Touch(filepath.Join("/w", string(rune('a'+i%26)), string(rune('a'+i/26))), "x")
	}
	if n := len(s.List()); n != maxRecents {
		t.Errorf("kept %d", n)
	}
}

func TestOpenSetAndFreeze(t *testing.T) {
	dir := t.TempDir()
	a, b, gone := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "gone")
	os.Mkdir(a, 0o755)
	os.Mkdir(b, 0o755)
	path := filepath.Join(dir, "recent.json")
	s := openRecents(path, time.Now)
	for _, r := range []string{a, b, gone, a} {
		s.MarkOpen(r)
	}
	s.MarkClosed(b)
	s.Freeze()
	s.MarkClosed(a)
	s.MarkClosed(gone)
	if got := openRecents(path, time.Now).OpenRoots(); !reflect.DeepEqual(got, []string{a}) {
		t.Errorf("open roots: %v", got)
	}
	if got := openRecents(filepath.Join(dir, "none.json"), time.Now).OpenRoots(); len(got) != 0 {
		t.Errorf("no file: %#v", got)
	}
}

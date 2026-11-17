package diffview

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/session"
)

// A binary file changed by several commits is binary; a binary part among text parts is named in a note.
func TestBinaryParts(t *testing.T) {
	bin := session.Patch{Binary: true}
	both := session.AggPatch{Mode: "commits", Parts: []session.AggPart{{Hash: "1111111aa", Subject: "a", Patch: bin}, {Hash: "2222222bb", Subject: "b", Patch: bin}}}
	if tx := TextsOfAgg(both); !tx.Binary || tx.Parts != nil {
		t.Errorf("every part binary: %+v", tx)
	}
	p := session.Patch{Hunks: []session.Hunk{hunk("@@ -1 +1 @@", session.Line{Kind: "-", Old: 1, Text: "a"}, session.Line{Kind: "+", New: 1, Text: "b"})}}
	tx := TextsOfAgg(session.AggPatch{Mode: "commits", Parts: []session.AggPart{{Hash: "1111111aa", Subject: "logo", Patch: bin}, {Hash: "2222222bb", Subject: "text", Patch: p}}})
	if tx.Binary || len(tx.Parts) != 2 {
		t.Fatalf("mixed parts: %+v", tx)
	}
	s := New([]File{{Path: "f", Status: "M", Texts: &tx}}, nil)
	tt := ui.NewTester(func(c *ui.Context) { View(c, s) }, 900, 400)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	tt.Frame()
	if !tt.HasText("无法预览：1111111  logo") || !tt.HasText("2222222  text") {
		t.Errorf("texts %q", tt.Texts())
	}
}

// load is a load that runs until it is stopped.
type load struct {
	path, owner string
	done        <-chan struct{}
}

func (l load) stopped() bool {
	select {
	case <-l.done:
		return true
	case <-time.After(time.Second):
		return false
	}
}

// Loads run as their State (ownerOf: two States on one file do not supersede each other's session calls), and a file
// stepped past while it loads stops loading; shown again, it loads again.
func TestLoadsBelongToTheirStateAndStopWhenSteppedPast(t *testing.T) {
	loads := make(chan load, 32)
	loader := func(ctx context.Context, f File) (Texts, error) {
		loads <- load{f.Path, ownerOf(ctx), ctx.Done()}
		<-ctx.Done()
		return Texts{}, ctx.Err()
	}
	next := func() load {
		t.Helper()
		select {
		case l := <-loads:
			return l
		case <-time.After(2 * time.Second):
			t.Fatal("no load started")
		}
		return load{}
	}
	files := []File{{Path: "0"}, {Path: "1"}, {Path: "2"}, {Path: "3"}}
	a, b := New(files, loader), New(files, loader)
	defer a.Close()
	defer b.Close()
	ta := tester(a) // shows file 0: loads 0 and 1
	tester(b)
	ownA, ownB := fmt.Sprintf("%p", a), fmt.Sprintf("%p", b)
	got := map[string]load{}
	for range 4 {
		l := next()
		got[l.owner+" "+l.path] = l
	}
	for _, k := range []string{ownA + " 0", ownA + " 1", ownB + " 0", ownB + " 1"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("no load %q; got %v", k, got)
		}
	}
	// a steps to file 2: its loads of 0 and 1 stop, b's go on.
	a.Select(2)
	ta.Frame()
	for _, p := range []string{"0", "1"} {
		if !got[ownA+" "+p].stopped() {
			t.Errorf("a's load of %s still runs", p)
		}
		select {
		case <-got[ownB+" "+p].done:
			t.Errorf("b's load of %s stopped", p)
		default:
		}
	}
	for range 2 {
		if l := next(); l.owner != ownA || (l.path != "2" && l.path != "3") {
			t.Errorf("load %+v", l)
		}
	}
	// Back on file 0, it loads again.
	a.Select(0)
	ta.Frame()
	seen := map[string]bool{}
	for range 2 {
		seen[next().path] = true
	}
	if !seen["0"] {
		t.Errorf("file 0 not loaded again: %v", seen)
	}
}

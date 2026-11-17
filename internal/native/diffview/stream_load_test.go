package diffview

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// captureStreamArrival holds a completed load before the UI applies it, so replacement and release need no timing race.
func captureStreamArrival(t *testing.T, s *Stream, i int, text string) streamArrival {
	t.Helper()
	s.load = func(context.Context, File) (Texts, error) {
		return Texts{Old: "base\n", New: text}, nil
	}
	s.fetch(ui.Services{}, i, s.item(i))
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		s.mu.Lock()
		if len(s.arrived) > 0 {
			a := s.arrived[0]
			s.arrived = s.arrived[1:]
			s.mu.Unlock()
			return a
		}
		s.mu.Unlock()
	}
	t.Fatal("the load did not return")
	return streamArrival{}
}

func settleStreamArrivals(s *Stream, arrivals ...streamArrival) {
	s.mu.Lock()
	s.arrived = append(s.arrived, arrivals...)
	s.mu.Unlock()
	s.settle()
}

func TestStreamPauseKeepsReadContentAndStopsNewLoads(t *testing.T) {
	s := NewStream()
	defer s.Close()
	s.SetFiles(streamOf(3, nil), nil)
	read := captureStreamArrival(t, s, 0, "reviewed text\n")
	settleStreamArrivals(s, read)
	pending := captureStreamArrival(t, s, 1, "outdated pending load\n")
	requested := s.Requested
	s.SetPaused(true)
	settleStreamArrivals(s, pending)
	s.schedule(ui.Services{}, 0, len(s.rows)-1)
	if s.Requested != requested || s.item(1).ready || s.item(1).pending {
		t.Fatal("paused stream loaded content after the change")
	}
	requireStreamText(t, s, 0, "reviewed text\n")
	s.SetPaused(false)
	fresh := captureStreamArrival(t, s, 1, "refreshed text\n")
	settleStreamArrivals(s, fresh)
	requireStreamText(t, s, 1, "refreshed text\n")
}

func requireStreamText(t *testing.T, s *Stream, i int, want string) {
	t.Helper()
	it := s.item(i)
	if it == nil || it.pending || !it.ready || it.err != nil || it.diff == nil {
		t.Fatalf("file %d did not finish its current request: %+v", i, it)
	}
	if got := it.diff.New.String(); got != want {
		t.Fatalf("file %d shows %q, want %q", i, got, want)
	}
}

func TestStreamArrivalBelongsToRequest(t *testing.T) {
	for _, change := range []string{"revision changed", "removed and readded", "same item reloaded", "scrolled away and back"} {
		t.Run(change, func(t *testing.T) {
			s := NewStream()
			defer s.Close()
			files := streamOf(20, nil)
			for i := 1; i < len(files); i++ {
				files[i].Binary = true
			}
			s.SetFiles(files, nil)
			old := captureStreamArrival(t, s, 0, "old request\n")
			switch change {
			case "revision changed":
				files[0].Rev = "2"
				s.SetFiles(files, nil)
			case "removed and readded":
				s.SetFiles(nil, nil)
				s.SetFiles(files, nil)
			case "scrolled away and back":
				row := s.fileRow[len(files)-1]
				s.schedule(ui.Services{}, row, row)
				if s.item(0) != nil {
					t.Fatal("the distant request was not released")
				}
			}
			fresh := captureStreamArrival(t, s, 0, "current request\n")
			settleStreamArrivals(s, old)
			if it := s.item(0); !it.pending || it.diff != nil || it.ready {
				t.Fatalf("an old arrival completed the current request: %+v", it)
			}
			settleStreamArrivals(s, fresh)
			requireStreamText(t, s, 0, "current request\n")
		})
	}
}

func TestStreamPreservesUnchangedRequest(t *testing.T) {
	s := NewStream()
	defer s.Close()
	files := streamOf(2, nil)
	s.SetFiles(files, nil)
	old := captureStreamArrival(t, s, 0, "old request\n")
	kept := captureStreamArrival(t, s, 1, "kept request\n")
	files[0].Rev = "2"
	s.SetFiles(files, nil)
	fresh := captureStreamArrival(t, s, 0, "current request\n")
	settleStreamArrivals(s, old, kept, fresh)
	requireStreamText(t, s, 0, "current request\n")
	requireStreamText(t, s, 1, "kept request\n")
}

func TestStreamEvictedItemIgnoresLateArrival(t *testing.T) {
	s := NewStream()
	defer s.Close()
	files := streamOf(Keep+1, nil)
	s.SetFiles(files, nil)
	for _, f := range files {
		s.items[f.Key] = &streamItem{rev: f.Rev, ready: true, diff: &editor.DiffState{}}
	}
	old := captureStreamArrival(t, s, 0, "released request\n")
	it := s.item(0)
	last := s.fileRow[len(files)-1]
	s.evict(last, last)
	if s.item(0) != it || it.diff != nil || it.ready {
		t.Fatal("the farthest diff was not released while retaining its item")
	}
	settleStreamArrivals(s, old)
	if it.diff != nil || it.ready || it.pending {
		t.Fatalf("the late arrival revived a released item: %+v", it)
	}
	fresh := captureStreamArrival(t, s, 0, "reloaded request\n")
	settleStreamArrivals(s, old, fresh)
	requireStreamText(t, s, 0, "reloaded request\n")
}

func TestStreamCloseIgnoresQueuedArrival(t *testing.T) {
	s := NewStream()
	defer s.Close()
	s.SetFiles(streamOf(1, nil), nil)
	old := captureStreamArrival(t, s, 0, "closed request\n")
	s.Close()
	settleStreamArrivals(s, old)
	if it := s.item(0); it.diff != nil || it.ready {
		t.Fatalf("an arrival was applied after close: %+v", it)
	}
}

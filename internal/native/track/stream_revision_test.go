package track

import (
	"encoding/json"
	"testing"
	"time"

	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

func unclassifiedRequirement(t *testing.T) *harness {
	t.Helper()
	h := viewtest.Open(t, reqFixture(t))
	h.St.Ready = false
	h.DockOf = Dock()
	hr := viewtest.Mount(t, h, "zh-Hans", func(h view.Host) view.View { return New(h) })
	p := hr.V.(*Page)
	h.St.Ready = true
	p.want = tmpID
	cond := query.Default()
	cond.Q = "YT-2841"
	p.temp = &tempFilter{Req: cond.Q, Cond: cond}
	p.input = p.inputFor(tmpID)
	data, _ := json.Marshal(p.input)
	p.loaded = string(data)
	res, err := h.S.TagAgg(t.Context(), *p.input)
	if err != nil {
		t.Fatal(err)
	}
	p.res = &res
	p.rebuildRows()
	p.onRows()
	p.files, err = h.S.TagAggFiles(t.Context(), *p.input)
	if err != nil {
		t.Fatal(err)
	}
	p.filesIn, p.filesVer = *p.input, 1
	p.stats = map[string]map[string]session.FileStat{}
	p.statsFor = p.loaded
	for _, row := range p.rows {
		stats, err := h.S.TagAggStats(t.Context(), *p.input, row.Key)
		if err != nil {
			t.Fatal(err)
		}
		p.stats[row.Key] = map[string]session.FileStat{}
		for _, stat := range stats {
			p.stats[row.Key][stat.Path] = stat
		}
	}
	return &harness{hr, p}
}

func TestRequirementStreamRemergePreservesUnchangedDiffs(t *testing.T) {
	h := unclassifiedRequirement(t)
	p := h.p
	h.TT.SetSize(1440, 900)
	h.Until("unclassified first screen", func() bool {
		return p.stream.Loaded() > 0 && !h.TT.FramePending()
	})
	revs := map[string]string{}
	for _, file := range p.stream.Files() {
		revs[file.Key] = file.Rev
	}
	requested := p.stream.Requested
	p.loadRemerge(*p.input, p.aggSeq)
	h.Until("classified first screen", func() bool {
		return p.filesSeq > 0 && !p.aggBusy && !p.filesBusy && p.stream.Loaded() > 0 && !h.TT.FramePending()
	})
	index := fileAt(t, p, "pay-core", "src/settle/SettleOrder.java")
	h.Until("classified merge part displayed", func() bool { return p.stream.Resolved(index) })
	for _, file := range p.stream.Files() {
		before, exists := revs[file.Key]
		if !exists || before == file.Rev {
			continue
		}
		t.Logf("revision changed: %s: %q -> %q", file.Path, before, file.Rev)
		if file.Path != "src/settle/SettleOrder.java" {
			t.Errorf("merge classification invalidated unchanged %s", file.Path)
		}
	}
	if extra := p.stream.Requested - requested; extra != 1 {
		t.Errorf("merge classification requested %d additional patches; only SettleOrder changed", extra)
	}
}

func TestRequirementStreamWaitsForStatsBeforeSwitching(t *testing.T) {
	h := unclassifiedRequirement(t)
	p := h.p
	stats := p.stats
	p.stats = map[string]map[string]session.FileStat{}
	p.streamKey = ""
	previous := *p
	p.prev = &previous
	for _, row := range p.rows {
		h.TT.Frame()
		if p.prev == nil || p.settled() || p.stream.Requested != 0 {
			t.Fatal("the page switched or requested patches before every row's line counts arrived")
		}
		p.stats[row.Key] = stats[row.Key]
	}
	h.TT.SetSize(1440, 900)
	h.Until("first diff after every row's line counts arrived", func() bool {
		return p.prev == nil && p.stream.Loaded() > 0 && !h.TT.FramePending()
	})
	requested := p.stream.Requested
	revs := map[string]string{}
	for _, file := range p.stream.Files() {
		revs[file.Key] = file.Rev
	}
	first := p.stream.Files()[0].Source.(session.AggFile)
	stat := p.stats[first.Key][first.Path]
	// Reading the same line counts again must not read the diffs again; changed counts would (the content changed).
	held := &heldTrackUpdates{Host: p.h, updates: make(chan func(), 32)}
	p.h = held
	p.loadStats(*p.input)
	for range len(p.rows) + 1 {
		held.next(t)()
	}
	p.h = held.Host
	h.TT.Frame()
	if p.stream.Files()[0].Lines != stat.Add+stat.Del {
		t.Fatal("line counts read again changed the file's layout")
	}
	for _, file := range p.stream.Files() {
		if file.Rev != revs[file.Key] {
			t.Errorf("unchanged line counts invalidated %s", file.Path)
		}
	}
	if p.stream.Requested != requested {
		t.Error("unchanged line counts reloaded displayed diffs")
	}
}

type heldTrackUpdates struct {
	view.Host
	updates chan func()
}

func (h *heldTrackUpdates) Update(f func()) { h.updates <- f }

func (h *heldTrackUpdates) next(t *testing.T) func() {
	t.Helper()
	select {
	case f := <-h.updates:
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a held track update")
		return nil
	}
}

func TestRequirementStreamErrorsRetirePendingReads(t *testing.T) {
	for _, stagesOnly := range []bool{false, true} {
		name := "aggregate"
		if stagesOnly {
			name = "stages"
		}
		t.Run(name, func(t *testing.T) {
			h := unclassifiedRequirement(t)
			p := h.p
			held := &heldTrackUpdates{Host: p.h, updates: make(chan func(), 32)}
			p.h = held
			p.loadRemerge(*p.input, p.aggSeq)
			oldMerge := held.next(t)
			p.loadFiles(*p.input)
			oldFiles := held.next(t)
			if !p.filesBusy {
				t.Fatal("file read was not pending")
			}
			p.temp.Cond.Q, p.temp.Cond.Regex = "[", true
			if stagesOnly {
				data, _ := json.Marshal(p.inputFor(tmpID))
				p.loaded = string(data)
			}
			p.applyAggregate(true, stagesOnly)
			held.next(t)()
			if p.aggErr == "" || p.aggBusy || p.filesBusy || !p.settled() {
				t.Fatalf("failed read did not settle: error=%q aggregate=%v files=%v settled=%v", p.aggErr, p.aggBusy, p.filesBusy, p.settled())
			}
			err := p.aggErr
			oldFiles()
			oldMerge()
			if p.aggErr != err || p.aggBusy || p.filesBusy || p.res != nil {
				t.Fatal("superseded callbacks revived a failed aggregate")
			}
			p.h = held.Host
			p.temp.Cond.Q, p.temp.Cond.Regex = "YT-2841", false
			p.loaded = ""
			p.apply(false)
			h.Until("diff after recovering from a failed read", func() bool {
				return p.aggErr == "" && p.settled() && p.stream.Loaded() > 0
			})
		})
	}
}

func TestRequirementStreamMetadataErrorsAllowFirstDiffAfterReplies(t *testing.T) {
	h := unclassifiedRequirement(t)
	p := h.p
	held := &heldTrackUpdates{Host: p.h, updates: make(chan func(), 32)}
	p.h = held
	p.stats = nil
	in := *p.input
	cond := *in.Cond
	cond.Q, cond.Regex = "[", true
	in.Cond = &cond
	p.loadStats(in)
	var callbacks []func()
	for range len(p.rows) + 1 {
		callbacks = append(callbacks, held.next(t))
	}
	p.loadRemerge(in, p.aggSeq)
	callbacks = append(callbacks, held.next(t))
	h.TT.Frame()
	if p.settled() || p.stream.Requested != 0 {
		t.Fatal("pending line counts did not defer the first diff")
	}
	for _, callback := range callbacks {
		callback()
	}
	h.Until("first diff after failed metadata replies", func() bool {
		return p.settled() && p.stream.Loaded() > 0
	})
}

func TestRequirementStreamFileReadErrorSettles(t *testing.T) {
	h := unclassifiedRequirement(t)
	p := h.p
	in := *p.input
	cond := *in.Cond
	cond.Q, cond.Regex = "[", true
	in.Cond = &cond
	p.loadFiles(in)
	h.Until("file read failure", func() bool { return p.filesErr != "" })
	if p.filesBusy || !p.settled() {
		t.Fatal("failed file read kept the page busy")
	}
	p.loadFiles(*p.input)
	h.Until("file read recovery", func() bool {
		return p.filesErr == "" && !p.filesBusy && p.stream.Loaded() > 0
	})
}

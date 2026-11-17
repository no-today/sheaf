package track

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/session"
)

func TestUnassignedFileTableFollowsStreamAndJumps(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := openDirty(t, locale)
			h.TT.SetSize(1440, 420)
			d, w := &h.p.dirty, wordsOf(h)
			shotClick(t, h, w.TabFiles+" 4")
			var rows []string
			for _, a := range h.TT.AccessAll() {
				if strings.HasPrefix(a.Name, "file ") {
					rows = append(rows, a.Name)
				}
			}
			var want []string
			for _, f := range d.stream.Files() {
				want = append(want, "file "+f.Group+"/"+f.Path)
			}
			if !slices.Equal(rows, want) || len(rows) != 4 {
				t.Fatalf("file table rows %q, want %q", rows, want)
			}
			for _, i := range []int{3, 2, 1, 0, 0} {
				shotClick(t, h, rows[i])
				h.Until("file selected in stream", func() bool { return d.stream.Current() == i && d.tab == 0 })
				shotClick(t, h, w.TabFiles+" 4")
			}
		})
	}
}

func TestUnassignedFileCountsUseNetHeadDiff(t *testing.T) {
	h := openDirty(t, "zh-Hans")
	d := &h.p.dirty
	shotClick(t, h, "文件 4")
	for _, f := range d.stream.Files() {
		file := f.Source.(session.AggFile)
		var stat dirtyFileStat
		h.Until("file line counts", func() bool {
			stat = d.fileStat(h.p, -1, file)
			return stat.ready
		})
		wantDel := 1
		if file.Untracked {
			wantDel = 0
		}
		if stat.add != 1 || stat.del != wantDel {
			t.Errorf("%s/%s: +%d −%d, want +1 −%d", file.Repo, file.Path, stat.add, stat.del, wantDel)
		}
	}
}

func TestUnassignedSplitDefaultAndResizeStayWithinPage(t *testing.T) {
	h := openDirty(t, "zh-Hans")
	d := &h.p.dirty
	initial := d.topHeight
	var divider ui.Rect
	for _, a := range h.TT.AccessAll() {
		if a.Role == ui.RoleSplitter && a.Bounds.W > a.Bounds.H {
			divider = a.Bounds
		}
	}
	if divider.H != 1 || d.topHeight < 134 || d.topHeight > 900*0.4 {
		t.Fatalf("default split height %v, divider %+v", d.topHeight, divider)
	}
	for _, a := range h.TT.AccessAll() {
		if a.Role == ui.RoleRow && a.Bounds.Y < divider.Y && a.Bounds.Y+a.Bounds.H > divider.Y {
			t.Errorf("directory row clipped by default split: %+v, divider %+v", a, divider)
		}
	}
	h.TT.ClickAt(divider.X+divider.W/2, divider.Y)
	h.TT.Key(0, ui.KeyDown)
	h.TT.Frame()
	resized := d.topHeight
	if !d.resized || resized <= initial {
		t.Fatalf("divider did not resize: %v", resized)
	}
	shotClick(t, h, "文件 4")
	h.TT.SetSize(1180, 760)
	if d.topHeight != resized {
		t.Fatalf("split height reset from %v to %v", resized, d.topHeight)
	}
}

func TestUnassignedDirectoryHeadUsesFileClassifier(t *testing.T) {
	if got := fill(tr.For("zh-Hans").WcFileCount, "n", 1); got != "1 个文件" {
		t.Errorf("Chinese directory count %q", got)
	}
	if got := fill(tr.For("en-US").WcFileCount, "n", 1); got != "1 file" {
		t.Errorf("English directory count %q", got)
	}
}

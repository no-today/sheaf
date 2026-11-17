package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

type nativeAuxCall struct {
	what string
	opts mygo.WindowOptions
	make nativeMaker
}

type nativeAuxRecorder struct {
	calls []nativeAuxCall
}

func (r *nativeAuxRecorder) openAuxOf(_ *mygo.Window, what string, opts mygo.WindowOptions, make nativeMaker) error {
	r.calls = append(r.calls, nativeAuxCall{what, opts, make})
	return nil
}

func TestNativeDiffAndSettingsUseWorkspaceSession(t *testing.T) {
	h := viewtest.Open(t, viewtest.Fixture(t))
	var parent *mygo.Window
	owner := &nativeAuxRecorder{}
	wins := newWindows(nil, openSettings(filepath.Join(t.TempDir(), "settings.json")), nil)
	n := nativeWindows{ws: owner, wins: wins, win: &parent}
	res, err := h.S.TagAggCommits(session.TagAggInput{Cond: &query.Conditions{Q: "second in a", Authors: []string{}}}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := h.S.Rows(res.ResultID, 0, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("source commit: %+v, %v", rows, err)
	}
	src := session.DiffSource{Kind: "commit", Repo: "repo-a", Hash: rows[0].Hash, Path: "a.txt"}
	if err := n.OpenDiff(src); err != nil {
		t.Fatal(err)
	}
	if err := n.OpenSettings("index"); err != nil {
		t.Fatal(err)
	}
	if len(owner.calls) != 2 || owner.calls[0].opts.StateKey != "diff" || owner.calls[1].opts.StateKey != "settings" {
		t.Fatalf("auxiliary windows: %+v", owner.calls)
	}
	for i, call := range owner.calls {
		content := call.make(h.S, h.Update)
		t.Cleanup(content.Close)
		tt := ui.NewTester(content.View, 1200, 780)
		tt.SetLocale("zh-Hans")
		ready := false
		for range 400 {
			h.Drain()
			tt.Frame()
			if i == 0 {
				ready = tt.HasText("second in a") && tt.HasText("a.txt") && tt.HasText("并排") && tt.HasText("单列")
			} else {
				ready = tt.HasText("查看记录") && tt.HasText("2 个仓库、0 个工作目录，有改动自动更新")
			}
			if ready {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !ready {
			t.Fatalf("window %q did not load from its workspace: %q", call.what, tt.Texts())
		}
		for _, removed := range []string{"文件历史", "显示作者", "隐藏作者"} {
			if _, shown := tt.Find(removed); shown {
				t.Errorf("window %q still offers %q", call.what, removed)
			}
		}
	}
}

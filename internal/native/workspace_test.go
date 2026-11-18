package native

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/shell"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
)

type queue struct {
	mu sync.Mutex
	fs []func()
}

func (q *queue) push(f func()) { q.mu.Lock(); q.fs = append(q.fs, f); q.mu.Unlock() }

func (q *queue) drain() {
	q.mu.Lock()
	fs := q.fs
	q.fs = nil
	q.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

type harness struct {
	t    *testing.T
	w    *Workspace
	s    *session.Session
	tt   *ui.Tester
	q    *queue
	wins *viewtest.Windows
}

func (h *harness) until(what string, ok func() bool) {
	h.t.Helper()
	for range 400 {
		h.q.drain()
		h.tt.Frame()
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s; texts %q", what, h.tt.Texts())
}

func openSession(t *testing.T, root, data string, w *Workspace) *session.Session {
	t.Helper()
	s, err := session.Open(context.Background(), root, session.Options{DataDir: data, CacheDir: t.TempDir(), Now: time.Now, OnState: w.State, OnIndex: w.Index})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func open(t *testing.T) *harness {
	return openAt(t, viewtest.Fixture(t), t.TempDir())
}

func openAt(t *testing.T, root, data string) *harness {
	q, wins := &queue{}, &viewtest.Windows{}
	w := New(q.push, wins)
	w.smoke = smoke{}
	t.Cleanup(w.Close)
	s := openSession(t, root, data, w)
	w.Attach(s)
	h := &harness{t: t, w: w, s: s, q: q, tt: ui.NewTester(w.View, 1440, 900), wins: wins}
	h.tt.SetLocale("zh-Hans")
	h.until("the requirement page", func() bool { return h.tt.HasText("输入需求号开始关注") })
	return h
}

func TestOpensOnTheRequirementPage(t *testing.T) {
	h := open(t)
	if h.w.active != view.Track || len(h.w.views) != 1 || !h.tt.HasText("未提交") {
		t.Fatalf("active %q, made %d views; texts %q", h.w.active, len(h.w.views), h.tt.Texts())
	}
	for _, label := range []string{"提交记录", "仓库范围", "2 个仓库", "隐藏"} {
		if _, ok := h.tt.Find(label); ok {
			t.Errorf("removed control %q is still present", label)
		}
	}
	select {
	case <-h.w.Painted():
	default:
		t.Error("first content not marked painted")
	}
}

func TestWorkspacePassesActualVisibilityToReview(t *testing.T) {
	h := open(t)
	dir := filepath.Join(h.s.Root, "repo-a")
	file := filepath.Join(dir, "pending.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.s.RefreshDirty(t.Context(), []string{dir}, true)
	h.w.Go(view.Track, view.UncommittedIntent{})
	h.until("uncommitted review", func() bool { return h.tt.HasText("+1") && !h.tt.HasText("更新中") && !h.tt.FramePending() })
	if err := os.WriteFile(file, []byte("after\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.s.RefreshDirty(t.Context(), []string{dir}, true)
	h.w.Worktrees(session.WorktreeStatus{Worktrees: []string{dir}})
	h.until("pending changes", func() bool { return h.tt.HasText("内容已变化 · 刷新") })
	h.w.SetVisible(false)
	h.until("updated in hidden window", func() bool {
		return h.tt.HasText("+2") && !h.tt.HasText("更新中") && !h.tt.HasText("内容已变化 · 刷新") && !h.tt.FramePending()
	})
	h.w.SetVisible(true)
	h.tt.Frame()
	if !h.tt.HasText("+2") || h.tt.HasText("内容已变化 · 刷新") || h.tt.HasText("已是最新") {
		t.Fatal("returning to the window did not show the latest diff")
	}
}

func TestGoReusesTheRequirementPage(t *testing.T) {
	h := open(t)
	p := h.w.views[view.Track].v
	h.w.Go(view.Track, view.TempIntent{Branch: "master"})
	h.until("the branch", func() bool { return h.tt.HasText("master") })
	h.w.Go(view.ID("log"), nil)
	if h.w.views[view.Track].v != p || h.w.active != view.Track || len(h.w.views) != 1 {
		t.Fatal("navigation recreated the requirement page or mounted a removed view")
	}
}

func TestDockSizesAreKeptAndHiddenRailStaysHidden(t *testing.T) {
	root, data := viewtest.Fixture(t), t.TempDir()
	h := openAt(t, root, data)
	d := h.w.views[view.Track].h.dock
	d.Left.Size, d.Left.Open = 280, false
	d.Splits[0], d.Splits[1] = 320, 300 // the requirement page's own dividers
	h.w.layout.changed()
	h.w.Close()
	b, err := os.ReadFile(filepath.Join(h.s.DataDir(), layoutFile))
	if err != nil {
		t.Fatal(err)
	}
	var saved layoutJSON
	if err := json.Unmarshal(b, &saved); err != nil || saved.Docks[view.Track].Left.Size != 280 || saved.Docks[view.Track].Splits[1] != 300 {
		t.Fatalf("saved %s (%v)", b, err)
	}
	w := New(h.q.push, nil)
	t.Cleanup(w.Close)
	w.smoke = smoke{}
	w.Attach(h.s)
	tt := ui.NewTester(w.View, 1440, 900)
	tt.Frame()
	d = w.views[view.Track].h.dock
	if d.Left.Open || d.Left.Size != 280 || d.Splits[0] != 320 || d.Splits[1] != 300 {
		t.Fatalf("saved rail lost its state or sizes: %+v", d)
	}
	tt.SetLocale("zh-Hans")
	tt.Frame()
	if err := tt.Click(i18n.Common.For("zh-Hans").ShowLeft + "  ⌘B"); err != nil {
		t.Fatal(err)
	}
	if tt.Frame(); !d.Left.Open {
		t.Fatal("the title bar button does not reopen the rail")
	}
}

func TestDockOfOlderDefaultsIsDropped(t *testing.T) {
	dir := t.TempDir()
	old := `{"docks":{"track":{"left":{"size":300,"open":false},"right":{"size":340,"open":true},"bottom":{"size":260,"open":false}}}}`
	if err := os.WriteFile(filepath.Join(dir, layoutFile), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	dock := func(l *layout) *shell.Dock { return l.dock(view.Track, def(view.Track).dock, def(view.Track).dockVer) }
	l := openLayout(dir)
	if d := dock(l); d.Left.Size == 300 || !d.Left.Open || d.Bottom.Open || d.Right.Open {
		t.Errorf("old track layout kept: %+v", d)
	}
	dock(l).Left.Size = 280
	l.changed()
	l.flush()
	if d := dock(openLayout(dir)); d.Left.Size != 280 {
		t.Errorf("saved track width not read: %+v", d)
	}
}

func TestSettingsOpenFromTheShortcut(t *testing.T) {
	h := open(t)
	h.tt.Key(ui.Cmd, ui.KeyComma)
	h.until("the settings window", func() bool { return len(h.wins.Settings()) == 1 })
}

// The left pane's button holds the bottom-left corner, so nothing in the status bar can push it; settings are in the
// app menu.
func TestLeftPaneButtonHoldsTheStatusBarCorner(t *testing.T) {
	h := open(t)
	r, ok := h.tt.Find("隐藏左栏  ⌘B")
	if !ok {
		t.Fatalf("no left pane button; texts %q", h.tt.Texts())
	}
	if r.X > 16 || r.Y < 4*theme.HTitleBar {
		t.Errorf("the left pane button is not at the bottom-left corner: %+v", r)
	}
	if slices.Contains(h.tt.Texts(), "设置") {
		t.Error("settings still has a button in the window")
	}
}

func TestCloseStopsUpdates(t *testing.T) {
	h := open(t)
	h.w.Close()
	ran := false
	h.w.update(func() { ran = true })
	h.q.drain()
	if ran {
		t.Error("update ran after Close")
	}
}

func TestSmokeFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want smoke
	}{
		{"none", nil, smoke{scenario: view.Smoke{Row: -1}}},
		{"a view", map[string]string{"SHEAF_NATIVE_VIEW": "track"}, smoke{on: true, view: view.Track, scenario: view.Smoke{Row: -1}}},
		{"search and row", map[string]string{"SHEAF_NATIVE_Q": "fix", "SHEAF_NATIVE_ROW": "3"}, smoke{on: true, scenario: view.Smoke{Q: "fix", Row: 3}}},
		{"removed log window scenario", map[string]string{"SHEAF_NATIVE_WIN": "diff", "SHEAF_NATIVE_DIFF": "1"}, smoke{scenario: view.Smoke{Row: -1}}},
		{"settings needs none", map[string]string{"SHEAF_NATIVE_WIN": "settings"}, smoke{on: true, win: "settings", scenario: view.Smoke{Row: -1}}},
		{"a bad row", map[string]string{"SHEAF_NATIVE_ROW": "x"}, smoke{scenario: view.Smoke{Row: -1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := smokeFromEnv(func(k string) string { return tc.env[k] }); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The views read worktree counts from Info (the 未提交 count in the rail): a worktree push must refresh it, not only
// the next index or config change.
func TestWorktreePushRefreshesInfo(t *testing.T) {
	h := open(t)
	h.until("first counts", func() bool { return !h.tt.FramePending() })
	dir := filepath.Join(h.s.Root, "repo-a")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.s.RefreshDirty(t.Context(), []string{dir}, false)
	h.w.Worktrees(session.WorktreeStatus{Read: []string{dir}}) // the harness does not wire OnWorktrees
	h.until("count in Info", func() bool {
		for _, r := range h.w.info.Repos {
			for _, wt := range r.Worktrees {
				if wt.Dirty > 0 {
					return !h.tt.FramePending()
				}
			}
		}
		return false
	})
}

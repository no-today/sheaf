package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestStateToastNamesFailedReposOnce(t *testing.T) {
	testutil.Isolate(t)
	h := open(t)
	queued := func() []string {
		texts := make([]string, len(h.w.toastTexts))
		for i, text := range h.w.toastTexts {
			texts[i] = text(winTr.For(h.w.locale))
		}
		h.w.toastTexts = nil // one toast per change: later calls see only what they queued
		return texts
	}
	fail := func(repos ...string) []string {
		t.Helper()
		failed := make([]session.RepoFailure, len(repos))
		for i, r := range repos {
			failed[i] = session.RepoFailure{Repo: r, Error: "boom"}
		}
		h.w.State(session.State{Ready: true, Phase: "ready", Failed: failed})
		h.q.drain()
		return queued()
	}
	if got := fail("a", "b"); len(got) != 1 || got[0] != "2 个仓库读取失败：a, b" {
		t.Fatalf("failed-repos toast: %q", got)
	}
	if got := fail("a", "b"); len(got) != 0 { // the same repos again say nothing
		t.Fatalf("repeat: %q", got)
	}
	if got := fail("a"); len(got) != 1 || got[0] != "1 个仓库读取失败：a" {
		t.Fatalf("shrunk set: %q", got)
	}
	if got := fail(); len(got) != 0 {
		t.Fatalf("recovered: %q", got)
	}
}

func TestOpenQueuesConfigBackupToast(t *testing.T) {
	testutil.Isolate(t)
	data := t.TempDir()
	root := filepath.Join(t.TempDir(), "w")
	testutil.NewRepo(t, filepath.Join(root, "a")).Commit("init", map[string]string{"a": "a"})
	dir := filepath.Join(data, "workspaces", session.WorkspaceID(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "workspace.json")
	if err := os.WriteFile(path, []byte(`{"master":`), 0o600); err != nil {
		t.Fatal(err)
	}
	q := &queue{}
	w := New(q.push, nil)
	t.Cleanup(w.Close)
	s := openSession(t, root, data, w)
	w.Attach(s)
	texts := make([]string, len(w.toastTexts))
	for i, text := range w.toastTexts {
		texts[i] = text(winTr.For("zh-Hans"))
	}
	if len(texts) != 1 || !strings.HasPrefix(texts[0], "工作区配置无法读取，已备份为 workspace.json.broken-") {
		t.Fatalf("backup toast: %q", texts)
	}
	if len(w.failedRepos) != 0 {
		t.Fatal(w.failedRepos)
	}
}

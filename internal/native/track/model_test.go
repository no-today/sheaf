package track

import (
	"slices"
	"strings"
	"testing"

	"sheaf/internal/session"
)

var zh = tr.For("zh-Hans")

func repo(x func(*session.AggRepo)) session.AggRepo {
	r := session.AggRepo{Key: "a", Repo: "a", Commits: 1, Files: 1, Base: "master", Branch: "feat", BranchFrom: session.FromInferred, Ahead: 1, Auto: "feat", Main: "master"}
	if x != nil {
		x(&r)
	}
	return r
}

func TestPickEntry(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		temp       bool
		dirty      int
		got        string
	}{
		{"explicit uncommitted", wcID, false, 0, wcID},
		{"dirty fallback", "", false, 4, wcID},
		{"empty", "", false, 0, ""},
		{"requirement or branch", tmpID, true, 0, tmpID},
		{"missing temporary entry", tmpID, false, 0, ""},
		{"saved id ignored", "t1", false, 0, ""},
	} {
		if got := pickEntry(tc.want, tc.temp, tc.dirty); got != tc.got {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.got)
		}
	}
}

func TestWithBranch(t *testing.T) {
	in := map[string]string{"a": "x", "b": "y"}
	if got := withBranch(map[string]string{}, "a", "x"); len(got) != 1 || got["a"] != "x" {
		t.Errorf("set: %v", got)
	}
	if got := withBranch(in, "a", ""); len(got) != 1 || got["b"] != "y" {
		t.Errorf("drop: %v", got)
	}
	if len(in) != 2 {
		t.Error("the input changed")
	}
}

func TestRowLabelAndBranchShown(t *testing.T) {
	if rowLabel("a", "a") != "a" || rowLabel(session.RowKey("x/y", "feat-1"), "x/y") != "x/y · feat-1" {
		t.Error("rowLabel")
	}
	for _, c := range []struct {
		r    session.AggRepo
		want string
	}{
		{repo(nil), "feat"},
		{repo(func(r *session.AggRepo) {
			r.Branch, r.BranchFrom, r.Stage = "", session.FromMerged, session.StageLanded
		}), "已合入 master"},
		{repo(func(r *session.AggRepo) {
			r.Branch, r.BranchFrom, r.Stage = "", session.FromMerged, session.StagePending
		}), "无分支"},
		{repo(func(r *session.AggRepo) {
			r.Branch, r.BranchFrom, r.Stage = "", session.FromMerged, session.StageUnknown
		}), "无分支"},
		{repo(func(r *session.AggRepo) { r.Branch, r.BranchFrom = "", session.FromNone }), "无分支"},
	} {
		if got := branchShown(zh, c.r); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestRepoTableRules(t *testing.T) {
	merged := repo(func(r *session.AggRepo) { r.Key, r.Repo, r.Branch, r.BranchFrom = "b", "b", "", session.FromMerged })
	rows := []session.AggRepo{
		repo(nil),
		merged,
		repo(func(r *session.AggRepo) { r.Key, r.Branch = "a\x00x", "x" }),
	}
	if got := defaultChecked(rows); len(got) != 2 || !got["a"] || !got["a\x00x"] || got["b"] {
		t.Errorf("defaultChecked %v", got)
	}
	for _, c := range []struct {
		r    session.AggRepo
		want abResult
	}{
		{repo(func(r *session.AggRepo) { r.Skip = "no_base" }), abResult{Kind: abNone}},
		{repo(func(r *session.AggRepo) { r.Skip = "base_unset" }), abResult{Kind: abUnset}},
		{merged, abResult{Kind: abDash}},
		{repo(func(r *session.AggRepo) { r.Ahead, r.Behind = 2, 5 }), abResult{Kind: abCount, Up: 2, Down: 5}},
	} {
		if got := abCell(c.r); got != c.want {
			t.Errorf("abCell %+v = %+v, want %+v", c.r, got, c.want)
		}
	}
	if usedBy(rows, rows[0], "x") != 3 || usedBy(rows, rows[2], "x") != 0 {
		t.Error("usedBy")
	}
}

func TestKeepOrder(t *testing.T) {
	rows := func(keys ...string) []session.AggRepo {
		var out []session.AggRepo
		for _, k := range keys {
			out = append(out, session.AggRepo{Key: k})
		}
		return out
	}
	keysOf := func(rs []session.AggRepo) string {
		var s []string
		for _, r := range rs {
			s = append(s, r.Key)
		}
		return strings.Join(s, "")
	}
	in := rows("c", "a", "b", "d")
	for _, c := range []struct {
		keys []string
		want string
	}{
		{nil, "cabd"},
		{[]string{"a", "b", "c"}, "abcd"},
		{[]string{"gone", "b"}, "bcad"},
	} {
		if got := keysOf(keepOrder(in, c.keys)); got != c.want {
			t.Errorf("%v: got %s, want %s", c.keys, got, c.want)
		}
	}
}

func TestTargetGroups(t *testing.T) {
	opts := []session.Option{{Value: "origin/uat", Count: 6}, {Value: "master", Count: 7}, {Value: "origin/master", Count: 7}}
	names := func(os []session.Option) string {
		var s []string
		for _, o := range os {
			s = append(s, o.Value)
		}
		return strings.Join(s, " ")
	}
	fav, rest := targetGroups(opts, []string{"origin/master", "gone"}, "")
	if names(fav) != "origin/master gone" || fav[1].Count != 0 || names(rest) != "origin/uat master" {
		t.Errorf("favs %s, rest %s", names(fav), names(rest))
	}
	fav, rest = targetGroups(opts, []string{"origin/master"}, "MAST")
	if names(fav) != "origin/master" || names(rest) != "master" {
		t.Errorf("filtered: favs %s, rest %s", names(fav), names(rest))
	}
	if got := toggleFav([]string{"a"}, "b"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("fav %v", got)
	}
	if got := toggleFav([]string{"a", "b"}, "a"); !slices.Equal(got, []string{"b"}) {
		t.Errorf("unfav %v", got)
	}
}

func TestFailReason(t *testing.T) {
	long := strings.Repeat("x", 200)
	for _, c := range []struct{ in, want string }{
		{"git fetch origin: exit status 128: fatal: unable to access 'https://x/': Could not resolve host: x", "fatal: unable to access 'https://x/': Could not resolve host: x"},
		{"git push: exit status 1: remote: hint\nerror: failed to push\nhint: pull first", "error: failed to push"},
		{"plain failure", "plain failure"},
		{"", ""},
		{"exit status 1: " + long, strings.Repeat("x", maxReason-1) + "…"},
	} {
		if got := failReason(c.in); got != c.want {
			t.Errorf("failReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if failHint(zh, "fatal: Authentication failed for 'x'") != zh.FailAuth || failHint(zh, "ssh: connect: Network is unreachable") != zh.FailNetwork || failHint(zh, "boom") != "" {
		t.Error("failHint")
	}
	top := topReasonText(zh, []string{"exit status 1: fatal: a", "exit status 1: fatal: b", "exit status 2: fatal: a", ""})
	if top != "主要原因：fatal: a（2 个）" {
		t.Errorf("top %q", top)
	}
}

func TestMRCells(t *testing.T) {
	url := "https://gitlab.x/g/p/-/merge_requests/12"
	if mrRef(url) != "MR !12" || mrRef("https://x/y") != "" {
		t.Error("mrRef")
	}
	c := mrCell(zh, session.MRRow{Outcome: "created", URL: url})
	if c.Text != "已创建 MR !12" || c.URL != url || c.Tone != toneOK {
		t.Errorf("created %+v", c)
	}
	if c := mrCell(zh, session.MRRow{Outcome: "exists", URL: url}); c.Text != "已有 MR !12" || c.Tone != toneSkip {
		t.Errorf("exists %+v", c)
	}
	if c := mrCell(zh, session.MRRow{Outcome: "skipped", Skip: "diverged"}); c.Text != "跳过 · 和远端对不上" {
		t.Errorf("skipped %+v", c)
	}
	auth := mrCell(zh, session.MRRow{Outcome: "failed", Auth: true, Error: "401 Unauthorized"})
	if auth.Text != "✕ glab 未登录" || auth.Reason != "" || !strings.HasPrefix(auth.Title, zh.MrAuthHint) {
		t.Errorf("auth %+v", auth)
	}
	failed := mrCell(zh, session.MRRow{Outcome: "failed", Error: "glab mr create: exit status 1: fatal: boom"})
	if failed.Text != "✕ 失败" || failed.Reason != "fatal: boom" || failed.Tone != toneBad {
		t.Errorf("failed %+v", failed)
	}
	run := session.MRRun{Created: 2, Existing: 1, Failed: 2, Skipped: 1, Rows: []session.MRRow{
		{Outcome: "failed", Error: "exit status 1: fatal: boom"}, {Outcome: "failed", Error: "exit status 1: fatal: boom"}, {Outcome: "created"}}}
	if got := mrSummary(zh, run); got != "2 个 MR 已创建 · 1 个已有 MR · 2 个失败 · 跳过 1 个 · 主要原因：fatal: boom（2 个）" {
		t.Errorf("summary %q", got)
	}
}

func TestSheetTexts(t *testing.T) {
	for _, c := range []struct {
		r    session.MRPlanRow
		want string
	}{
		{session.MRPlanRow{Branch: "f", Target: "uat"}, "创建 f → uat 的 MR"},
		{session.MRPlanRow{Branch: "f", Target: "uat", Push: true}, "先推送 f，再创建 f → uat 的 MR"},
		{session.MRPlanRow{Branch: "f", Skip: "is_target"}, "f 就是目标分支，跳过"},
		{session.MRPlanRow{Skip: "no_remote"}, "没有远端，跳过"},
	} {
		if got := mrHow(zh, c.r); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

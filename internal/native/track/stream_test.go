package track

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/view"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

// reqFixture is the design's sample: requirement YT-2841 across five repos, 23 files, one repo at each stage and two
// developing, an uncommitted file and a merge edited while merging in pay-core; and three more requirements on branches
// of their own.
func reqFixture(t *testing.T) string {
	t.Helper()
	testutil.Isolate(t)
	root := filepath.Join(t.TempDir(), "ws")
	remotes := t.TempDir()
	const branch = "feat/settle-split-YT-2841"
	repo := func(name string, paths []string) *testutil.Repo {
		r := testutil.NewRepo(t, filepath.Join(root, name))
		old := map[string]string{}
		for _, p := range paths {
			old[p], _ = reqTexts(p)
		}
		r.Commit("初始化", old)
		return r
	}
	work := func(r *testutil.Repo, b string, paths []string) {
		r.Git("checkout", "-q", "-b", b)
		nw := map[string]string{}
		for _, p := range paths {
			_, nw[p] = reqTexts(p)
		}
		r.Commit("YT-2841 分院结算拆单", nw)
	}
	push := func(r *testutil.Repo, name, b string) {
		bare := filepath.Join(remotes, name+".git")
		r.Git("init", "-q", "--bare", bare)
		r.Git("remote", "add", "origin", bare)
		r.Git("push", "-q", "-u", "origin", "master", b)
		r.Git("fetch", "-q", "origin")
	}

	pay := []string{"src/settle/SplitPlanner.java", "src/settle/SettleOrder.java", "src/settle/BranchLedger.java", "src/refund/RefundRouter.java", "test/settle/SplitPlannerTest.java", "config/settle.yaml", "src/settle/SplitRule.java"}
	p := repo("pay-core", append(pay, "README.md"))
	work(p, branch, pay)
	p.Git("checkout", "-q", "master")
	p.Commit("master: 文档", map[string]string{"README.md": "# pay-core\n\nsettlement\n"})
	p.Git("checkout", "-q", branch)
	p.Git("merge", "-q", "--no-ff", "--no-commit", "master")
	_, so := reqTexts("src/settle/SettleOrder.java")
	p.Commit("YT-2841 合并 master", map[string]string{"src/settle/SettleOrder.java": so + "// branch rounding kept while merging\n"})
	_, sp := reqTexts("src/settle/SplitPlanner.java")
	if err := os.WriteFile(filepath.Join(p.Dir, "src/settle/SplitPlanner.java"), []byte(sp+"// todo: refund split\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	order := []string{"api/OrderSettleController.java", "mapper/OrderSplitMapper.xml", "model/SettleView.java", "service/OrderService.java", "config/routes.yaml", "test/OrderTest.java"}
	o := repo("order-server", order)
	work(o, branch, order)
	push(o, "order-server", branch)
	extra := testutil.NewRepoAt(t, filepath.Join(root, "order-server-2856"))
	o.Git("worktree", "add", "-q", "-b", "feat/rx-YT-2856", extra.Dir, "master")
	extra.Commit("YT-2856 处方流转回执", map[string]string{"api/Rx.java": "class Rx {}\n"})

	wms := []string{"stock/BranchStockQuery.java", "stock/query.go", "stock/ledger.go", "config/stock.yaml"}
	m := repo("wms-server", wms)
	m.Git("checkout", "-q", "-b", branch)
	m.Commit("YT-2841 库存按分院", map[string]string{"stock/ledger.go": "package stock\n\nfunc Ledger() int {\n    return 3\n}\n"})
	nw := map[string]string{}
	for _, f := range wms {
		_, nw[f] = reqTexts(f)
	}
	m.Commit("YT-2841 分院结算拆单", nw)
	push(m, "wms-server", branch)

	web := []string{"src/pages/settle/SplitTable.vue", "src/api/settle.ts", "src/api/types.ts", "test/settle.ts"}
	s := repo("settle-web", web)
	work(s, branch, web)
	s.Git("branch", "release")
	extra2 := testutil.NewRepoAt(t, filepath.Join(root, "settle-web-2790"))
	s.Git("worktree", "add", "-q", "-b", "fix/refund-YT-2790", extra2.Dir, "master")
	extra2.Commit("YT-2790 退费审批加签", map[string]string{"src/refund.ts": "export const refund = 1\n"})

	gw := []string{"routes/settle.yaml", "proxy/settle.go"}
	g := repo("gateway", gw)
	work(g, branch, gw)
	g.Git("checkout", "-q", "master")
	g.Merge(branch, "Merge branch '"+branch+"'")
	extra3 := testutil.NewRepoAt(t, filepath.Join(root, "gateway-2702"))
	g.Git("worktree", "add", "-q", "-b", "feat/med-YT-2702", extra3.Dir, "master")
	extra3.Commit("YT-2702 医保目录同步", map[string]string{"routes/med.yaml": "med: true\n"})
	return root
}

// reqTexts are a file before and after the requirement: SplitPlanner as in the design, the others a small edit.
func reqTexts(path string) (string, string) {
	if strings.HasSuffix(path, "SplitPlanner.java") {
		old := "package core.settle;\n\nimport java.util.*;\n\npublic final class SplitPlanner {\n    private final Ledger ledger;\n\n    public List<SettlePlan> plan(SettleOrder order) {\n        requireOpen(order);\n        List<SettlePlan> plans = new ArrayList<>();\n        plans.add(planOf(order));\n        return plans;\n    }\n}\n"
		nw := strings.Replace(old, "        plans.add(planOf(order));", "        for (Branch branch : order.branches()) {\n            Money share = ledger.shareOf(order, branch);\n            if (share.isZero()) {\n                continue;\n            }\n            plans.add(planOf(order, branch, share));\n        }\n        requireBalanced(order, plans);", 1)
		nw = strings.Replace(nw, "    }\n}\n", "    }\n\n    private void requireBalanced(SettleOrder order, List<SettlePlan> plans) {\n        Money total = plans.stream().map(SettlePlan::amount).reduce(Money.ZERO, Money::plus);\n        if (!total.equals(order.amount())) {\n            throw new SettleException(\"拆单金额不平: \" + order.id());\n        }\n    }\n}\n", 1)
		return old, nw
	}
	if strings.HasSuffix(path, "README.md") {
		return "# pay-core\n", "# pay-core\n"
	}
	var text string
	switch filepath.Ext(path) {
	case ".go":
		text = "package settle\n\nfunc Amount() int {\n    return 100\n}\n"
	case ".ts":
		text = "export function settle(order: Order): number {\n    const amount = 100;\n    return amount;\n}\n"
	case ".yaml":
		text = "settlement:\n  split: false\n  limit: 100\n  route: /settle\n"
	case ".xml", ".vue":
		text = "<settlement>\n  <amount>100</amount>\n  <split>false</split>\n</settlement>\n"
	default:
		text = "package core.settle;\n\npublic final class Settlement {\n    private final long id;\n    private final BigDecimal amount;\n\n    public int amount() {\n        return 100;\n    }\n}\n"
	}
	return text, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(text, "100", "200"), "false", "true"), "BigDecimal", "Money")
}

// openReq shows the requirement page of reqFixture at 1440×900 with YT-2841 picked, once its stream is read.
func openReq(t *testing.T, locale string) *harness {
	t.Helper()
	h := viewtest.Open(t, reqFixture(t))
	viewtest.TrackAll(t, h.S)
	h.DockOf = Dock()
	hr := viewtest.Mount(t, h, locale, func(h view.Host) view.View { return New(h) })
	x := &harness{hr, hr.V.(*Page)}
	x.TT.SetSize(1440, 900)
	x.Until("requirements", func() bool { return x.p.reqsOK })
	for _, r := range x.p.reqs {
		if r.ID == "YT-2841" && x.p.shownReq() != r.ID {
			x.p.chooseReq(r)
		}
	}
	x.Until("the stream", func() bool {
		return x.p.shownReq() == "YT-2841" && !x.p.filesBusy && len(x.p.stream.Files()) > 0 && x.p.stream.Loaded() > 0 && len(x.p.stats) == len(x.p.rows)
	})
	return x
}

func TestRequirementStreamFirstDiffLatency(t *testing.T) {
	h := viewtest.Open(t, reqFixture(t))
	viewtest.TrackAll(t, h.S)
	h.DockOf = Dock()
	started := time.Now()
	hr := viewtest.Mount(t, h, "zh-Hans", func(h view.Host) view.View { return New(h) })
	p := hr.V.(*Page)
	hr.TT.SetSize(1440, 900)
	frames := 0
	hr.Until("first requirement diff", func() bool {
		frames++
		return p.shownReq() == "YT-2841" && p.stream.Loaded() > 0
	})
	t.Logf("first diff: %.2f ms, %d frames, stats %d/%d, requested %d", float64(time.Since(started).Microseconds())/1000, frames, len(p.stats), len(p.rows), p.stream.Requested)
	in := *p.input
	cond := *in.Cond
	cond.Q = "YT-2856"
	in.Cond, in.Requirement = &cond, cond.Q
	h.S.PrefetchTagAgg(t.Context(), []session.TagAggInput{in})
	started, frames = time.Now(), 0
	p.chooseReq(session.Requirement{ID: in.Requirement})
	hr.Until("first diff after a cached requirement switch", func() bool {
		frames++
		return p.prev == nil && p.filesIn.Requirement == in.Requirement && p.stream.Loaded() > 0
	})
	t.Logf("cached switch first diff: %.2f ms, %d frames", float64(time.Since(started).Microseconds())/1000, frames)
}

func TestRequirementStream(t *testing.T) {
	h := openReq(t, "zh-Hans")
	p := h.p
	if got := len(p.stream.Files()); got != 24 {
		var names []string
		for _, f := range p.stream.Files() {
			names = append(names, f.Key)
		}
		t.Fatalf("got %d files: %q", got, names)
	}
	first, last := p.stream.List.Visible()
	visible := 0
	for i := first; i <= last; i++ {
		visible++
	}
	if p.stream.Requested > visible+2 {
		t.Errorf("the first screen read %d patches for %d visible rows", p.stream.Requested, visible)
	}
}

func TestRequirementStreamWaitsForRefreshInputs(t *testing.T) {
	h := viewtest.Open(t, viewtest.Fixture(t))
	for _, phase := range []string{"overview", "merge classification", "files", "line counts"} {
		t.Run(phase, func(t *testing.T) {
			f := session.AggFile{Key: "repo", Repo: "repo", Path: "file.go", Status: "M"}
			p := &Page{
				h:      h,
				s:      h.S,
				stream: diffview.NewStream(),
				rows:   []session.AggRepo{{Key: f.Key}},
				files:  []session.AggFile{f},
				stats: map[string]map[string]session.FileStat{
					f.Key: {f.Path: {Add: 1, Del: 1}},
				},
			}
			t.Cleanup(p.stream.Close)
			p.syncStream()
			rev := p.stream.Files()[0].Rev
			p.reread++
			p.filesVer++
			switch phase {
			case "overview":
				p.aggBusy = true
			case "merge classification":
				p.loadRemerge(session.TagAggInput{}, p.aggSeq)
			case "files":
				p.filesBusy = true
			case "line counts":
				p.loadStats(session.TagAggInput{})
			}
			p.syncStream()
			if got := p.stream.Files()[0].Rev; got != rev {
				t.Fatalf("unfinished %s replaced the displayed diff revision: %q -> %q", phase, rev, got)
			}
			p.aggBusy, p.filesBusy = false, false
			p.filesReread = p.reread
			p.filesVer++
			p.syncStream()
			if p.stream.Files()[0].Rev == rev {
				t.Fatal("completed inputs did not refresh the displayed diff")
			}
		})
	}
}

func TestRequirementWholeNumber(t *testing.T) {
	testutil.Isolate(t)
	root := t.TempDir()
	short := testutil.NewRepo(t, filepath.Join(root, "short"))
	short.Commit("ABC-12 landed", map[string]string{"short.txt": "short\n"})
	short.Git("checkout", "-q", "-b", "feat/ABC-12")
	long := testutil.NewRepo(t, filepath.Join(root, "long"))
	long.Commit("init", map[string]string{"init.txt": "init\n"})
	long.Git("checkout", "-q", "-b", "feat/ABC-123")
	long.Commit("ABC-123 developing", map[string]string{"long.txt": "long\n"})
	host := viewtest.Open(t, root)
	if err := host.S.SetRequirement(`ABC-\d+`); err != nil {
		t.Fatal(err)
	}
	host.DockOf = Dock()
	viewtest.TrackAll(t, host.S)
	hr := viewtest.Mount(t, host, "zh-Hans", func(h view.Host) view.View { return New(h) })
	h := &harness{hr, hr.V.(*Page)}
	p := h.p
	h.Until("both requirement numbers", func() bool { return p.reqsOK && len(p.reqs) == 2 })
	i := slices.IndexFunc(p.reqs, func(r session.Requirement) bool { return r.ID == "ABC-12" })
	if i < 0 || p.reqs[i].Repos != 1 || p.reqs[i].Stage != session.StageLanded {
		t.Fatalf("left requirements: %+v", p.reqs)
	}
	p.chooseReq(p.reqs[i])
	h.Until("only the whole requirement", func() bool {
		return !p.aggBusy && !p.filesBusy && p.allCommits != nil && p.allCommits.Total == 1 && len(p.files) == 1
	})
	if len(p.rows) != 1 || p.rows[0].Repo != "short" || p.rows[0].Stage != p.reqs[i].Stage || p.files[0].Path != "short.txt" {
		t.Fatalf("page differs from the requirement list: rows %+v, files %+v", p.rows, p.files)
	}
	// Diff windows carry the input through JSON too.
	data, err := json.Marshal(p.input)
	if err != nil {
		t.Fatal(err)
	}
	var in session.TagAggInput
	if err := json.Unmarshal(data, &in); err != nil {
		t.Fatal(err)
	}
	commits, err := host.S.TagAggCommits(in, "", false)
	if err != nil || commits.Total != 1 {
		t.Fatalf("serialized requirement: %+v, %v", commits, err)
	}
}

func TestRequirementCommitCounts(t *testing.T) {
	h := openReq(t, "zh-Hans")
	p := h.p
	for _, tc := range []struct {
		name   string
		merges bool
		want   int
	}{
		{"clean merges hidden", false, 7},
		{"all merges shown", true, 8},
		{"clean merges hidden again", false, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if p.merges != tc.merges {
				p.toggleMerges()
			}
			h.Until("the requirement's tab count", func() bool {
				return p.allCommits != nil && p.allCommits.Total == tc.want &&
					h.TT.HasText(fmt.Sprintf("提交 %d", tc.want))
			})
			if h.TT.HasText(fmt.Sprintf("5 仓库 %d 提交 23 文件", tc.want)) {
				t.Errorf("removed title summary is still visible: %q", h.TT.Texts())
			}
		})
	}
	p.showDown("pay-core")
	h.Until("the focused row", func() bool { return p.commits != nil && p.commits.Total == 2 && p.down != nil })
	if !h.TT.HasText("提交 7") || h.TT.HasText("5 仓库 7 提交 23 文件") {
		t.Errorf("focusing a row changed the requirement's count: %q", h.TT.Texts())
	}
}

func TestRequirementLineTotalsSitInTheTitleBar(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := openReq(t, locale)
			w := tr.For(locale)
			for _, width := range []int{1180, 1440} {
				h.TT.SetSize(width, 900)
				h.TT.Frame()
				lines, ok := h.TT.Find("+54 −39")
				if !ok {
					t.Fatalf("line totals are missing at width %v: %q", width, h.TT.Texts())
				}
				fetch, ok := h.TT.Find(w.Fetch)
				if !ok || lines.Y+lines.H > theme.HTitleBar || lines.X+lines.W > fetch.X {
					t.Errorf("line totals are outside the title bar or overlap its actions at width %v: totals %+v, fetch %+v", width, lines, fetch)
				}
			}
			key := h.p.rows[0].Key
			stats := h.p.stats[key]
			delete(h.p.stats, key)
			h.TT.Frame()
			if h.TT.HasText("+54 −39") {
				t.Error("line totals appear before all rows have been counted")
			}
			h.p.stats[key] = stats
			h.TT.Frame()
			if !h.TT.HasText("+54 −39") {
				t.Error("line totals did not return after counting finished")
			}
		})
	}
}

// fileAt is the index in the stream of a file of repo.
func fileAt(t *testing.T, p *Page, repo, path string) int {
	t.Helper()
	for i, f := range p.stream.Files() {
		if af := f.Source.(session.AggFile); af.Repo == repo && af.Path == path {
			return i
		}
	}
	t.Fatalf("no %s/%s in the stream", repo, path)
	return -1
}

// The outline jumps to a file, the repo header stays pinned over its files, and ⌥↓ / ⌥↑ step file by file.
func TestStreamNavigation(t *testing.T) {
	h := openReq(t, "zh-Hans")
	p := h.p
	i := fileAt(t, p, "order-server", "service/OrderService.java")
	key := p.stream.Files()[i].Group
	if err := h.TT.Click("outline " + key + "/service/OrderService.java"); err != nil {
		t.Fatal(err)
	}
	h.Until("the jump", func() bool { return p.stream.Current() == i })
	head, ok := h.TT.Access("stream repo " + key)
	if !ok {
		t.Fatalf("no pinned header for %s: %q", key, h.TT.Texts())
	}
	file, ok := h.TT.Access("order-server/service/OrderService.java")
	if !ok || head.Bounds.Y > file.Bounds.Y || file.Bounds.Y < head.Bounds.Y+head.Bounds.H-0.5 {
		t.Errorf("header %+v, file %+v", head.Bounds, file.Bounds)
	}
	h.TT.Key(ui.Alt, ui.KeyDown)
	h.Until("the next file", func() bool { return p.stream.Current() == i+1 })
	h.TT.Key(ui.Alt, ui.KeyUp)
	h.Until("back", func() bool { return p.stream.Current() == i })
	h.Until("the diffs in view", func() bool { return p.stream.Loaded() > 0 && h.TT.HasText("OrderService.java") })
}

// The arrow before a file's head folds its diff to the head and opens it again.
func TestStreamFileFolds(t *testing.T) {
	h := openReq(t, "zh-Hans")
	p := h.p
	i := fileAt(t, p, "pay-core", "config/settle.yaml")
	h.Until("the diff", func() bool { return p.stream.Loaded() > 0 && !p.stream.Folded(i) })
	head, ok := h.TT.Access("pay-core/config/settle.yaml")
	if !ok {
		t.Fatalf("no head: %q", h.TT.Texts())
	}
	arrow := func() { h.TT.ClickAt(head.Bounds.X-8, head.Bounds.Y+head.Bounds.H/2) }
	arrow()
	h.Until("folded", func() bool { return p.stream.Folded(i) })
	arrow()
	h.Until("open", func() bool { return !p.stream.Folded(i) })
	all := func(folded bool) bool {
		for j := range p.stream.Files() {
			if p.stream.Folded(j) != folded {
				return false
			}
		}
		return true
	}
	h.TT.ClickAtWith(ui.Alt, head.Bounds.X-8, head.Bounds.Y+head.Bounds.H/2)
	h.Until("all folded", func() bool { return all(true) })
	if head, ok = h.TT.Access("pay-core/config/settle.yaml"); !ok {
		t.Fatalf("no head after folding: %q", h.TT.Texts())
	}
	if err := h.TT.RightClick("pay-core/config/settle.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := h.TT.ChooseMenuItem(tr.For("zh-Hans").UnfoldAll); err != nil {
		t.Fatal(err)
	}
	h.Until("all open", func() bool { return all(false) })
}

// The file head's menu copies the path; the tail sums up the stream.
func TestStreamFileMenuAndTail(t *testing.T) {
	h := openReq(t, "zh-Hans")
	w := tr.For("zh-Hans")
	if err := h.TT.RightClick("pay-core/config/settle.yaml"); err != nil {
		t.Fatal(err)
	}
	if got := h.TT.Menu(); slices.Contains(got, i18n.Common.For("zh-Hans").History) || !slices.Contains(got, w.OpenDiff) || !slices.Contains(got, w.CopyPath) {
		t.Fatalf("menu %q", got)
	}
	if err := h.TT.ChooseMenuItem(w.CopyPath); err != nil {
		t.Fatal(err)
	}
	if got := h.TT.Clipboard(); got != "config/settle.yaml" {
		t.Errorf("copied %q", got)
	}
	h.p.stream.ScrollToFile(len(h.p.stream.Files()) - 1)
	h.p.stream.List.ScrollTo(len(h.p.stream.Files())+10, ui.End)
	end := fill(w.StreamEnd, "n", 24)
	h.Until("the tail", func() bool {
		return slices.ContainsFunc(h.TT.Texts(), func(s string) bool { return strings.HasPrefix(s, end) })
	})
}

// A requirement nothing matches says so instead of an empty stream.
func TestEmptyRequirement(t *testing.T) {
	h := openReq(t, "zh-Hans")
	w := tr.For("zh-Hans")
	h.p.chooseReq(session.Requirement{ID: "YT-9999", Title: "没有改动"})
	h.Until("the empty note", func() bool { return h.TT.HasText(fill(w.ReqEmpty, "id", "YT-9999")) })
	if h.TT.HasText("settle.go") {
		t.Errorf("the old stream stays: %q", h.TT.Texts())
	}
}

// requirementShots are the requirement page of the design's sample: the stream, its pinned header, a jump from the
// outline, the commits, and a requirement nothing matches, at both widths and in both languages.
func requirementShots(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		h := openReq(t, locale)
		p := h.p
		for _, width := range []int{1440, 1180, 820} {
			h.TT.SetSize(width, 900)
			p.stream.ScrollToTop()
			h.Until("the first screen", func() bool { return p.stream.Current() == 0 && p.stream.Loaded() > 0 })
			h.Shot(fmt.Sprintf("req-stream-%d-%s", width, locale))
			i := fileAt(t, p, "pay-core", "src/settle/SplitPlanner.java")
			p.stream.ScrollToFile(i)
			h.Until("SplitPlanner under the pinned header", func() bool {
				_, _, read := p.stream.Stats(i)
				return p.stream.Current() == i && read && !h.TT.FramePending()
			})
			h.Shot(fmt.Sprintf("req-pinned-%d-%s", width, locale))
			last := fileAt(t, p, "pay-core", "test/settle/SplitPlannerTest.java")
			p.stream.ScrollToFile(last)
			h.Until("end of pay-core committed files", func() bool {
				_, _, read := p.stream.Stats(last)
				for j, file := range p.stream.Files() {
					af := file.Source.(session.AggFile)
					if af.Repo == "pay-core" && af.Worktree {
						_, _, loaded := p.stream.Stats(j)
						read = read && loaded
					}
				}
				return p.stream.Current() == last && read && !h.TT.FramePending()
			})
			h.Shot(fmt.Sprintf("req-uncommitted-%d-%s", width, locale))
		}
		h.TT.SetSize(1440, 900)
		j := fileAt(t, p, "order-server", "service/OrderService.java")
		if err := h.TT.Click("outline " + p.stream.Files()[j].Group + "/service/OrderService.java"); err != nil {
			t.Fatal(err)
		}
		h.Until("the jump", func() bool {
			_, _, read := p.stream.Stats(j)
			return p.stream.Current() == j && read && !h.TT.FramePending()
		})
		h.Shot("req-outline-jump-" + locale)
		for _, r := range p.rows {
			if r.Repo == "gateway" {
				p.stream.ScrollToGroup(r.Key)
			}
		}
		g := fileAt(t, p, "gateway", "proxy/settle.go")
		h.Until("the landed row", func() bool {
			_, _, read := p.stream.Stats(g)
			return read && h.TT.HasText("gateway/proxy/settle.go") && !h.TT.FramePending()
		})
		h.Shot("req-landed-" + locale)
		p.tab = 1
		h.Until("the commits", func() bool { return p.commits != nil && h.TT.HasText("YT-2841 合并 master") })
		for _, width := range []int{1440, 1180} {
			h.TT.SetSize(width, 900)
			h.Shot(fmt.Sprintf("req-commits-%d-%s", width, locale))
		}
		p.tab = 0
		h.TT.SetSize(1440, 900)
		p.chooseReq(session.Requirement{ID: "YT-9999"})
		h.Until("the empty requirement", func() bool { return !p.aggBusy && len(p.rows) == 0 })
		h.Shot("req-empty-" + locale)
	}
}

// TestSwitchingRequirementSkipsLoadingStates: the previous requirement stays on screen, untouchable, until the next one
// has its overview, files and line counts; the page then changes in one frame, never through 加载中 or an empty stream.
func TestSwitchingRequirementSkipsLoadingStates(t *testing.T) {
	h := openReq(t, "zh-Hans")
	p := h.p
	i := slices.IndexFunc(p.reqs, func(r session.Requirement) bool { return r.ID == "YT-2702" })
	if i < 0 {
		t.Fatalf("no YT-2702 in %+v", p.reqs)
	}
	p.chooseReq(p.reqs[i])
	held := 0
	h.Until("YT-2702 shown", func() bool {
		if h.TT.HasText("加载中…") {
			t.Fatal("the switch showed 加载中…")
		}
		if p.prev != nil {
			held++
			if !h.TT.HasText("SplitPlanner.java") {
				t.Fatalf("the previous stream is gone before the next settled; texts %q", h.TT.Texts())
			}
			return false
		}
		return h.TT.HasText("routes/med.yaml")
	})
	if held == 0 {
		t.Error("the switch never held the previous page")
	}
	if h.TT.HasText("SplitPlanner.java") {
		t.Error("the previous stream is still shown")
	}
}

// TestRailAndOutlineResize: the divider between the requirements and the repos, and the one before the outline, are
// dragged to resize, within bounds.
func TestRailAndOutlineResize(t *testing.T) {
	h := openReq(t, "zh-Hans")
	p := h.p
	drag := func(r ui.Rect, dx, dy float32) {
		x, y := r.X+r.W/2, r.Y+r.H/2
		h.TT.Press(x, y)
		for i := float32(1); i <= 4; i++ {
			h.TT.Move(x+dx*i/4, y+dy*i/4)
		}
		h.TT.Release(x+dx, y+dy)
		h.TT.Frame()
	}
	var rail, outline ui.Rect
	for _, a := range h.TT.AccessAll() {
		if a.Name != "分隔条" {
			continue
		}
		switch {
		case a.Bounds.W > a.Bounds.H && a.Bounds.X < railWidth:
			rail = a.Bounds
		case a.Bounds.H > a.Bounds.W && a.Bounds.X > 1000:
			outline = a.Bounds
		}
	}
	if rail.W == 0 || outline.H == 0 {
		t.Fatalf("dividers: rail %+v, outline %+v", rail, outline)
	}
	drag(rail, 0, -100)
	if p.h.Dock().Splits[splitRail] == 0 || p.h.Dock().Splits[splitRail] > rail.Y-60 {
		t.Errorf("the rail divider did not move up: top %v, divider was at %v", p.h.Dock().Splits[splitRail], rail.Y)
	}
	drag(outline, -80, 0)
	if p.outlineW() != outlineWidth+80 {
		t.Errorf("outline width %v, want %v", p.outlineW(), outlineWidth+80)
	}
	outline.X -= 80
	drag(outline, 2000, 0)
	if p.outlineW() != outlineMin {
		t.Errorf("outline width %v, want the least %v", p.outlineW(), outlineMin)
	}
	if err := h.TT.Click("搜索需求号或提交说明"); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	h.Shot("rail-resized-find-focused-zh-Hans")
}

func TestSwitchScrollsOutlineToTop(t *testing.T) {
	h := openReq(t, "zh-Hans")
	p := h.p
	h.TT.SetSize(1440, 420)
	p.outline.ScrollToEnd()
	h.Until("the outline scrolled down", func() bool { first, _ := p.outline.Visible(); return first > 0 })
	req := p.reqs[slices.IndexFunc(p.reqs, func(r session.Requirement) bool { return r.ID == "YT-2841" })]
	p.choose(wcID)
	h.Until("the uncommitted page", func() bool { return p.picked() == wcID })
	p.chooseReq(req)
	h.Until("the requirement again", func() bool { return p.shownReq() == req.ID && p.prev == nil && len(p.stream.Files()) > 0 })
	h.Until("the outline at its top", func() bool { first, _ := p.outline.Visible(); return first == 0 })
}

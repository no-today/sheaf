// An interactive mock of Sheaf's requirement page. All data is made up; nothing leaves the browser.
(function () {
  "use strict";

  // ---------- data ----------
  // A hunk is [oldStart, newStart, context, body]; each body line starts with " ", "+" or "-".
  function file(path, hunks, isNew) {
    var i = path.lastIndexOf("/");
    return { path: path, name: path.slice(i + 1), dir: i < 0 ? "" : path.slice(0, i + 1), isNew: !!isNew, hunks: hunks.map(parseHunk) };
  }
  function parseHunk(h) {
    var lines = h[3].replace(/^\n/, "").replace(/\n$/, "").split("\n").map(function (l) {
      var k = l.charAt(0);
      return { k: k === "+" || k === "-" ? k : " ", t: l.slice(1) };
    });
    var oc = lines.filter(function (l) { return l.k !== "+"; }).length;
    var nc = lines.filter(function (l) { return l.k !== "-"; }).length;
    return { o: h[0], n: h[1], head: "@@ -" + h[0] + "," + oc + " +" + h[1] + "," + nc + " @@" + (h[2] ? " " + h[2] : ""), lines: lines };
  }

  var REQS = [
    {
      id: "PAY-142",
      title: { en: "Add payment capture to gateway", zh: "网关接入支付扣款" },
      repos: [
        {
          name: "payment-api", branch: "feat-PAY-142", stage: 0, ahead: 1, behind: 1,
          files: [file("internal/payments/capture.go", [[1, 1, "", `
 package payments

 func Capture(amount int) int {
+	if amount <= 0 {
+		return 0
+	}
 	return amount
 }`]])],
          commits: [{ hash: "3f9c2a1", s: "PAY-142 Reject non-positive capture amounts", when: 2, files: [0] }],
          unc: {
            dir: "payment-api-PAY-142",
            files: [
              file("internal/payments/capture.go", [[4, 4, "func Capture(amount int) int {", `
 	if amount <= 0 {
 		return 0
 	}
-	return amount
+	// Retry requests must not capture the same payment twice.
+	return captureOnce(amount)
+}
+
+func captureOnce(amount int) int {
+	return amount
 }`]]),
              file("internal/payments/capture_test.go", [[0, 1, "", `
+package payments
+
+import "testing"
+
+func TestCaptureRejectsZero(t *testing.T) {
+	if Capture(0) != 0 {
+		t.Fatal("zero amount captured")
+	}
+}`]], true)
            ]
          }
        },
        {
          name: "checkout-web", branch: "feat-PAY-142", stage: 1, ahead: 2, behind: 0, github: true,
          files: [
            file("src/pay/capture.ts", [[12, 12, "export async function pay(order: Order) {", `
   const token = await tokenize(order.card)
-  return api.post("/charge", { token, amount: order.total })
+  const auth = await api.post("/authorize", { token, amount: order.total })
+  if (!auth.ok) {
+    return { status: "declined" }
+  }
+  return api.post("/capture", { id: auth.id })
 }`]]),
            file("src/pay/Result.tsx", [[3, 3, "", `
 export function Result({ status }: Props) {
-  return <Done />
+  return status === "declined" ? <Declined /> : <Done />
 }`]])
          ],
          commits: [
            { hash: "b71e04d", s: "PAY-142 Authorize before capture at checkout", when: 5, files: [0] },
            { hash: "c02a9f3", s: "PAY-142 Show the declined state", when: 4, files: [1] }
          ]
        },
        {
          name: "receipt-worker", branch: "feat-PAY-142", stage: 1, ahead: 1, behind: 0,
          files: [file("handlers/capture.go", [[30, 30, "func (h *Handler) OnCapture(e Event) error {", `
 	r := receipt.From(e)
-	r.Kind = "charge"
+	r.Kind = "capture"
 	return h.store.Save(r)`]])],
          commits: [{ hash: "9a40be7", s: "PAY-142 Label receipts as captures", when: 26 }]
        },
        {
          name: "risk-engine", branch: "feat-PAY-142", stage: 1, ahead: 1, behind: 0,
          files: [file("rules/capture.go", [[8, 8, "", `
 var rules = []Rule{
 	maxAmount(50_000),
-	velocity(5, time.Minute),
+	velocity(3, time.Minute),
+	duplicateCapture(10 * time.Minute),
 }`]])],
          commits: [{ hash: "e5d1c08", s: "PAY-142 Block duplicate captures", when: 27 }]
        },
        {
          name: "ledger-service", branch: "feat-PAY-142", stage: 2, ahead: 1, behind: 0,
          files: [file("migrations/0142_payment_captures.sql", [[0, 1, "", `
+CREATE TABLE payment_captures (
+  id         BIGSERIAL PRIMARY KEY,
+  payment_id BIGINT NOT NULL,
+  amount     BIGINT NOT NULL,
+  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
+);
+CREATE UNIQUE INDEX ON payment_captures (payment_id);`]], true)],
          commits: [{ hash: "71c3f2e", s: "PAY-142 Add payment_captures table", when: 50 }]
        },
        {
          name: "gateway", branch: "feat-PAY-142", stage: 3, ahead: 0, behind: 0,
          files: [file("routes.yml", [[14, 14, "", `
   - path: /charge
     service: payment-api
+  - path: /capture
+    service: payment-api
+    timeout: 5s`]])],
          commits: [{ hash: "0d8e6b4", s: "PAY-142 Route /capture to payment-api", when: 74 }]
        }
      ]
    },
    {
      id: "ORD-88",
      title: { en: "Split orders by warehouse", zh: "订单按仓库拆单" },
      repos: [
        {
          name: "order-service", branch: "feat-ORD-88", stage: 0, ahead: 2, behind: 0,
          files: [file("internal/split/split.go", [[0, 1, "", `
+package split
+
+// ByWarehouse keeps lines from one warehouse in one shipment.
+func ByWarehouse(o Order) []Shipment {
+	groups := map[string][]Line{}
+	for _, l := range o.Lines {
+		groups[l.Warehouse] = append(groups[l.Warehouse], l)
+	}
+	return toShipments(o.ID, groups)
+}`]], true)],
          commits: [
            { hash: "4be2d91", s: "ORD-88 Group order lines by warehouse", when: 1 },
            { hash: "a8f7c30", s: "ORD-88 Wire splitting into checkout", when: 1 }
          ]
        },
        {
          name: "inventory", branch: "feat-ORD-88", stage: 1, ahead: 1, behind: 0,
          files: [file("stock/reserve.go", [[41, 41, "func Reserve(ctx context.Context, l Line) error {", `
-	return db.Reserve(ctx, l.SKU, l.Qty)
+	return db.ReserveAt(ctx, l.Warehouse, l.SKU, l.Qty)
 }`]])],
          commits: [{ hash: "5c19ad2", s: "ORD-88 Reserve stock per warehouse", when: 20 }]
        },
        {
          name: "checkout-web", branch: "feat-ORD-88", stage: 2, ahead: 1, behind: 0, github: true,
          files: [file("src/cart/Shipping.tsx", [[18, 18, "", `
-  <p>{t("ships-together")}</p>
+  {shipments.map((s) => (
+    <ShipmentRow key={s.id} shipment={s} />
+  ))}`]])],
          commits: [{ hash: "d3e9f61", s: "ORD-88 Show one row per shipment", when: 48 }]
        }
      ]
    },
    {
      id: "AUTH-17",
      title: { en: "Expire idle sessions", zh: "闲置会话自动过期" },
      repos: [
        {
          name: "auth-service", branch: "feat-AUTH-17", stage: 2, ahead: 1, behind: 0,
          files: [file("session/ttl.go", [[5, 5, "", `
-const IdleTimeout = 0
+// Idle sessions end after 30 minutes; active ones are renewed on each request.
+const IdleTimeout = 30 * time.Minute`]])],
          commits: [{ hash: "f60a2b8", s: "AUTH-17 End sessions idle for 30 minutes", when: 70 }]
        },
        {
          name: "gateway", branch: "feat-AUTH-17", stage: 3, ahead: 0, behind: 0,
          files: [file("middleware.yml", [[3, 3, "", `
   - name: auth
+    renew_session: true`]])],
          commits: [{ hash: "2e7b05c", s: "AUTH-17 Renew sessions at the gateway", when: 96 }]
        }
      ]
    }
  ];

  var WORDS = {
    en: {
      stages: ["In progress", "Awaiting release", "Awaiting master", "Fully merged"],
      groups: ["In progress", "Awaiting release", "Awaiting master", "Fully merged"],
      find: "Search requirements or commits", unc: "Uncommitted", across: "{id} across repos",
      changes: "Changes", commits: "Commits", view: "View", compare: "Compare to", mainBranch: "Each repo's main branch",
      all: "All changes", awaitRel: "Awaiting release", awaitMaster: "Awaiting master", noChanges: "no changes",
      refresh: "Refresh Remotes", sync: "Update Branches", mr: "Create MR/PR",
      newFile: "new file", uncSection: "Uncommitted · {n} file|Uncommitted · {n} files",
      uncCount: "{n} uncommitted", empty: "Nothing to show in this view.", noMatch: "No requirement matches.",
      hoursAgo: "{n}h ago", daysAgo: "{n}d ago", agent: "coding agent",
      openHint: "Click a commit to open it in its own window",
      dirtyTitle: "{n} directory with uncommitted changes|{n} directories with uncommitted changes",
      mrTitle: "Create {n} MR/PR", mrPromise: "Titles come from commits; pushes only when needed, never force-pushes; existing ones just get a link.",
      mrOpen: "Create {kind}", mrPush: "Push and create {kind}", mrGo: "Create {n}",
      mrDone: "Created {mr}",
      syncTitle: "Update {n} favourite branches", syncPromise: "No branch switching, no merging, no touching directories with uncommitted changes.",
      syncRef: "Update", syncDirty: "{wt} has uncommitted changes: skipped", syncGo: "Update {n} branches",
      syncDone: "Fast-forwarded", upToDate: "Already up to date",
      cancel: "Cancel", close: "Close", running: "Running…",
      fetching: "Refreshing remotes {i}/{n}", fetched: "Refreshed {n} remotes",
      synced: "Updated {n} branches, skipped {s}", mrs: "{n} MR/PR created",
      side: "Side by side", unified: "Unified",
      nothingMr: "Every branch has merged; nothing to open.", ws: "ws", reqs: "Requirements"
    },
    zh: {
      stages: ["开发中", "待合 release", "待合 master", "已合完"],
      groups: ["开发中", "待合 release", "待合 master", "已合完"],
      find: "查找需求或提交", unc: "未提交", across: "{id} 在各仓库",
      changes: "改动", commits: "提交", view: "查看", compare: "对比", mainBranch: "各仓库主分支",
      all: "全部改动", awaitRel: "待合 release", awaitMaster: "待合 master", noChanges: "无改动",
      refresh: "刷新远端", sync: "更新分支", mr: "创建 MR/PR",
      newFile: "新文件", uncSection: "未提交 · {n} 个文件",
      uncCount: "{n} 个未提交", empty: "这个视图下没有改动。", noMatch: "没有匹配的需求。",
      hoursAgo: "{n} 小时前", daysAgo: "{n} 天前", agent: "Coding Agent",
      openHint: "点提交，在独立窗口里看它的改动",
      dirtyTitle: "{n} 个有未提交改动的目录",
      mrTitle: "为 {n} 个分支创建 MR/PR", mrPromise: "标题取自提交；按需推送，不强推；已有的只给链接。",
      mrOpen: "创建 {kind}", mrPush: "推送并创建 {kind}", mrGo: "创建 {n} 个",
      mrDone: "已创建 {mr}",
      syncTitle: "更新 {n} 个收藏分支", syncPromise: "不切分支、不合并、不碰没提交的目录。",
      syncRef: "更新", syncDirty: "{wt} 有未提交改动，跳过", syncGo: "更新 {n} 个分支",
      syncDone: "已快进", upToDate: "已是最新",
      cancel: "取消", close: "关闭", running: "进行中…",
      fetching: "正在刷新远端 {i}/{n}", fetched: "已刷新 {n} 个远端",
      synced: "已更新 {n} 个分支，跳过 {s} 个", mrs: "已创建 {n} 个 MR/PR",
      side: "并排", unified: "单列",
      nothingMr: "分支都已合并，没有要创建的。", ws: "ws", reqs: "需求"
    }
  };

  // ---------- helpers ----------
  var root = document.getElementById("demo");
  if (!root) return;

  function lang() { return document.documentElement.dataset.lang === "zh" ? "zh" : "en"; }
  function w(key, vars) {
    var s = WORDS[lang()][key];
    if (vars && typeof s === "string") {
      if (s.indexOf("|") >= 0) s = s.split("|")[vars.n === 1 ? 0 : 1];
      s = s.replace(/\{(\w+)\}/g, function (_, k) { return vars[k] != null ? vars[k] : ""; });
    }
    return s;
  }
  function esc(s) { return String(s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
  function ago(h) { return h < 24 ? w("hoursAgo", { n: h }) : w("daysAgo", { n: Math.round(h / 24) }); }
  function reqStage(r) { return Math.min.apply(null, r.repos.map(function (p) { return p.stage; })); }
  function pips(stage) {
    var out = "";
    for (var i = 0; i < 4; i++) out += "<i class=\"" + (i <= stage ? (stage === 0 ? "wip" : "on") : "") + "\"></i>";
    return "<span class=\"d-pips\">" + out + "</span>";
  }
  function counts(files) {
    var a = 0, d = 0;
    files.forEach(function (f) { f.hunks.forEach(function (h) { h.lines.forEach(function (l) { if (l.k === "+") a++; else if (l.k === "-") d++; }); }); });
    return { a: a, d: d };
  }
  function delta(c) {
    return (c.a ? "<span class=\"d-add\">+" + c.a + "</span>" : "") + (c.d ? " <span class=\"d-del\">−" + c.d + "</span>" : "");
  }

  var KEYWORDS = /^(package|import|func|return|if|else|for|range|var|const|type|struct|map|export|async|await|function|let|CREATE|TABLE|UNIQUE|INDEX|ON|PRIMARY|KEY|NOT|NULL|DEFAULT)$/;
  function highlight(text) {
    var re = /(\/\/.*$|#.*$)|("(?:[^"\\]|\\.)*")|(\b\d[\d_]*\b)|([A-Za-z_]\w*)/g, out = "", last = 0, m;
    while ((m = re.exec(text))) {
      out += esc(text.slice(last, m.index));
      if (m[1]) out += "<span class=\"t-c\">" + esc(m[1]) + "</span>";
      else if (m[2]) out += "<span class=\"t-s\">" + esc(m[2]) + "</span>";
      else if (m[3]) out += "<span class=\"t-n\">" + esc(m[3]) + "</span>";
      else out += KEYWORDS.test(m[4]) ? "<span class=\"t-k\">" + esc(m[4]) + "</span>" : esc(m[4]);
      last = re.lastIndex;
    }
    return out + esc(text.slice(last));
  }

  // ---------- state ----------
  var state = { req: 0, dirty: false, tab: "changes", view: "all", query: "", collapsed: {}, busy: false };

  // Sections of the change stream for what is on screen: one per repo, then its uncommitted files.
  function sections() {
    if (state.dirty) {
      var out = [];
      REQS.forEach(function (r) {
        r.repos.forEach(function (p) {
          if (p.unc) out.push({ key: r.id + "/" + p.name + "/unc", repo: p.name, branch: p.branch, req: r.id, files: [], unc: p.unc });
        });
      });
      return out;
    }
    var req = REQS[state.req];
    return req.repos.filter(function (p) {
      if (state.view === "unc") return !!p.unc;
      if (state.view === "rel") return p.stage <= 1;
      if (state.view === "master") return p.stage <= 2;
      return true;
    }).map(function (p) {
      return { key: req.id + "/" + p.name, repo: p.name, branch: p.branch, stage: p.stage, ahead: p.ahead, behind: p.behind, files: state.view === "unc" ? [] : p.files, unc: p.unc };
    });
  }
  function viewHasChanges(v) {
    var req = REQS[state.req];
    return req.repos.some(function (p) {
      if (v === "unc") return !!p.unc;
      if (v === "rel") return p.stage <= 1;
      if (v === "master") return p.stage <= 2;
      return true;
    });
  }

  // ---------- render ----------
  function render() {
    var req = REQS[state.req];
    var title = state.dirty
      ? "<b>" + esc(w("unc")) + "</b>"
      : "<b>" + req.id + "</b><span>" + esc(req.title[lang()]) + "</span>" + delta(counts([].concat.apply([], req.repos.map(function (p) { return p.files; }))));
    var mrN = state.dirty ? 0 : req.repos.filter(function (p) { return p.stage <= 1; }).length;
    root.innerHTML =
      "<div class=\"d-title\"><div class=\"d-lights\"><i></i><i></i><i></i></div><div class=\"d-what\">" + title + "</div>" +
      "<div class=\"d-actions\">" +
      "<button data-act=\"refresh\"" + (state.busy ? " disabled" : "") + ">" + esc(w("refresh")) + "</button>" +
      "<button data-act=\"sync\"" + (state.dirty || state.busy ? " disabled" : "") + ">" + esc(w("sync")) + "</button>" +
      "<button data-act=\"mr\" class=\"d-primary\"" + (state.dirty || !mrN || state.busy ? " disabled" : "") + ">" + esc(w("mr")) + "<span class=\"d-slot\">" + (mrN || "") + "</span></button>" +
      "</div></div>" +
      "<div class=\"d-body\">" + left() + main() + outline() + "</div>" +
      "<div class=\"d-status\"><span class=\"d-crumb\">" + esc(w("ws")) + " › " + esc(w("reqs")) + (state.dirty ? " › " + esc(w("unc")) : " › " + req.id) + "</span><span class=\"d-task\" id=\"d-task\"></span></div>" +
      "<div class=\"d-modal-host\" id=\"d-modal\"></div>";
    var input = root.querySelector(".d-find");
    if (state.focusFind) { input.focus(); input.setSelectionRange(input.value.length, input.value.length); state.focusFind = false; }
    trackOutline();
  }

  function left() {
    var q = state.query.trim().toLowerCase();
    var groups = [[], [], [], []];
    REQS.forEach(function (r, i) {
      if (q && (r.id + " " + r.title.en + " " + r.title.zh).toLowerCase().indexOf(q) < 0) return;
      groups[reqStage(r)].push(i);
    });
    var list = "";
    groups.forEach(function (g, s) {
      if (!g.length) return;
      list += "<div class=\"d-group\">" + esc(w("groups")[s]) + "</div>";
      g.forEach(function (i) {
        var r = REQS[i], n = 0;
        r.repos.forEach(function (p) { n += p.commits.length; });
        list += "<button class=\"d-req" + (!state.dirty && i === state.req ? " sel" : "") + "\" data-req=\"" + i + "\"><b>" + r.id + "</b><span>" + esc(r.title[lang()]) + "</span><em>" + n + "</em></button>";
      });
    });
    if (!list) list = "<div class=\"d-muted d-pad\">" + esc(w("noMatch")) + "</div>";
    var dirtyN = 0;
    REQS.forEach(function (r) { r.repos.forEach(function (p) { if (p.unc) dirtyN++; }); });

    var req = REQS[state.req], stages = "";
    if (!state.dirty) {
      stages = "<div class=\"d-railhead\">" + esc(w("across", { id: req.id })) + "</div><div class=\"d-stages\">";
      req.repos.forEach(function (p) {
        stages += "<button class=\"d-stage\" data-jump=\"" + req.id + "/" + p.name + "\"><div>" + pips(p.stage) + "<b>" + p.name + "</b><em>" + esc(w("stages")[p.stage]) + "</em></div>" +
          "<div class=\"d-sub\">" + (p.unc ? "<span class=\"d-warn\">" + esc(w("uncCount", { n: p.unc.files.length })) + "</span> · " : "") + p.branch + "</div></button>";
      });
      stages += "</div>";
    }
    return "<aside class=\"d-left\"><div class=\"d-top\">" +
      "<input class=\"d-find\" placeholder=\"" + esc(w("find")) + "\" value=\"" + esc(state.query) + "\" spellcheck=\"false\">" +
      "<button class=\"d-req d-uncentry" + (state.dirty ? " sel" : "") + "\" data-dirty><span>" + esc(w("unc")) + "</span><em>" + dirtyN + "</em></button>" +
      list + "</div><div class=\"d-bottom\">" + stages + "</div></aside>";
  }

  function main() {
    var secs = sections(), nFiles = 0, nCommits = 0;
    secs.forEach(function (s) { nFiles += s.files.length; });
    if (!state.dirty) REQS[state.req].repos.forEach(function (p) { nCommits += p.commits.length; });
    var head;
    if (state.dirty) {
      head = "<div class=\"d-tabs\"><span class=\"d-tab on\">" + esc(w("dirtyTitle", { n: secs.length })) + "</span></div>";
    } else {
      var opts = [["all", w("all")], ["unc", w("unc")], ["rel", w("awaitRel")], ["master", w("awaitMaster")]].map(function (o) {
        var ok = o[0] === state.view || viewHasChanges(o[0]);
        return "<option value=\"" + o[0] + "\"" + (o[0] === state.view ? " selected" : "") + (ok ? "" : " disabled") + ">" + esc(o[1]) + (ok ? "" : " · " + esc(w("noChanges"))) + "</option>";
      }).join("");
      head = "<div class=\"d-tabs\">" +
        "<button class=\"d-tab" + (state.tab === "changes" ? " on" : "") + "\" data-tab=\"changes\">" + esc(w("changes")) + " " + nFiles + "</button>" +
        "<button class=\"d-tab" + (state.tab === "commits" ? " on" : "") + "\" data-tab=\"commits\">" + esc(w("commits")) + " " + nCommits + "</button>" +
        "<span class=\"d-viewpick\">" + esc(w("view")) + " <select class=\"d-select\" data-view>" + opts + "</select>" +
        (state.view === "all" ? " <span class=\"d-hide-narrow\">" + esc(w("compare")) + " <b>" + esc(w("mainBranch")) + "</b></span>" : "") + "</span></div>";
    }
    return "<section class=\"d-main\">" + head + "<div class=\"d-stream\" id=\"d-stream\">" + (state.tab === "commits" && !state.dirty ? commitList() : stream(secs)) + "</div></section>";
  }

  function stream(secs) {
    if (!secs.length) return "<div class=\"d-muted d-pad\">" + esc(w("empty")) + "</div>";
    return secs.map(function (s) {
      var right = s.stage != null ? "<span class=\"d-right\">" + pips(s.stage) + "<span>" + esc(w("stages")[s.stage]) + "</span><span class=\"d-muted\">↑" + s.ahead + " ↓" + s.behind + "</span></span>" : "";
      var h = "<div class=\"d-sec\" id=\"" + anchor(s.key) + "\"><div class=\"d-repo\"><b>" + s.repo + "</b><span class=\"d-muted\">" + s.branch + "</span>" +
        (s.req && state.dirty ? "<span class=\"d-muted\">" + s.req + "</span>" : "") + right + "</div>";
      s.files.forEach(function (f, i) { h += fileBlock(f, s.key + "/" + i); });
      if (s.unc) {
        // Like the app: the working directory's changes get their own repo header, marked uncommitted.
        if (!state.dirty) h += "<div class=\"d-repo\" id=\"" + anchor(s.key + "/unc") + "\"><b>" + s.repo + "</b>" +
          "<span class=\"d-warn d-small d-keep\">" + esc(w("uncSection", { n: s.unc.files.length })) + "</span><span class=\"d-warn d-small\">" + esc(s.unc.dir) + "</span>" +
          "<span class=\"d-muted\">" + s.branch + "</span>" + right + "</div>";
        s.unc.files.forEach(function (f, i) { h += fileBlock(f, s.key + "/unc/" + i); });
      }
      return h + "</div>";
    }).join("");
  }

  function fileBlock(f, key) {
    var c = counts([f]), shut = !!state.collapsed[key];
    var h = "<div class=\"d-file\" id=\"" + anchor(key) + "\"><div class=\"d-fhead\" data-fold=\"" + key + "\">" +
      "<span class=\"d-caret" + (shut ? " shut" : "") + "\">▾</span><b>" + esc(f.name) + "</b><span class=\"d-muted\">" + esc(f.dir) + "</span>" +
      (f.isNew ? "<span class=\"d-muted\">" + esc(w("newFile")) + "</span>" : "") + "<span class=\"d-right\">" + delta(c) + "</span></div>";
    if (!shut) h += diff(f);
    return h + "</div>";
  }

  function diff(f) {
    var h = "<div class=\"d-diff\">";
    f.hunks.forEach(function (hk) {
      h += "<div class=\"d-hunk\">" + esc(hk.head) + "</div>";
      var n = hk.n, o = hk.o;
      hk.lines.forEach(function (l) {
        var num = l.k === "-" ? o : n;
        h += "<div class=\"d-line" + (l.k === "+" ? " add" : l.k === "-" ? " del" : "") + "\"><span class=\"d-num\">" + num + "</span><span class=\"d-mark\">" + (l.k === " " ? "" : l.k === "-" ? "−" : "+") + "</span><code>" + (highlight(l.t) || " ") + "</code></div>";
        if (l.k !== "+") o++;
        if (l.k !== "-") n++;
      });
    });
    return h + "</div>";
  }

  function commitList() {
    var req = REQS[state.req], rows = [];
    req.repos.forEach(function (p) {
      p.commits.forEach(function (c) { rows.push({ c: c, p: p }); });
    });
    rows.sort(function (a, b) { return a.c.when - b.c.when; });
    return "<div class=\"d-hint\">" + esc(w("openHint")) + "</div>" + rows.map(function (r) {
      return "<button class=\"d-commit\" data-commit=\"" + r.p.name + "/" + r.c.hash + "\"><code>" + r.c.hash + "</code><span class=\"d-csub\">" + esc(r.c.s) + "</span><span class=\"d-muted\">" + r.p.name + "</span><span class=\"d-muted\">" + esc(ago(r.c.when)) + "</span></button>";
    }).join("");
  }

  function outline() {
    var h = "";
    sections().forEach(function (s) {
      if (s.files.length) {
        h += "<div class=\"d-orepo\">" + s.repo + "</div>";
        s.files.forEach(function (f, i) { h += outlineRow(f, s.key + "/" + i); });
      }
      if (s.unc) {
        h += "<div class=\"d-orepo d-warn\">" + (state.dirty ? s.repo : esc(w("unc"))) + "</div>";
        s.unc.files.forEach(function (f, i) { h += outlineRow(f, s.key + "/unc/" + i); });
      }
    });
    return "<aside class=\"d-outline\" id=\"d-outline\">" + h + "</aside>";
  }
  function outlineRow(f, key) {
    var c = counts([f]), total = c.a + c.d, bar = Math.min(28, 6 + total * 2), g = total ? Math.round(bar * c.a / total) : 0;
    return "<button class=\"d-orow\" data-jump=\"" + key + "\"><span>" + esc(f.name) + "</span><i class=\"d-bar\"><b style=\"width:" + g + "px\"></b><s style=\"width:" + (bar - g) + "px\"></s></i></button>";
  }

  function anchor(key) { return "d-" + key.replace(/[^\w-]/g, "_"); }

  function trackOutline() {
    var s = document.getElementById("d-stream"), o = document.getElementById("d-outline");
    if (!s || !o) return;
    var rows = o.querySelectorAll(".d-orow");
    function update() {
      var cur = null, top = s.getBoundingClientRect().top + 40;
      rows.forEach(function (r) {
        var t = document.getElementById(anchor(r.dataset.jump));
        if (t && t.getBoundingClientRect().top <= top) cur = r;
      });
      if (!cur && rows.length) cur = rows[0];
      rows.forEach(function (r) { r.classList.toggle("cur", r === cur); });
    }
    s.addEventListener("scroll", update, { passive: true });
    update();
  }

  // ---------- change window ----------
  function openCommit(ref) {
    var parts = ref.split("/"), req = REQS[state.req], p = req.repos.filter(function (x) { return x.name === parts[0]; })[0];
    var c = p.commits.filter(function (x) { return x.hash === parts[1]; })[0];
    var files = (c.files || p.files.map(function (_, i) { return i; })).map(function (i) { return p.files[i]; });
    var idx = 0;
    function draw() {
      modal("<div class=\"d-win\"><div class=\"d-wtitle\"><div class=\"d-lights\"><i data-close></i><i></i><i></i></div><code>" + c.hash + "</code><span>" + esc(c.s) + "</span>" +
        "<span class=\"d-right d-muted\">" + p.name + "</span></div><div class=\"d-wbody\"><div class=\"d-wtree\">" +
        files.map(function (f, i) { return "<button class=\"d-orow" + (i === idx ? " cur" : "") + "\" data-wfile=\"" + i + "\"><span>" + esc(f.name) + "</span></button>"; }).join("") +
        "</div><div class=\"d-wdiff\">" + diff(files[idx]) + "</div></div>" +
        "<div class=\"d-status\"><span class=\"d-crumb\">" + esc(w("agent")) + " · " + esc(ago(c.when)) + "</span></div></div>", function (e) {
          var b = e.target.closest("[data-wfile]");
          if (b) { idx = +b.dataset.wfile; draw(); }
        });
    }
    draw();
  }

  // ---------- batch actions ----------
  function plan(kind) {
    var req = REQS[state.req], rows = [];
    if (kind === "mr") {
      req.repos.forEach(function (p, i) {
        if (p.stage > 1) return;
        var k = p.github ? "PR" : "MR", push = p.stage === 0;
        rows.push({ repo: p.name, way: p.branch + " → master", brief: w(push ? "mrPush" : "mrOpen", { kind: k }), ok: true, done: w("mrDone", { mr: (p.github ? "#" : "!") + (40 + i * 7) }) });
      });
    } else {
      req.repos.forEach(function (p) {
        ["master"].forEach(function (b) {
          if (p.unc && b === "master") {
            rows.push({ repo: p.name, way: b + " ← origin", brief: w("syncDirty", { wt: p.unc.dir }), ok: false });
          } else {
            var n = (p.name.length + b.length) % 4;
            rows.push({ repo: p.name, way: b + " ← origin", brief: w("syncRef"), ok: true, done: n ? w("syncDone") + " · " + n : w("upToDate") });
          }
        });
      });
    }
    return rows;
  }

  function confirmPlan(kind) {
    var rows = plan(kind), go = rows.filter(function (r) { return r.ok; }).length;
    var title = kind === "mr" ? w("mrTitle", { n: go }) : w("syncTitle", { n: go });
    var promise = kind === "mr" ? w("mrPromise") : w("syncPromise");
    function rowsHtml() {
      return rows.map(function (r, i) {
        return "<div class=\"d-prow" + (r.ok ? "" : " skip") + "\" id=\"d-prow-" + i + "\"><b>" + r.repo + "</b><span class=\"d-muted\">" + esc(r.way) + "</span><span class=\"d-right\">" + esc(r.result || r.brief) + "</span></div>";
      }).join("");
    }
    modal("<div class=\"d-dialog\"><h4>" + esc(title) + "</h4><p class=\"d-muted\">" + esc(promise) + "</p><div class=\"d-plan\">" + rowsHtml() + "</div>" +
      "<div class=\"d-dbtns\"><button data-close>" + esc(w("cancel")) + "</button><button class=\"d-primary\" data-go>" + esc(kind === "mr" ? w("mrGo", { n: go }) : w("syncGo", { n: go })) + "</button></div></div>",
      function (e) {
        if (!e.target.closest("[data-go]")) return;
        var btns = root.querySelector(".d-dbtns");
        btns.innerHTML = "<span class=\"d-muted\">" + esc(w("running")) + "</span>";
        var i = 0;
        (function step() {
          while (i < rows.length && !rows[i].ok) i++;
          if (i >= rows.length) {
            btns.innerHTML = "<button class=\"d-primary\" data-close>" + esc(w("close")) + "</button>";
            toast(kind === "mr" ? w("mrs", { n: go }) : w("synced", { n: go, s: rows.length - go }));
            return;
          }
          var el = document.getElementById("d-prow-" + i);
          el.classList.add("doing");
          setTimeout(function () {
            el.classList.remove("doing");
            el.classList.add("ok");
            el.querySelector(".d-right").textContent = "✓ " + rows[i].done;
            i++;
            step();
          }, 380);
        })();
      });
  }

  function refresh() {
    var n = new Set([].concat.apply([], REQS.map(function (r) { return r.repos.map(function (p) { return p.name; }); }))).size, i = 0;
    state.busy = true;
    render();
    var t = setInterval(function () {
      i++;
      var task = document.getElementById("d-task");
      if (task) task.innerHTML = esc(w("fetching", { i: i, n: n })) + "<i class=\"d-prog\"><b style=\"width:" + Math.round(100 * i / n) + "%\"></b></i>";
      if (i >= n) {
        clearInterval(t);
        setTimeout(function () { state.busy = false; render(); toast(w("fetched", { n: n })); }, 300);
      }
    }, 160);
  }

  var modalClick = null;
  function modal(html, onClick) {
    var host = document.getElementById("d-modal");
    host.innerHTML = html;
    host.classList.add("open");
    modalClick = onClick;
  }
  function closeModal() {
    var host = document.getElementById("d-modal");
    if (host) { host.classList.remove("open"); host.innerHTML = ""; }
    modalClick = null;
  }
  function toast(text) {
    var el = document.createElement("div");
    el.className = "d-toast";
    el.textContent = text;
    root.appendChild(el);
    setTimeout(function () { el.classList.add("gone"); }, 2200);
    setTimeout(function () { el.remove(); }, 2600);
  }

  // ---------- events ----------
  root.addEventListener("click", function (e) {
    var t = e.target;
    var host = document.getElementById("d-modal");
    if (host && host.contains(t)) {
      if (t === host || t.closest("[data-close]")) { closeModal(); return; }
      if (modalClick) modalClick(e);
      return;
    }
    var b;
    if ((b = t.closest("[data-req]"))) { state.req = +b.dataset.req; state.dirty = false; state.view = "all"; state.tab = "changes"; render(); return; }
    if (t.closest("[data-dirty]")) { state.dirty = true; render(); return; }
    if ((b = t.closest("[data-tab]"))) { state.tab = b.dataset.tab; render(); return; }
    if ((b = t.closest("[data-fold]"))) {
      var key = b.dataset.fold, shut = !state.collapsed[key];
      if (e.altKey) {
        root.querySelectorAll("[data-fold]").forEach(function (x) { state.collapsed[x.dataset.fold] = shut; });
      } else state.collapsed[key] = shut;
      var s = document.getElementById("d-stream"), y = s.scrollTop;
      render();
      document.getElementById("d-stream").scrollTop = y;
      return;
    }
    if ((b = t.closest("[data-jump]"))) {
      if (state.tab !== "changes") { state.tab = "changes"; render(); }
      var el = document.getElementById(anchor(b.dataset.jump)), st = document.getElementById("d-stream");
      if (el && st) {
        var top = el.getBoundingClientRect().top - st.getBoundingClientRect().top + st.scrollTop;
        if (!el.classList.contains("d-sec")) top -= el.closest(".d-sec").querySelector(".d-repo").offsetHeight;
        st.scrollTo({ top: top, behavior: "smooth" });
      }
      return;
    }
    if ((b = t.closest("[data-commit]"))) { openCommit(b.dataset.commit); return; }
    if ((b = t.closest("[data-act]"))) {
      if (b.dataset.act === "refresh") refresh();
      else confirmPlan(b.dataset.act);
    }
  });
  root.addEventListener("change", function (e) {
    if (e.target.matches("[data-view]")) { state.view = e.target.value; render(); }
  });
  root.addEventListener("input", function (e) {
    if (e.target.matches(".d-find") && !e.isComposing) { state.query = e.target.value; state.focusFind = true; render(); }
  });
  root.addEventListener("compositionend", function (e) {
    if (e.target.matches(".d-find")) { state.query = e.target.value; state.focusFind = true; render(); }
  });
  root.addEventListener("keydown", function (e) {
    if (e.key === "Escape") closeModal();
    if (e.target.matches(".d-find") && e.key === "Enter") {
      var first = root.querySelector(".d-left .d-req[data-req]");
      if (first) first.click();
    }
  });
  document.addEventListener("langchange", function () { closeModal(); render(); });

  render();
})();

// Package track is the requirement page (需求页) of the workspace window: one requirement across the
// repos it touches: how far each repo has got, every changed file's merged diff in one stream, the commits behind them,
// and the batch actions on its rows (获取, 同步, 创建 MR). It talks to session only, never to git.
package track

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffview"
	"sheaf/internal/native/shell"
	"sheaf/internal/native/view"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

// commitPage is how many rows one Rows call fetches for the commits list.
const commitPage = 128

// tempFilter is a requirement picked on the left, a branch opened by an intent or the commit text typed in
// the search field. There is one at a time and it is never saved.
type tempFilter struct {
	Req    string // the requirement number; "" for a branch or a text
	Title  string // the requirement's title
	Branch string
	Text   string // commits whose message holds this
	Cond   query.Conditions
}

// name is what the filter is called: its requirement number, else its branch, else its text.
func (t *tempFilter) name() string {
	switch {
	case t.Req != "":
		return t.Req
	case t.Branch != "":
		return t.Branch
	}
	return t.Text
}

// Dock is the page's arrangement of a first launch: the requirements on the left, the stream filling the rest.
func Dock() shell.Dock {
	return shell.Dock{Left: shell.Side{Size: railWidth, Open: true}}
}

// Page is the filter page's state. Every field is read and written on the main thread (the view, and the functions given
// to Host.Update); background work hands its results over through Update.
type Page struct {
	h view.Host
	s *session.Session
	w *words // the words of the frame being built, for code that runs between frames

	want      string // wcID or tmpID; empty selects the first discovered requirement
	temp      *tempFilter
	master    string // "" = each repo's own base
	diffMode  string
	diffStage int
	flow      []session.MergeStep
	setting   string // the other settings the page reads by (settingKey), to tell which saved settings concern it
	favs      []string

	branches map[string]string // the branch picked on this page per row, not saved with the filter
	input    *session.TagAggInput
	loaded   string // the key of the input the overview was asked for
	res      *session.TagAggResult
	aggErr   string
	aggBusy  bool
	aggSeq   int
	rows     []session.AggRepo
	frozen   []string // the order of the rows before a batch action, kept until the page is entered again or another filter shows

	files       []session.AggFile
	filesVer    int                 // bumped as files is replaced
	filesReread int                 // the content refresh that produced files
	filesIn     session.TagAggInput // the input files was read with
	filesBusy   bool
	filesErr    string
	filesSeq    int

	avail    *session.DiffAvail // the files of each way of viewing the changes, to grey out the empty ones; nil until read
	availFor string             // the input avail was asked for, whatever its diff mode
	availSeq int

	checked    map[string]bool // the rows batch actions act on: every row that can take them
	focus      string          // the row whose ↓ the commits tab shows; "" = the commits of every row
	rowsKey    string
	tab        int // 0 changes, 1 commits
	side       int // commits tab, one row focused: 0 its own ↑, 1 what the compare target has beyond it ↓
	merges     bool
	allCommits *commitList // every row, also the shared count of the title and tab
	commits    *commitList
	down       *commitList
	listSeq    int
	commitSt   ui.ListState
	commitSel  int
	commitOf   *commitList

	reqs   []session.Requirement // the tracked requirements of the left pane, stage by stage
	cands  []session.Candidate   // numbers found but not tracked
	reqsOK bool
	reqSeq int
	find   string // the left pane's search

	focusFind bool // the search field takes the focus once: the list is empty

	archivedOpen bool

	renaming    string // the requirement whose row is a field now
	renameText  string
	renameFocus bool // the field had the focus in the last frame: losing it cancels

	stream    *diffview.Stream
	streamKey string                                 // what the stream was given
	stats     map[string]map[string]session.FileStat // row -> path -> lines added and deleted
	statsFor  string                                 // the input the stats belong to
	reread    int                                    // bumped by a forced read: the diffs are read again too
	statsSeq  int
	outline   ui.ListState

	stopPrefetch context.CancelFunc // ends the running prefetch of the other requirements

	mrRun   *session.MRRun
	syncRun *session.BatchRun
	mrFor   string // filterKey of the view each run belongs to
	syncFor string
	sheet   *sheet

	pop popup // the one open compare-target or branch popover

	dirty dirtyPage

	wantSmoke string
	smokeRow  int // the smoke's row to scroll the stream to once the rows come (-1 none)

	prev    *Page     // the page as it was before another entry was chosen, drawn until the new one settles
	prevAt  time.Time // when prev was first drawn
	visible bool
	pending bool
}

// commitList is a page source of the commits tab: the rows are fetched a page at a time from the result.
type commitList struct {
	session.AggCommits
	pages map[int][]session.Row
}

// New makes the requirement page and reads the discovered requirements.
func New(h view.Host) *Page {
	p := &Page{h: h, s: h.Session(), visible: true, diffMode: session.DiffBase, branches: map[string]string{}, checked: map[string]bool{}, commitSel: -1, smokeRow: -1, stream: diffview.NewStream()}
	p.follow(h.Info())
	p.favs = p.s.Favs()
	p.commitSt.Selected = &p.commitSel
	p.initStream()
	p.dirty.init(p)
	p.reload()
	return p
}

// Close stops the loads of the stream; the others end with the window's context.
func (p *Page) Close() {
	p.stream.Close()
	p.dirty.stream.Close()
}

// Open takes a branch intent or the screenshot scenario.
func (p *Page) Open(intent any) {
	switch in := intent.(type) {
	case view.UncommittedIntent:
		p.choose(wcID)
	case view.TempIntent:
		p.chooseTemp(in.Branch)
	case view.Smoke:
		p.wantSmoke, p.smokeRow = in.Q, in.Row
		p.pickSmoke()
		return
	}
	p.reload()
}

// pickSmoke shows a discovered requirement by number, otherwise a branch.
func (p *Page) pickSmoke() {
	if p.wantSmoke == "" || !p.reqsOK {
		return
	}
	q := p.wantSmoke
	p.wantSmoke = ""
	for _, r := range p.reqs {
		if r.ID == q {
			p.chooseReq(r)
			return
		}
	}
	if id, ok := p.normalize(q); ok {
		r := session.Requirement{ID: id}
		for _, k := range p.cands {
			if k.ID == id {
				r.Title = k.Title
			}
		}
		p.chooseReq(r)
		return
	}
	p.chooseTemp(q)
	p.apply(true)
}

// Event follows the session: the page reads again when the index has new content.
func (p *Page) Event(e view.Event) {
	switch e := e.(type) {
	case view.StateChanged:
		if e.Now.Ready && !e.Was.Ready {
			p.reload()
		}
	case view.IndexChanged:
		p.follow(p.h.Info())
		if !p.affectedRepos(e.Update.ChangedRepos) || p.holdChanges() {
			p.loadReqs()
		} else {
			p.reload()
		}
	case view.ConfigChanged:
		// The page's own saves come back here too, with the values it already has.
		targets, other := p.follow(p.h.Info())
		if other {
			p.reload()
		} else if targets {
			p.apply(false)
		}
	case view.WorktreesChanged:
		if p.affectedWorktrees(e.Status.Worktrees) && !p.holdChanges() {
			p.reload()
			return
		}
		p.loadReqs()
	case view.VisibilityChanged:
		p.visible = e.Visible
		p.refreshIfHidden()
	}
}

// follow takes the compare branches and settings of inf, and tells which of them differ from the page's.
func (p *Page) follow(inf session.Info) (targets, other bool) {
	targets = inf.Master != p.master
	key := settingKey(inf)
	other = key != p.setting
	p.master, p.setting = inf.Master, key
	if p.diffStage < len(p.flow) {
		branch := p.flow[p.diffStage].Branch
		if index := slices.IndexFunc(inf.MergeFlow, func(step session.MergeStep) bool { return step.Branch == branch }); index >= 0 {
			p.diffStage = index
		}
	}
	p.flow = slices.Clone(inf.MergeFlow)
	p.diffStage = max(0, min(p.diffStage, len(p.flow)-1))
	return targets, other
}

// settingKey sums up saved settings and worktree ownership that change what the page shows.
func settingKey(inf session.Info) string {
	var b strings.Builder
	b.WriteString(inf.Requirement)
	fmt.Fprint(&b, inf.MergeFlow)
	for _, r := range inf.Repos {
		if r.BaselineOnly {
			fmt.Fprintf(&b, "\x00%s:baseline", r.Name)
		}
		if !r.BaseAuto && r.Base != "" {
			fmt.Fprintf(&b, "\x00%s=%s", r.Name, r.Base)
		}
		for _, w := range r.Worktrees {
			if w.Requirement != "" {
				fmt.Fprintf(&b, "\x00%s:requirement=%s", w.Path, w.Requirement)
			}
		}
	}
	return b.String()
}

// chooseTemp shows branch as an unsaved filter. Every author, all time: the branch's own commits, whoever made them.
func (p *Page) chooseTemp(branch string) {
	changed := p.temp == nil || p.temp.Req != "" || p.temp.Text != "" || p.temp.Branch != branch
	c := query.Default()
	c.Branches = []string{branch}
	p.temp = &tempFilter{Branch: branch, Cond: c}
	p.chooseWith(tmpID, changed)
}

// chooseText shows the commits whose message holds text, whoever made them and whenever.
func (p *Page) chooseText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	changed := p.temp == nil || p.temp.Req != "" || p.temp.Branch != "" || p.temp.Text != text
	c := query.Default()
	c.Q = text
	p.temp = &tempFilter{Text: text, Cond: c}
	p.chooseWith(tmpID, changed)
}

// chooseReq shows requirement r: an unsaved filter whose commit text is its number, every author, all time.
func (p *Page) chooseReq(r session.Requirement) {
	changed := p.temp == nil || p.temp.Req != r.ID
	c := query.Default()
	c.Q = r.ID
	p.temp = &tempFilter{Req: r.ID, Title: r.Title, Cond: c}
	p.chooseWith(tmpID, changed)
}

// shownReq is the requirement on screen, or empty for a branch or uncommitted changes.
func (p *Page) shownReq() string {
	if p.picked() == tmpID && p.temp != nil {
		return p.temp.Req
	}
	return ""
}

// filterKey identifies the requirement or temporary branch that owns a batch result.
func (p *Page) filterKey() string {
	if p.input == nil || p.temp == nil {
		return ""
	}
	return tmpID + ":" + p.temp.name()
}

// choose shows entry id (a filter, wcID or tmpID); the picks made on the page for the previous one are dropped.
func (p *Page) choose(id string) { p.chooseWith(id, false) }

// chooseWith is choose; force drops the picks also when id is already shown (a temporary filter of another branch).
func (p *Page) chooseWith(id string, force bool) {
	if !force && (id == p.want || id == p.picked()) {
		// Shown already, as the entry picked for want "" : the page stays as it is.
		p.want = id
		return
	}
	p.want = id
	p.clearChanges()
	if p.prev == nil && p.res != nil && p.aggErr == "" {
		old := *p
		p.prev, p.prevAt = &old, time.Time{}
	}
	p.sheet = nil // its rows belong to the previous filter
	p.branches = map[string]string{}
	p.checked, p.focus, p.rowsKey = map[string]bool{}, "", ""
	p.frozen, p.res, p.rows, p.aggErr = nil, nil, nil, ""
	p.files, p.allCommits, p.commits, p.down = nil, nil, nil, nil
	p.avail, p.availFor = nil, ""
	p.loaded = ""
	p.pop = popup{}
	p.tab, p.side, p.stats, p.statsFor = 0, 0, nil, ""
	p.statsSeq++
	p.streamKey = ""
	if p.prev == nil {
		p.stream.ScrollToTop()
		p.outline.ScrollTo(0, ui.Start)
	}
	p.apply(true)
}

// shown is the page to draw: the previous one while the entry chosen has not settled, so a switch replaces the whole
// page in one frame rather than passing through its loading states.
func (p *Page) shown() *Page {
	if p.prev == nil {
		return p
	}
	if id := p.picked(); id == wcID || id == "" || p.settled() {
		p.prev = nil
		p.stream.ScrollToTop()
		p.outline.ScrollTo(0, ui.Start)
		return p
	}
	return p.prev
}

// settled keeps the previous entry visible until the next entry's overview, files and line counts are ready, or a read failed.
func (p *Page) settled() bool {
	switch {
	case p.aggErr != "" || p.input == nil:
		return true
	case p.res == nil || p.aggBusy:
		return false
	case len(p.rows) == 0 || p.filesErr != "":
		return true
	case p.filesBusy:
		return false
	}
	for _, r := range p.rows {
		if _, ok := p.stats[r.Key]; !ok {
			return false
		}
	}
	return true
}

// words are the words of the last frame, for what runs between frames.
func (p *Page) words() *words {
	if p.w == nil {
		return tr.For("")
	}
	return p.w
}

// dirtyDirs counts all directories with uncommitted changes.
func (p *Page) dirtyDirs() int {
	n := 0
	for _, r := range p.h.Info().Repos {
		for _, w := range r.Worktrees {
			if w.Dirty > 0 {
				n++
			}
		}
	}
	return n
}

// ready: the discovered requirements have been read, or an explicit entry was requested.
func (p *Page) ready() bool {
	return p.want != "" || p.reqsOK
}

// picked is the entry on screen.
func (p *Page) picked() string {
	if !p.ready() {
		return ""
	}
	return pickEntry(p.want, p.temp != nil, p.dirtyDirs())
}

// tagName is the name of the entry on screen.
func (p *Page) tagName(w *words) string {
	switch id := p.picked(); id {
	case wcID:
		return w.Uncommitted
	case tmpID:
		if p.temp != nil {
			return p.temp.name()
		}
	}
	return ""
}

// inputFor is what the overview of entry id is asked with; nil when there is none to show.
func (p *Page) inputFor(id string) *session.TagAggInput {
	switch id {
	case "", wcID:
		return nil
	case tmpID:
		c := p.temp.Cond
		return &session.TagAggInput{Base: p.master, DiffMode: p.diffMode, DiffStage: p.diffStage, Branches: p.branches, Cond: &c, Requirement: p.temp.Req}
	}
	return nil
}

// reload reads requirements again and refreshes the current overview after an index change.
func (p *Page) reload() {
	if !p.h.State().Ready {
		return
	}
	p.apply(true)
	p.loadReqs()
}

// loadReqs reads the requirements of the left pane (index only, ~100 ms when something changed, cached otherwise). With
// nothing asked for yet, the first requirement shows. Then git is asked for the patch comparisons the first list lacked;
// when that changes a stage the list is read again and replaces the shown one, with nothing cleared in between.
func (p *Page) loadReqs() {
	if !p.h.State().Ready {
		return
	}
	p.reqSeq++
	seq, s, ctx := p.reqSeq, p.s, p.h.Caller()
	go func() {
		reqs, err := s.Requirements(ctx)
		cands, _ := s.RequirementCandidates(ctx)
		p.h.Update(func() {
			if seq == p.reqSeq {
				p.cands = cands
				p.gotReqs(reqs, err, false)
			}
		})
		if err != nil {
			return
		}
		if changed, err := s.RequirementsSettle(ctx); err != nil || !changed {
			return
		}
		reqs, err = s.Requirements(ctx)
		cands, _ = s.RequirementCandidates(ctx)
		p.h.Update(func() {
			if seq == p.reqSeq {
				p.cands = cands
				p.gotReqs(reqs, err, true)
			}
		})
	}()
}

// rename saves a requirement's own title (blank: the commits' again) and reads the list again.
func (p *Page) rename(id, name string) {
	s := p.s
	go func() {
		err := s.SetRequirementName(id, name)
		p.h.Update(func() {
			if err != nil {
				p.h.Toasts().Error(err.Error())
				return
			}
			p.loadReqs()
		})
	}()
}

// normalize is text as a requirement number of the workspace, and whether it is one.
func (p *Page) normalize(text string) (string, bool) {
	if p.s == nil {
		return "", false
	}
	return p.s.NormalizeRequirement(text)
}

// track adds requirement id to the tracked ones, shows it and empties the search field.
func (p *Page) track(id, title string) {
	s := p.s
	p.find = ""
	p.chooseReq(session.Requirement{ID: id, Title: title})
	go func() {
		err := s.Track(id)
		p.h.Update(func() {
			if err != nil {
				p.h.Toasts().Error(err.Error())
				return
			}
			p.loadReqs()
		})
	}()
}

// untrack drops requirement id from the tracked ones for five seconds of undo. What is on screen stays.
func (p *Page) untrack(id string) {
	s := p.s
	go func() {
		err := s.Untrack(id)
		p.h.Update(func() {
			if err != nil {
				p.h.Toasts().Error(err.Error())
				return
			}
			p.loadReqs()
			p.h.Toasts().Undo(fill(p.w.Untracked, "id", id), func() {
				go func() {
					err := s.Track(id)
					p.h.Update(func() {
						if err != nil {
							p.h.Toasts().Error(err.Error())
							return
						}
						p.loadReqs()
					})
				}()
			}, "")
		})
	}()
}

// gotReqs takes a list of requirements for the left pane. The requirement on screen stays on screen.
func (p *Page) gotReqs(reqs []session.Requirement, err error, force bool) {
	if err != nil {
		reqs = nil
	}
	if !p.reqsOK && len(reqs) == 0 {
		p.focusFind = true
	}
	p.reqs, p.reqsOK = reqs, true
	p.pickSmoke()
	if p.want == "" && len(reqs) > 0 && p.wantSmoke == "" {
		p.chooseReq(reqs[0])
		return
	}
	if p.temp != nil && p.temp.Req != "" {
		for _, r := range reqs {
			if r.ID == p.temp.Req {
				p.temp.Title = r.Title
			}
		}
	}
	if !p.pending {
		p.applyAggregate(force, true)
	}
}

// apply asks for what the entry on screen needs, when the input changed or force is set.
func (p *Page) apply(force bool) {
	if force {
		p.clearChanges()
		p.reread++
	}
	p.applyAggregate(force, false)
}

func (p *Page) applyAggregate(force, stagesOnly bool) {
	if !p.ready() {
		return
	}
	id := p.picked()
	if id == wcID {
		p.dirty.load(p)
		return
	}
	in := p.inputFor(id)
	p.input = in
	if in == nil {
		p.stopAggregateLoads()
		p.res, p.rows, p.aggErr = nil, nil, ""
		return
	}
	b, _ := json.Marshal(in)
	key := string(b)
	if !force && key == p.loaded {
		return
	}
	stagesOnly = stagesOnly && force && !p.aggBusy && p.res != nil && key == p.loaded
	p.loaded = key
	if !stagesOnly {
		p.stopAggregateLoads()
		p.aggBusy = true
	}
	seq, s, ctx, input := p.aggSeq, p.s, p.h.Caller(), *in
	go func() {
		res, err := s.TagAgg(ctx, input)
		p.h.Update(func() {
			if seq != p.aggSeq {
				return
			}
			if !stagesOnly {
				p.aggBusy = false
			}
			if err != nil {
				p.stopAggregateLoads()
				p.res, p.rows, p.aggErr = nil, nil, err.Error()
				return
			}
			p.res, p.aggErr = &res, ""
			p.rebuildRows()
			p.onRows()
			if stagesOnly {
				p.streamKey = ""
				return
			}
			if i := p.smokeRow; i >= 0 && i < len(p.rows) {
				p.smokeRow = -1
				p.stream.ScrollToGroup(p.rows[i].Key)
			}
			p.loadRemerge(input, seq)
			p.loadFiles(input)
			p.loadAvail(input, force)
			p.loadStats(input)
			p.loadCommits(input)
		})
	}()
}

func (p *Page) stopAggregateLoads() {
	p.aggSeq++
	p.filesSeq++
	p.statsSeq++
	p.aggBusy, p.filesBusy = false, false
}

// rebuildRows lays the frozen order over the overview.
func (p *Page) rebuildRows() {
	if p.res == nil {
		p.rows = nil
		return
	}
	p.rows = keepOrder(p.res.Repos, p.frozen)
	// The repos still being worked on come first: they are what there is to review.
	slices.SortStableFunc(p.rows, func(a, b session.AggRepo) int { return stageRank(a.Stage) - stageRank(b.Stage) })
}

// stageRank is where stage comes in stageOrder; a row without one goes last.
func stageRank(stage string) int {
	return session.StageRank(stage)
}

// onRows ticks the default rows when the set of rows changed (another filter, a branch picked), and drops a focus that is gone.
func (p *Page) onRows() {
	keys := make([]string, len(p.rows))
	for i, r := range p.rows {
		keys[i] = r.Key
	}
	k := p.picked() + "\x00" + strings.Join(sortedCopy(keys), "\x00")
	if k == p.rowsKey {
		return
	}
	p.rowsKey = k
	p.checked = defaultChecked(p.rows)
	found := false
	for _, r := range p.rows {
		found = found || r.Key == p.focus
	}
	if !found {
		p.focus, p.side = "", 0
	}
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// loadRemerge classifies, in the background (one git call per repo, ~8 ms a merge), the merge commits of the rows into
// clean and edited while merging; when some are edited, the overview, files and commits are read again with them. seq is
// the overview request it follows.
func (p *Page) loadRemerge(in session.TagAggInput, seq int) {
	s, ctx := p.s, p.h.Caller()
	go func() {
		changed, err := s.TagAggRemerge(ctx, in)
		p.h.Update(func() {
			if seq != p.aggSeq || err != nil || !changed {
				return
			}
			if p.pending {
				p.loadReqs()
				return
			}
			// Classification adds merge parts only to the files whose commit counts changed.
			p.applyAggregate(true, false)
			p.loadReqs()
		})
	}()
}

// loadFiles reads the changed files (git status in the worktrees: ~0.2 s); the previous list stays until the new one comes.
func (p *Page) loadFiles(in session.TagAggInput) {
	p.filesSeq++
	p.filesBusy = true
	seq, s, ctx, reread := p.filesSeq, p.s, p.h.Caller(), p.reread
	go func() {
		files, err := s.TagAggFiles(ctx, in)
		p.h.Update(func() {
			if seq != p.filesSeq {
				return
			}
			p.filesBusy = false
			p.files, p.filesErr, p.filesIn = files, "", in
			p.filesReread = reread
			p.filesVer++
			if err != nil {
				p.filesErr = err.Error()
			}
		})
	}()
}

// loadAvail counts the files of every way of viewing in's changes, unless they were counted for in already (in another
// diff mode) and force is off.
func (p *Page) loadAvail(in session.TagAggInput, force bool) {
	in.DiffMode, in.DiffStage = "", 0
	b, _ := json.Marshal(in)
	if !force && string(b) == p.availFor {
		return
	}
	if string(b) != p.availFor {
		p.avail = nil
	}
	p.availFor = string(b)
	p.availSeq++
	seq, s, ctx := p.availSeq, p.s, p.h.Caller()
	go func() {
		avail, err := s.TagAggDiffAvail(ctx, in)
		p.h.Update(func() {
			if seq != p.availSeq || err != nil {
				return
			}
			p.avail = &avail
		})
	}()
}

// diffEmpty: mode (stage: at flow step stage) is known to show no file.
func (p *Page) diffEmpty(mode string, stage int) bool {
	a := p.avail
	switch {
	case a == nil:
		return false
	case mode == session.DiffWorktree:
		return a.Worktree == 0
	case mode == session.DiffStage:
		return stage < len(a.Stages) && a.Stages[stage] == 0
	}
	return a.Base == 0
}

// loadCommits asks for the commits of the focused row (or all rows) and, when a row is focused, what the compare target has beyond it.
func (p *Page) loadCommits(in session.TagAggInput) {
	p.listSeq++
	seq, s, focus, merges := p.listSeq, p.s, p.focus, p.merges
	go func() {
		all, err := s.TagAggCommits(in, "", merges)
		c := all
		if err == nil && focus != "" {
			c, err = s.TagAggCommits(in, focus, merges)
		}
		var d session.AggCommits
		var derr error = fmt.Errorf("no row")
		if focus != "" {
			d, derr = s.TagAggDown(in, focus)
		}
		p.h.Update(func() {
			if seq != p.listSeq {
				return
			}
			p.allCommits, p.commits, p.down = nil, nil, nil
			if err == nil {
				p.allCommits = &commitList{AggCommits: all, pages: map[int][]session.Row{}}
				p.commits = p.allCommits
				if focus != "" {
					p.commits = &commitList{AggCommits: c, pages: map[int][]session.Row{}}
				}
			}
			if derr == nil {
				p.down = &commitList{AggCommits: d, pages: map[int][]session.Row{}}
			}
			p.commitSel = -1
		})
	}()
}

// commitRow is row i of a commits list.
func (p *Page) commitRow(l *commitList, i int) (session.Row, bool) {
	pg := i / commitPage
	rows, ok := l.pages[pg]
	if !ok {
		var err error
		rows, err = p.s.Rows(l.ResultID, pg*commitPage, commitPage)
		if err != nil {
			return session.Row{}, false
		}
		l.pages[pg] = rows
	}
	if k := i - pg*commitPage; k < len(rows) {
		return rows[k], true
	}
	return session.Row{}, false
}

// setFocus limits the commits tab to row key ("" = all rows).
func (p *Page) setFocus(key string) {
	if p.focus == key {
		return
	}
	p.focus = key
	p.side = 0
	if p.input != nil {
		p.loadCommits(*p.input)
	}
}

// setBranch picks the branch of a row for this page only ("" = back to the automatic one).
func (p *Page) setBranch(row, branch string) {
	p.branches = withBranch(p.branches, row, branch)
	p.apply(false)
}

// setTargets saves the workspace's compare branches and reads the filter again against them.
func (p *Page) setBase(master string) {
	p.master = master
	if err := p.s.SetCompareBase(master); err != nil {
		p.h.Toasts().Error(err.Error())
	}
	p.apply(false)
}

func (p *Page) setFavs(favs []string) {
	p.favs = favs
	if err := p.s.SetFavs(favs); err != nil {
		p.h.Toasts().Error(err.Error())
	}
}

// ticked are the ticked rows in table order.
func (p *Page) ticked() []string {
	var out []string
	for _, r := range p.rows {
		if p.checked[r.Key] {
			out = append(out, r.Key)
		}
	}
	return out
}

func (p *Page) rowOf(key string) (session.AggRepo, bool) {
	for _, r := range p.rows {
		if r.Key == key {
			return r, true
		}
	}
	return session.AggRepo{}, false
}

// shownFiles are the files the stream shows: with 仅未提交 only the uncommitted ones.
func (p *Page) shownFiles() []session.AggFile {
	out := slices.Clone(p.files)
	at := make(map[string]int, len(p.rows))
	for i, r := range p.rows {
		at[r.Key] = i
	}
	slices.SortStableFunc(out, func(a, b session.AggFile) int { return at[a.Key] - at[b.Key] })
	return out
}

// worktreeFiles counts the separate worktree entries; the changes tab counts only committed files.
func (p *Page) worktreeFiles() int {
	n := 0
	for _, f := range p.files {
		if f.Worktree {
			n++
		}
	}
	return n
}

// review opens file f in the diff window, which steps through every file of the same diff mode.
func (p *Page) review(f session.AggFile) {
	if p.input == nil {
		return
	}
	in := p.filesIn
	if f.Worktree {
		in.DiffMode = session.DiffWorktree
	}
	src := session.DiffSource{Kind: "agg", Repo: f.Repo, Row: f.Key, Agg: &in, All: true, Only: f.Worktree, Untracked: f.Untracked, OldPath: f.OldPath}
	src.Path = f.Path
	if err := p.h.Windows().OpenDiff(src); err != nil {
		p.h.Toasts().Error(err.Error())
	}
}

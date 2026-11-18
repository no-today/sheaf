// Package session is one open workspace: it owns the index, the Tags and the
// workspace config, keeps the index fresh, and serves native views.
package session

import (
	"cmp"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"sheaf/internal/discover"
	"sheaf/internal/engine"
	"sheaf/internal/gitcli"
	"sheaf/internal/gitview"
	"sheaf/internal/index"
	"sheaf/internal/query"
	"sheaf/internal/watch"
)

// Config is workspace.json: per-workspace settings.
type Config struct {
	Root string `json:"root,omitempty"`
	// MeManual are the emails the user added by hand; "me" is these plus the ones derived from git config (meLocked).
	MeManual []string `json:"meManual"`
	// OldMe / OldMeSet only migrate files from before "me" became derived plus added: there me was the whole list when meSet was true.
	OldMe    []string          `json:"me,omitempty"`
	OldMeSet bool              `json:"meSet,omitempty"`
	Bases    map[string]string `json:"bases"`             // repo name -> base branch
	Favs     []string          `json:"favs,omitempty"`    // favourite compare targets, in the user's order
	Ignored  []string          `json:"ignored,omitempty"` // repo names not tracked: dropped right after discovery
	// Release and Master are the two branches the filter page compares with, one pair for the workspace.
	Release string `json:"release,omitempty"` // "" = DefaultRelease
	Master  string `json:"master,omitempty"`  // "" = each repo's own base
	// Requirement is the regular expression of a requirement number; "" = DefaultRequirement.
	Requirement string `json:"requirement,omitempty"`
	// RequirementNames are the titles the user gave requirements, by number: they replace the title found in commits.
	RequirementNames map[string]string `json:"requirementNames,omitempty"`
	// Tracked are the requirement numbers the user follows, in the order added; only these are listed.
	Tracked      []string    `json:"tracked,omitempty"`
	MergeFlow    []MergeStep `json:"mergeFlow,omitempty"`
	Archived     []string    `json:"archived,omitempty"`
	BaselineOnly []string    `json:"baselineOnly,omitempty"`
	// GlabConfig is the workspace's own glab configuration directory (GLAB_CONFIG_DIR); "" = the system's glab login.
	GlabConfig string `json:"glabConfig,omitempty"`
}

type Options struct {
	DataDir  string      // per-user app data (workspace.json lives below it)
	CacheDir string      // index snapshots live below it: they can always be rebuilt
	OnState  func(State) // called when the index state changes
	// OnWorktrees is called when uncommitted changes of worktrees may have changed.
	OnWorktrees func(WorktreeStatus)
	// OnIndex is called when the index has new content (or worktree branches moved).
	OnIndex func(IndexUpdate)
	// OnTasks is called (any goroutine) when a task starts, moves on or ends: Tasks reads differently.
	OnTasks func()
	// OnCommands is called (at most 4 times a second) when git commands start or end.
	OnCommands func(CmdPulse)
	// OnConfig is called (on its own goroutine) after the workspace settings were saved: Info may read differently.
	OnConfig func()
	// Watch starts a file watch on roots (watch.Files in the app); nil = no watching.
	Watch func(ctx context.Context, roots []string) (watch.Source, error)
	Now   func() time.Time
	// GitLimit bounds the git processes of every session that shares it; nil = a limit of gitParallel for this session alone.
	GitLimit *GitLimit
	// NoAutoRefresh leaves refreshing to the caller instead of starting it in Open.
	NoAutoRefresh bool
	// ReuseScan reloads known repositories instead of walking the workspace when a scan cache exists.
	ReuseScan bool
	// CacheWrite synchronously invokes write when cache writes are allowed, or skips it.
	// Nil writes directly. It covers index and scan caches; derived facts are always merged and saved.
	CacheWrite func(write func())
}

// gitParallel is the ceiling on git processes running at once (the app shares one across its windows): the index build
// takes NumCPU of them, the rest keep what the user clicks quick meanwhile. Measured on a large workspace (35 repos, 230k commits): no
// slower to open than without a ceiling; a ceiling of 8 doubled the wait of a click during the first index.
func gitParallel() int { return runtime.NumCPU() + 4 }

// GitLimit bounds the git processes running at once.
type GitLimit = gitcli.Limit

// NewGitLimit is the limit the app shares between its windows' sessions (Options.GitLimit).
func NewGitLimit() *GitLimit { return gitview.NewLimit(gitParallel()) }

type Session struct {
	Root string
	Name string

	opts          Options
	cfgPath       string
	indexPath     string
	scanPath      string // the last scan of the repos (cache dir), shown until the next scan is done
	derivedPath   string
	derivedMu     sync.Mutex
	derivedSaved  *derivedData
	derivedTimer  *time.Timer
	derivedDelay  time.Duration
	derivedClosed bool

	mu  sync.Mutex
	cfg Config
	// configBackup is where a broken workspace.json was moved aside on open; "" = none.
	configBackup string
	meAuto       []MeFrom // what the last derivation found
	repos        []discover.Repo
	webBases     map[string]string // RepoWebBases, read once
	// ignoredDirs are the git dirs and worktrees of the repos the user does not track; their changes are dropped.
	ignoredDirs []string
	ignoredWith map[string][]string // ignored repo -> repos it took along (Info.IgnoredWith)
	scanned     []discover.Repo     // every repo of the last scan, ignored or not: an ignore-list change filters it instead of scanning again
	eng         *engine.Engine
	state       State
	results     resultCache
	dirty       dirtyTracker
	outside     watch.Outside // subdirectories of watched dirs outside every repo
	ab          abCache
	cover       coverCache
	reqs        reqCache
	disc        discCache
	reach       reachCache
	remerge     remergeCache
	stats       statsCache
	latest      calls           // git-backed calls: a new one cancels the previous of its kind
	refresh     sync.Mutex      // one index update at a time
	refreshing  int             // Refresh calls running (State.Indexing); guarded by mu
	scanSeq     int             // the last scan begun (beginScan); guarded by mu
	scanTaken   int             // the newest scan applyScan took; guarded by mu
	reloaded    map[string]int  // git dir -> scanSeq when UpdateRepos last read its worktrees (freshenLocked); guarded by mu
	indexGen    int             // bumped when the index changes; guarded by mu
	saveMu      sync.Mutex      // one index write at a time: they share a temporary file
	savedGen    int             // the indexGen last written (saveIndex); guarded by saveMu
	ctx         context.Context // background work; cancelled by Close
	cancel      context.CancelFunc
	watcher     watcher
	bg          sync.WaitGroup
	rec         gitcli.Recorder // the command log
	pulseMu     sync.Mutex
	pulsing     bool       // an OnCommands call is scheduled
	writes      sync.Mutex // writes to one repo never interleave
	tasks       tasks
	indexTask   *taskRun // the index build or update running, as a task; guarded by mu
}

type result struct {
	eng  *engine.Engine
	m    *query.Matcher
	rows []index.CommitRef
	// resolved marks the merge commits of rows that someone edited while merging (Row.Resolved).
	resolved map[index.CommitRef]bool
}

const keepResults = 16

// Open loads the workspace at root: config, Tags and the index snapshot (so
// the list shows at once), then refreshes the index in the background.
func Open(ctx context.Context, root string, opts Options) (*Session, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("open workspace %s: not a directory", root)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.GitLimit == nil {
		opts.GitLimit = NewGitLimit()
	}
	id := workspaceID(root)
	s := &Session{
		Root: root, Name: filepath.Base(root), opts: opts,
		cfgPath:      filepath.Join(opts.DataDir, "workspaces", id, "workspace.json"),
		indexPath:    filepath.Join(opts.CacheDir, "workspaces", id, "index.bin"),
		scanPath:     filepath.Join(opts.CacheDir, "workspaces", id, "scan.json"),
		derivedPath:  filepath.Join(opts.CacheDir, "workspaces", id, "derived.bin"),
		derivedDelay: derivedSaveDelay,
		reloaded:     map[string]int{},
	}
	s.tasks.changed = opts.OnTasks
	s.rec = gitcli.Recorder{WriteLog: filepath.Join(opts.DataDir, "workspaces", id, "commands.jsonl"), OnChange: s.cmdChanged, Describe: sourceText, Now: opts.Now, Workspace: root}
	ctx = s.git(ctx, "open")
	if cfg, err := s.loadConfig(ctx); err == nil {
		s.cfg = cfg
	} else {
		// A file we could not read or parse stays on disk under its own name; opening continues with defaults only
		// after the broken bytes are safe in a backup. Never open a file a later write would overwrite.
		if s.configBackup == "" {
			return nil, err
		}
		s.cfg = Config{Root: root, Bases: map[string]string{}}
	}
	if s.cfg.Root != root {
		s.cfg.Root = root
		// Existing configurations retain their bytes until an explicit settings write; recent.json locates them meanwhile.
		if _, err := os.Stat(s.cfgPath); os.IsNotExist(err) && opts.CacheWrite == nil {
			onConfig := s.opts.OnConfig
			s.opts.OnConfig = nil
			saveErr := s.saveConfig()
			s.opts.OnConfig = onConfig
			if saveErr != nil {
				return nil, fmt.Errorf("save workspace root: %w", saveErr)
			}
		}
	}
	if e := strings.ToLower(discover.UserEmail(ctx)); e != "" {
		s.meAuto = []MeFrom{{Email: e, Global: true, Repos: []string{}}} // until discovery has found the repos' own emails
	}
	// The last scan shows the repos at once; Refresh scans again (seconds on a big workspace) and replaces it.
	if repos, ok := loadScan(s.scanPath); ok {
		s.scanned = repos
		s.repos, _, s.ignoredWith = withoutIgnored(repos, s.cfg.Ignored)
	}
	if ix, err := index.Load(s.indexPath); err == nil {
		s.eng = engine.New(ix)
		s.state = State{Ready: true, Commits: ix.Commits()}
	}
	s.loadDerived()
	s.state.Scanned = len(s.repos)
	s.state.setPhase()
	s.ctx, s.cancel = context.WithCancel(s.git(context.Background(), ""))
	if !opts.NoAutoRefresh {
		s.Go(func() { s.Refresh(s.git(s.ctx, "open")) })
	}
	return s, nil
}

// Go runs f in the background; Close waits for it.
func (s *Session) Go(f func()) {
	s.bg.Add(1)
	go func() { defer s.bg.Done(); f() }()
}

func (s *Session) Close() {
	s.cancel()
	s.derivedMu.Lock()
	s.derivedClosed = true
	if s.derivedTimer != nil {
		s.derivedTimer.Stop()
	}
	s.derivedMu.Unlock()
	s.bg.Wait()
	s.rec.Idle()
	s.saveDerived()
}

// WorkspaceID names a workspace in the data and cache directories (and in window state keys).
func WorkspaceID(root string) string { return workspaceID(root) }

// HasRepos tells whether a folder holds at least one git repo (a workspace worth opening).
func HasRepos(ctx context.Context, root string) (bool, error) {
	repos, err := discover.Scan(ctx, root)
	return len(repos) > 0, err
}

func workspaceID(root string) string {
	h := sha1.Sum([]byte(root))
	return hex.EncodeToString(h[:])[:12]
}

func (s *Session) loadConfig(ctx context.Context) (Config, error) {
	var c Config
	b, err := os.ReadFile(s.cfgPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Config{}, s.backupConfig(fmt.Errorf("read workspace config: %w", err))
		}
	} else if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, s.backupConfig(fmt.Errorf("parse workspace config: %w", err))
	}
	if c.OldMeSet && c.MeManual == nil {
		c.MeManual = c.OldMe
	}
	c.OldMe, c.OldMeSet = nil, false
	if c.Bases == nil {
		c.Bases = map[string]string{}
	}
	return c, nil
}

// backupConfig moves a broken or unreadable config file aside so no later write can overwrite it, and says where. It
// keeps the original contents even under the same name twice (the second move renames to a free name).
func (s *Session) backupConfig(reason error) error {
	stamp := s.opts.Now().Format("20060102-150405")
	for n := 0; ; n++ {
		name := fmt.Sprintf("%s.broken-%s", s.cfgPath, stamp)
		if n > 0 {
			name = fmt.Sprintf("%s-%d", name, n)
		}
		if _, err := os.Lstat(name); err == nil {
			continue // rename would silently replace it: a backup from an earlier break in the same second stays
		}
		switch err := os.Rename(s.cfgPath, name); {
		case err == nil:
			s.mu.Lock()
			s.configBackup = name
			s.mu.Unlock()
			return fmt.Errorf("%s: moved to %s", reason, filepath.Base(name))
		case errors.Is(err, os.ErrNotExist):
			return nil // gone before the move: whatever is there now was saved, not broken
		case errors.Is(err, os.ErrExist):
			continue
		default:
			return fmt.Errorf("%s: move aside: %w", reason, err)
		}
	}
}

// DataDir is the folder of this workspace's own data.
func (s *Session) DataDir() string { return filepath.Dir(s.cfgPath) }

func (s *Session) saveConfig() error {
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(s.cfgPath), 0o755)
	tmp := s.cfgPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.cfgPath); err != nil {
		return err
	}
	// Callers hold s.mu, and the listener reads Info, which takes it.
	if s.opts.OnConfig != nil {
		go s.opts.OnConfig()
	}
	return nil
}

// Refresh rescans the workspace and updates the index; only repos whose refs
// moved are re-read. The updates of calls running together go one after the other.
func (s *Session) Refresh(ctx context.Context) error {
	ctx = s.git(ctx, "refresh")
	seq := s.beginScan()
	// Keep the scan outside the update lock so changing the ignore list can use the last scan.
	var repos []discover.Repo
	var err error
	if s.opts.ReuseScan {
		b, readErr := os.ReadFile(s.scanPath)
		if readErr == nil && json.Unmarshal(b, &repos) == nil {
			repos = slices.DeleteFunc(repos, func(r discover.Repo) bool {
				_, err := os.Stat(r.GitDir)
				return os.IsNotExist(err)
			})
			err = parallel(ctx, len(repos), len(repos), func(i int) {
				repos[i] = discover.Reload(ctx, repos[i])
			})
			return s.applyScan(ctx, seq, repos, err)
		}
	}
	repos, err = discover.Scan(ctx, s.Root)
	return s.applyScan(ctx, seq, repos, err)
}

// beginScan numbers a scan about to start, and counts its refresh as running (State.Indexing).
func (s *Session) beginScan() int {
	var seq int
	s.setState(func(st *State) {
		s.refreshing++
		s.scanSeq++
		seq = s.scanSeq
		st.Indexing, st.Error = true, ""
	})
	return seq
}

// applyScan takes scan number seq and updates the index from it, one update at a time. A scan older than one already
// taken is dropped (the lock is not first come, first served; the newer one saw at least as much), and the worktrees
// UpdateRepos read after the scan began are kept: a checkout the watch saw meanwhile does not go back to an older HEAD.
func (s *Session) applyScan(ctx context.Context, seq int, repos []discover.Repo, err error) error {
	s.refresh.Lock()
	defer s.refresh.Unlock()
	s.mu.Lock()
	take := err == nil && seq > s.scanTaken
	if take {
		s.scanTaken = seq
		s.freshenLocked(repos, seq)
	}
	s.mu.Unlock()
	if take {
		s.mu.Lock()
		var dropped []discover.Repo
		s.scanned = repos
		s.cacheWrite(func() { saveScan(s.scanPath, repos) })
		repos, dropped, s.ignoredWith = withoutIgnored(repos, s.cfg.Ignored)
		s.repos = repos
		s.ignoredDirs = repoDirs(dropped)
		s.mu.Unlock()
		s.setState(func(st *State) { st.Scanned = len(repos) })
		s.syncWatch(repos)
		s.outside.Reset(s.Root)
		s.defaultMe(ctx, repos)
		// Uncommitted counts come from git status, apart from the index: never slow down Info or the update.
		if !s.opts.NoAutoRefresh {
			s.Go(func() { s.RefreshDirty(s.ctx, nil, false) })
		}
		s.mu.Lock()
		var old *index.Index
		if s.eng != nil {
			old = s.eng.Index()
		}
		s.mu.Unlock()
		var ix *index.Index
		var d index.Delta
		ix, d, err = index.Update(ctx, old, repos, func(done, total int) {
			s.setState(func(st *State) { st.Done, st.Total = done, total })
		})
		if err == nil {
			if old == nil || len(d.Changed) > 0 {
				eng := engine.New(ix)
				s.mu.Lock()
				s.eng = eng
				s.indexGen++
				s.mu.Unlock()
				err = s.saveIndex()
				s.indexUpdated(d.Changed)
			}
			failed := make([]RepoFailure, len(d.Failed))
			for i, f := range d.Failed {
				failed[i] = RepoFailure{Repo: f.Repo, Error: f.Err.Error()}
			}
			slices.SortFunc(failed, func(a, b RepoFailure) int { return strings.Compare(a.Repo, b.Repo) })
			s.setState(func(st *State) {
				st.Ready, st.Commits = true, ix.Commits()
				st.Failed = failed
			})
		}
	}
	s.setState(func(st *State) {
		s.refreshing--
		st.Indexing = s.refreshing > 0
		st.Updated = s.opts.Now().Unix()
		if err != nil {
			st.Error = err.Error()
		}
	})
	return err
}

// freshenLocked puts into scan number seq the worktrees UpdateRepos read after it began (s.mu held), and forgets the
// reads it is newer than.
func (s *Session) freshenLocked(repos []discover.Repo, seq int) {
	for i, r := range repos {
		if at, ok := s.reloaded[r.GitDir]; ok && at >= seq {
			if j := slices.IndexFunc(s.scanned, func(x discover.Repo) bool { return x.GitDir == r.GitDir }); j >= 0 {
				repos[i].Worktrees = s.scanned[j].Worktrees
			}
		}
	}
	maps.DeleteFunc(s.reloaded, func(_ string, at int) bool { return at < seq })
}

func (st *State) setPhase() {
	switch {
	case st.Ready && st.Indexing:
		st.Phase = "updating"
	case st.Ready:
		st.Phase = "ready"
	case st.Indexing && st.Total > 0:
		st.Phase = "indexing"
	default:
		st.Phase = "loading"
	}
}

func (s *Session) setState(f func(*State)) {
	s.mu.Lock()
	f(&s.state)
	s.state.setPhase()
	st := s.state
	task, began, ended := s.indexTask, false, false
	switch {
	case st.Indexing && task == nil:
		began = true
	case !st.Indexing && task != nil:
		ended, s.indexTask = true, nil
	}
	if began {
		task = s.newTask(TaskIndex, "", nil, nil)
		s.indexTask = task
	}
	s.mu.Unlock()
	if began {
		s.tasks.notify()
	}
	switch {
	case ended:
		var err error
		if st.Error != "" {
			err = errors.New(st.Error)
		}
		task.end(err, true) // routine updates leave the list when done; a failed one stays to be read
	case task != nil:
		task.progress(st.Done, st.Total)
	}
	if s.opts.OnState != nil {
		s.opts.OnState(st)
	}
}

// saveIndex serializes writes to the shared temporary file and skips versions already saved.
func (s *Session) saveIndex() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	gen, eng := s.indexGen, s.eng
	s.mu.Unlock()
	if eng == nil || gen == s.savedGen {
		return nil
	}
	var err error
	s.cacheWrite(func() {
		err = eng.Index().Save(s.indexPath)
		if err == nil {
			s.savedGen = gen
		}
	})
	return err
}

func (s *Session) cacheWrite(write func()) {
	if s.opts.CacheWrite == nil {
		write()
	} else {
		s.opts.CacheWrite(write)
	}
}

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := Info{Root: s.Root, Name: s.Name, Me: s.meLocked(), State: s.state, Repos: []RepoInfo{}, MeAuto: append([]MeFrom{}, s.meAuto...), MeManual: []string{},
		Ignored: append([]string{}, s.cfg.Ignored...), IgnoredWith: map[string][]string{},
		Release: cmp.Or(s.cfg.Release, DefaultRelease), Master: s.cfg.Master, Requirement: cmp.Or(s.cfg.Requirement, DefaultRequirement), GlabConfig: s.cfg.GlabConfig,
		ConfigBackup: s.configBackup}
	info.MergeFlow = s.mergeFlowLocked()
	trackedBranch := func(string) string { return "" }
	if re, err := compileRequirement(s.cfg.Requirement); err == nil && len(s.cfg.Tracked) > 0 {
		trackedBranch = requirementMatch(re, s.cfg.Tracked...)
	}
	info.Scanned = []string{}
	for k, v := range s.ignoredWith {
		info.IgnoredWith[k] = append([]string{}, v...)
	}
	for _, r := range s.scanned {
		if !tookAlong(s.ignoredWith, r.Name) {
			info.Scanned = append(info.Scanned, r.Name)
		}
	}
	slices.Sort(info.Scanned)
	for _, e := range s.cfg.MeManual {
		if !slices.ContainsFunc(s.meAuto, func(f MeFrom) bool { return f.Email == e }) {
			info.MeManual = append(info.MeManual, e)
		}
	}
	for _, r := range s.repos {
		ri := RepoInfo{Name: r.Name, Kind: string(r.Kind), Base: s.cfg.Bases[r.Name], BaselineOnly: slices.Contains(s.cfg.BaselineOnly, r.Name), Worktrees: []WorktreeInfo{}}
		if ri.Base == "" {
			ri.Base = s.autoBaseLocked(r.Name)
			ri.BaseAuto = ri.Base != ""
		}
		for _, w := range r.Worktrees {
			a, b := s.aheadBehind(r.Name, w.Head)
			ri.Worktrees = append(ri.Worktrees, WorktreeInfo{
				Path: w.Path, Branch: strings.TrimPrefix(w.Branch, "refs/heads/"), Main: w.Main,
				Requirement: trackedBranch(w.Branch),
				Ahead:       a, Behind: b, Dirty: s.dirty.get(w.Path),
			})
		}
		info.Repos = append(info.Repos, ri)
	}
	return info
}

var errNotReady = errors.New("index not ready")

func (s *Session) env() query.Env {
	return query.Env{Me: s.meLocked(), Now: s.opts.Now()}
}

// current returns the engine, env and bases under one lock.
func (s *Session) current() (*engine.Engine, query.Env, engine.Bases) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eng, s.env(), s.basesLocked()
}

// autoBases: the base of a repo without one set is the first of these it has. The team
// merges feature branches into master; origin/HEAD is not used, it can name another branch (develop) and goes stale.
var autoBases = []string{"refs/remotes/origin/master", "refs/remotes/origin/main", "refs/heads/master", "refs/heads/main"}

// autoBaseLocked is repo's derived base, short name (origin/master); "" when it has none of autoBases or before the
// first index. Caller holds s.mu.
func (s *Session) autoBaseLocked(repo string) string {
	if s.eng == nil {
		return ""
	}
	ix := s.eng.Index()
	id, ok := ix.RepoByName(repo)
	if !ok {
		return ""
	}
	return autoBase(ix.Repos[id])
}

func autoBase(r *index.Repo) string {
	for _, ref := range autoBases {
		if _, ok := r.Ref(ref); ok {
			name, _ := strings.CutPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/")
			return name
		}
	}
	return ""
}

// basesLocked: the base of every repo, set or derived. Caller holds s.mu.
func (s *Session) basesLocked() engine.Bases {
	out := maps.Clone(s.cfg.Bases)
	if s.eng == nil {
		return out
	}
	for _, r := range s.eng.Index().Repos {
		if out[r.Name] == "" {
			if b := s.autoBaseLocked(r.Name); b != "" {
				out[r.Name] = b
			}
		}
	}
	return out
}

func (s *Session) Query(c query.Conditions) (QueryResult, error) {
	eng, env, bases := s.current()
	if eng == nil {
		return QueryResult{}, errNotReady
	}
	m, serr := query.Compile(c, env)
	if serr != nil {
		return QueryResult{Error: serr}, nil
	}
	res := eng.Run(m, bases)
	out := QueryResult{Total: len(res.Rows), ByRepo: map[string]int{}, Relax: []query.Suggestion{}}
	for k, n := range res.ByRepo {
		if n > 0 {
			out.ByRepo[eng.Index().Repos[k].Name] = n
		}
	}
	if !c.Regex {
		if e := query.Parse(c.Q); e.HasOps {
			if e.Neg == nil {
				e.Neg = []string{}
			}
			if e.Clauses == nil {
				e.Clauses = [][]string{}
			}
			out.Explain = &e
		}
	}
	if out.Total == 0 {
		out.Relax = query.Relax(c, func(x query.Conditions) int {
			mx, err := query.Compile(x, env)
			if err != nil {
				return 0
			}
			return eng.Count(mx, bases)
		})
	}
	stored := &result{eng: eng, m: m, rows: res.Rows}
	out.ID = s.results.put(stored)
	return out, nil
}

func (s *Session) Rows(id, offset, limit int) ([]Row, error) {
	r := s.results.get(id)
	if r == nil {
		return nil, fmt.Errorf("result %d expired", id)
	}
	if offset < 0 || offset >= len(r.rows) {
		return []Row{}, nil
	}
	page := r.rows[offset:min(len(r.rows), offset+limit)]
	out := make([]Row, len(page))
	ix := r.eng.Index()
	for k, c := range page {
		out[k] = rowOf(ix, c, r.m)
		out[k].Resolved = r.resolved[c]
	}
	return out, nil
}

// rowOf builds the list line of c. m (may be nil) marks the search hit.
func rowOf(ix *index.Index, c index.CommitRef, m *query.Matcher) Row {
	repo := ix.Repos[c.Repo]
	subj := repo.Subject[c.I]
	row := Row{
		Repo: repo.Name, Hash: repo.Oid(c.I).String(), Subject: subj,
		Author: ix.Strs[repo.Author[c.I]], Email: ix.Strs[repo.Email[c.I]], Time: repo.Time[c.I],
		Merge: len(repo.Parents(c.I)) > 1, Refs: refViews(repo.RefsAt(c.I)), Hit: []int{},
	}
	if m != nil {
		if a, b, ok := m.Hit(subj); ok {
			u0, u1 := engine.UTF16Range(subj, a, b)
			row.Hit = []int{u0, u1}
		}
	}
	return row
}

func refViews(refs []index.Ref) []RefView {
	out := []RefView{}
	for _, r := range refs {
		switch {
		case strings.HasPrefix(r.Name, "refs/heads/"):
			out = append(out, RefView{strings.TrimPrefix(r.Name, "refs/heads/"), "local"})
		case strings.HasPrefix(r.Name, "refs/remotes/"):
			out = append(out, RefView{strings.TrimPrefix(r.Name, "refs/remotes/"), "remote"})
		case strings.HasPrefix(r.Name, "refs/tags/"):
			out = append(out, RefView{strings.TrimPrefix(r.Name, "refs/tags/"), "tag"})
		}
	}
	return out
}

func (s *Session) find(k CommitKey) (*engine.Engine, index.CommitRef, error) {
	s.mu.Lock()
	eng := s.eng
	s.mu.Unlock()
	if eng == nil {
		return nil, index.CommitRef{}, errNotReady
	}
	c, err := findIn(eng.Index(), k)
	return eng, c, err
}

func findIn(ix *index.Index, k CommitKey) (index.CommitRef, error) {
	id, ok := ix.RepoByName(k.Repo)
	if !ok {
		return index.CommitRef{}, fmt.Errorf("unknown repo %q", k.Repo)
	}
	i, err := ix.Repos[id].FindHash(k.Hash)
	if err != nil {
		return index.CommitRef{}, err
	}
	if i < 0 {
		return index.CommitRef{}, fmt.Errorf("commit %s not in %s", k.Hash, k.Repo)
	}
	return index.CommitRef{Repo: id, I: i}, nil
}

func (s *Session) Commit(ctx context.Context, k CommitKey) (CommitDetail, error) {
	ctx = s.git(ctx, "commit")
	eng, c, err := s.find(k)
	if err != nil {
		return CommitDetail{}, err
	}
	ix := eng.Index()
	r := ix.Repos[c.Repo]
	subj, body, _ := gitview.Message(ctx, r, c.I)
	d := CommitDetail{
		Repo: r.Name, Hash: k.Hash, Subject: subj, Body: body,
		Author: ix.Strs[r.Author[c.I]], Email: ix.Strs[r.Email[c.I]], Time: r.Time[c.I],
		Refs: refViews(r.RefsAt(c.I)), Parents: []string{},
	}
	for _, p := range r.Parents(c.I) {
		d.Parents = append(d.Parents, r.Oid(p).String())
	}
	return d, nil
}

// Files lists changed files: of one commit, or the union of several.
func (s *Session) Files(ctx context.Context, keys []CommitKey) ([]FileChange, error) {
	ctx = s.git(ctx, "files")
	ctx, done := s.latest.start(ctx, "files")
	defer done()
	var refs []index.CommitRef
	var eng *engine.Engine
	for _, k := range keys {
		e, c, err := s.find(k)
		if err != nil {
			return nil, err
		}
		eng = e
		refs = append(refs, c)
	}
	if len(refs) == 0 {
		return []FileChange{}, nil
	}
	files, err := gitview.UnionFiles(ctx, eng.Index(), refs)
	if files == nil {
		files = []FileChange{}
	}
	return files, err
}

// patchOf converts a patch; the native diff colours code itself, by the file's language.
func patchOf(p gitcli.Patch, file string) Patch {
	out := Patch{Binary: p.Binary, Hunks: make([]Hunk, len(p.Hunks))}
	for i, h := range p.Hunks {
		out.Hunks[i] = Hunk{Header: h.Header, Lines: make([]Line, len(h.Lines))}
		for j, l := range h.Lines {
			out.Hunks[i].Lines[j] = Line{Kind: string(l.Kind), Old: l.Old, New: l.New, Text: l.Text}
		}
	}
	return out
}

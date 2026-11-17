package gitcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Kind is what a git command does.
type Kind string

const (
	KindRead   Kind = "read"   // reads commits and refs
	KindDiff   Kind = "diff"   // computes a diff or a file's content
	KindStatus Kind = "status" // looks at working directories
	KindWrite  Kind = "write"  // changes a repository
)

// subcommands is every git subcommand Sheaf runs and its kind; anything else is refused (Cmd fails it). A write runs only through Write and a read only through the
// other functions (TestWriteWhitelist), so every change Sheaf makes to a repository is one of the KindWrite entries here.
var subcommands = map[string]Kind{
	"cat-file":     KindRead,
	"config":       KindRead,
	"diff":         KindDiff,
	"diff-tree":    KindDiff,
	"for-each-ref": KindRead,
	"log":          KindRead,
	"patch-id":     KindDiff, // reads a patch from stdin
	"rev-list":     KindRead,
	"rev-parse":    KindRead,
	"show":         KindDiff,
	"status":       KindStatus,
	"version":      KindRead,
	"worktree":     KindStatus, // list only (forms)
	// Writes (write.go), run only through Write.
	"fetch": KindWrite,
	"merge": KindWrite, // only --ff-only (mergeFFOnly)
	"push":  KindWrite, // only the plain form (pushPlain)
	// Only for one uncommitted file the user chose to discard: restoreFile, cleanFile.
	"restore": KindWrite,
	"clean":   KindWrite,
}

// forms are the subcommands whose kind depends on their first argument; any other form of them is refused.
var forms = map[string]map[string]Kind{
	"worktree": {"list": KindStatus},
}

// KindOf is the kind of a command line (after the flags every command gets); "" when it is not one Sheaf runs.
func KindOf(args []string) Kind {
	if len(args) == 0 {
		return ""
	}
	if f, ok := forms[args[0]]; ok {
		if len(args) < 2 {
			return ""
		}
		return f[args[1]]
	}
	if args[0] == "cat-file" && (len(args) != 2 || args[1] != "--batch") {
		return ""
	}
	if args[0] == "config" && !configRead(args[1:]) {
		return ""
	}
	if args[0] == "merge" && !mergeFFOnly(args[1:]) {
		return ""
	}
	if args[0] == "push" && !pushPlain(args[1:]) {
		return ""
	}
	if args[0] == "restore" && !restoreFile(args[1:]) {
		return ""
	}
	if args[0] == "clean" && !cleanFile(args[1:]) {
		return ""
	}
	return subcommands[args[0]]
}

// mergeFFOnly allows exactly the fast-forward form used by branch sync, so git never starts a conflicted merge.
func mergeFFOnly(args []string) bool {
	return len(args) == 3 && args[0] == "--ff-only" && args[1] == "--end-of-options" && args[2] != ""
}

// pushPlain: push only sends one branch to a remote, as a fast-forward (push --set-upstream [--end-of-options] <remote>
// <ref>:<ref>). No --force / --force-with-lease / -f, no "+" refspec (also a force), no ":<ref>" or --delete (deletes a remote
// branch), no --all / --mirror / --tags: any other flag is refused.
func pushPlain(args []string) bool {
	var pos []string
	end := false
	for _, a := range args {
		switch {
		case end:
			pos = append(pos, a)
		case a == "--end-of-options":
			end = true
		case a == "--set-upstream":
		case strings.HasPrefix(a, "-"):
			return false
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) != 2 {
		return false
	}
	src, dst, ok := strings.Cut(pos[1], ":")
	return ok && src != "" && dst != "" && !strings.HasPrefix(src, "+") && !strings.Contains(dst, ":")
}

// restoreFile allows only putting one file (and the old path of its rename) back to HEAD in the index and the working
// directory, by literal path: no other source, no wildcard, no directory such as ".".
func restoreFile(args []string) bool {
	head := []string{"--source=HEAD", "--staged", "--worktree", "--"}
	if len(args) < len(head)+1 || len(args) > len(head)+2 || !slices.Equal(args[:len(head)], head) {
		return false
	}
	return !slices.ContainsFunc(args[len(head):], func(p string) bool { return !literalFile(p) })
}

// cleanFile allows only deleting one untracked file by literal path: no -d (directories), no -x / -X (ignored files).
func cleanFile(args []string) bool {
	return len(args) == 3 && args[0] == "-f" && args[1] == "--" && literalFile(args[2])
}

// literalFile is a pathspec of one file below the working directory: :(literal) magic, so nothing in it is a wildcard, and
// no path that names the whole tree or leaves it.
func literalFile(spec string) bool {
	p, ok := strings.CutPrefix(spec, literalMagic)
	if !ok || p == "" || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") || strings.ContainsRune(p, 0) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

const literalMagic = ":(literal)"

// configRead: config only gets a value (config [--global] <key>); a second positional sets it, and other flags edit the file.
func configRead(args []string) bool {
	n := 0
	for _, a := range args {
		switch {
		case !strings.HasPrefix(a, "-"):
			n++
		case !slices.Contains([]string{"--get", "--get-all", "--global", "--local", "--null", "-z"}, a):
			return false
		}
	}
	return n <= 1
}

// Results of an Event.
const (
	Running   = "running"
	Succeeded = "ok"
	Failed    = "fail"
	Cancelled = "cancel"
)

// Event is one git invocation.
type Event struct {
	ID         uint64        `json:"id"`
	Tool       string        `json:"tool,omitempty"` // ToolGit (git), ToolGlab or ToolGh
	Start      time.Time     `json:"start"`
	Dur        time.Duration `json:"dur"`
	Source     string        `json:"source"`               // what asked for it, a key (WithSource)
	SourceText string        `json:"sourceText,omitempty"` // Source in words (Recorder.Describe)
	Dir        string        `json:"dir"`
	Args       []string      `json:"args"` // after the flags every command gets (Cmd)
	StdinLines int           `json:"stdinLines,omitempty"`
	Kind       Kind          `json:"kind"`
	Result     string        `json:"result"`
	Exit       int           `json:"exit"`                 // exit code; 0 for success, -1 when the program did not start
	Out        int64         `json:"out"`                  // bytes on stdout
	Stdout     string        `json:"stdout,omitempty"`     // the first maxCapture bytes of stdout
	Stderr     string        `json:"stderr,omitempty"`     // the first maxCapture bytes of stderr
	Workspace  string        `json:"workspace,omitempty"`  // the workspace root the command ran for (Recorder.Workspace)
	GlabConfig bool          `json:"glabConfig,omitempty"` // a glab run with the workspace's own GLAB_CONFIG_DIR (the path is not kept)
	Slot       int           `json:"slot"`                 // lowest slot free when it started
}

const (
	keepReads  = 2000                // read commands kept in memory
	keepWrites = 30 * 24 * time.Hour // write commands kept in WriteLog
	maxCapture = 4 << 10             // stdout and stderr kept per command
)

// Recorder keeps the git commands run with it on the context (WithRecorder). Reads stay in memory (the last 2,000); writes are an
// audit trail, appended to WriteLog (a file the user can open) and kept 30 days. The zero value records reads only.
type Recorder struct {
	WriteLog  string                     // JSON lines; "" = writes are kept in memory only
	Workspace string                     // written into every Event
	OnChange  func()                     // called (outside the lock) when a command starts or ends
	Describe  func(source string) string // Event.SourceText: the source in words; nil = none
	Now       func() time.Time

	mu      sync.Mutex
	reads   []*Event // ring: next is where the next one goes once it is full
	next    int
	writes  []*Event
	loaded  bool
	slots   []bool
	lastID  uint64
	running int
	idle    sync.Cond // signalled when running drops to 0; L is mu, set on first use
}

func (r *Recorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Recorder) begin(ctx context.Context, dir string, args []string, stdin string) *Event {
	e := &Event{Dir: dir, Args: args, Kind: KindOf(args)}
	if stdin != "" {
		e.StdinLines = strings.Count(stdin, "\n")
	}
	return r.add(ctx, e)
}

// add records e (its Tool, Dir, Args, Kind and StdinLines set) as started now.
func (r *Recorder) add(ctx context.Context, e *Event) *Event {
	e.Start, e.Source, e.Result, e.Workspace = r.now(), SourceOf(ctx), Running, r.Workspace
	if r.Describe != nil {
		e.SourceText = r.Describe(e.Source)
	}
	r.mu.Lock()
	r.lastID++
	e.ID = r.lastID
	e.Slot = slices.Index(r.slots, false)
	if e.Slot < 0 {
		e.Slot = len(r.slots)
		r.slots = append(r.slots, true)
	} else {
		r.slots[e.Slot] = true
	}
	r.running++
	if e.Kind == KindWrite {
		r.load()
		r.writes = append(r.writes, e)
	} else if len(r.reads) < keepReads {
		r.reads = append(r.reads, e)
	} else {
		r.reads[r.next] = e
		r.next = (r.next + 1) % keepReads
	}
	r.mu.Unlock()
	r.changed()
	return e
}

func (r *Recorder) end(ctx context.Context, e *Event, out int64, stdout, stderr string, err error) {
	r.mu.Lock()
	e.Dur = r.now().Sub(e.Start)
	e.Out = out
	e.Stdout, e.Stderr = head(stdout), head(stderr)
	var exit *exec.ExitError
	switch {
	case err == nil:
		e.Result = Succeeded
	case ctx.Err() != nil:
		e.Result = Cancelled
	case errors.As(err, &exit):
		e.Result, e.Exit = Failed, exit.ExitCode()
	default:
		e.Result, e.Exit = Failed, -1
		if e.Stderr == "" {
			e.Stderr = err.Error()
		}
	}
	r.slots[e.Slot] = false
	r.running--
	if r.running == 0 {
		r.idle.Broadcast()
	}
	var line []byte
	if e.Kind == KindWrite && r.WriteLog != "" {
		line, _ = json.Marshal(e)
	}
	r.mu.Unlock()
	if line != nil {
		r.appendWrite(line)
	}
	r.changed()
}

// Idle waits until no command run with r is running, so its owner can close without git still writing in the repos
// (git show --remerge-diff writes temporary objects even for a read).
func (r *Recorder) Idle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idle.L == nil {
		r.idle.L = &r.mu
	}
	for r.running > 0 {
		r.idle.Wait()
	}
}

// head is the first maxCapture bytes of s, trimmed, without a cut-off character at the end.
func head(s string) string {
	if len(s) > maxCapture {
		s = s[:maxCapture]
	}
	return strings.TrimSpace(strings.ToValidUTF8(s, ""))
}

func (r *Recorder) changed() {
	if r.OnChange != nil {
		r.OnChange()
	}
}

// load reads the writes of the last 30 days from WriteLog, once, and drops older ones from the file. Called with mu held.
func (r *Recorder) load() {
	if r.loaded || r.WriteLog == "" {
		r.loaded = true
		return
	}
	r.loaded = true
	f, err := os.Open(r.WriteLog)
	if err != nil {
		return
	}
	defer f.Close()
	cut := r.now().Add(-keepWrites)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	old := false
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Start.Before(cut) {
			old = true
			continue
		}
		r.writes = append(r.writes, &e)
		r.lastID = max(r.lastID, e.ID)
	}
	if old {
		r.rewrite()
	}
}

func (r *Recorder) rewrite() {
	var b strings.Builder
	for _, e := range r.writes {
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	tmp := r.WriteLog + ".tmp"
	if os.WriteFile(tmp, []byte(b.String()), 0o644) == nil {
		os.Rename(tmp, r.WriteLog)
	}
}

func (r *Recorder) appendWrite(line []byte) {
	if err := os.MkdirAll(filepath.Dir(r.WriteLog), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(r.WriteLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

// Events is a copy of what is kept: the running ones and the reads in memory, and the writes of the last 30 days; newest first.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	out := make([]Event, 0, len(r.reads)+len(r.writes))
	for _, e := range r.reads {
		out = append(out, *e)
	}
	for _, e := range r.writes {
		out = append(out, *e)
	}
	slices.SortFunc(out, func(a, b Event) int { return b.Start.Compare(a.Start) })
	return out
}

// Running is how many commands are running now.
func (r *Recorder) Running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

type recorderKey struct{}
type sourceKey struct{}

// WithRecorder makes the commands run with the returned context be recorded in r.
func WithRecorder(ctx context.Context, r *Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

// WithSource names what the commands run with the returned context are for, in one phrase; an outer source wins, so a command
// is told by what the user (or the watch) did, not by the helper that ran it.
func WithSource(ctx context.Context, source string) context.Context {
	if SourceOf(ctx) != "" || source == "" {
		return ctx
	}
	return context.WithValue(ctx, sourceKey{}, source)
}

func SourceOf(ctx context.Context) string {
	s, _ := ctx.Value(sourceKey{}).(string)
	return s
}

func recorderOf(ctx context.Context) *Recorder {
	r, _ := ctx.Value(recorderKey{}).(*Recorder)
	return r
}

// LastID is the id of the newest command.
func (r *Recorder) LastID() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastID
}

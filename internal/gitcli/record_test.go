package gitcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sheaf/internal/testutil"
)

// Every command that runs with a Recorder on its context is recorded once, with its kind, result and output size.
func TestRecord(t *testing.T) {
	testutil.Isolate(t)
	r := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	head := r.Commit("init", map[string]string{"a.txt": "a\n"})
	oid, _ := ParseOid(head)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name   string
		ctx    context.Context
		run    func(ctx context.Context) error
		kind   Kind
		result string
		exit   int
		out    bool // stdout not empty
		stderr string
	}{
		{"read", context.Background(), func(ctx context.Context) error { _, err := Refs(ctx, r.Dir); return err }, KindRead, Succeeded, 0, true, ""},
		{"status", context.Background(), func(ctx context.Context) error { _, err := Status(ctx, r.Dir); return err }, KindStatus, Succeeded, 0, false, ""},
		{"stream", context.Background(), func(ctx context.Context) error {
			return Log(ctx, r.Dir, []Oid{oid}, nil, func(*RawCommit) error { return nil })
		}, KindRead, Succeeded, 0, true, ""},
		{"failed", context.Background(), func(ctx context.Context) error { _, err := Output(ctx, r.Dir, "show", "nope"); return err }, KindDiff, Failed, 128, false, "nope"},
		{"cancelled", cancelled, func(ctx context.Context) error { _, err := Output(ctx, r.Dir, "status"); return err }, KindStatus, Cancelled, 0, false, ""},
		{"not whitelisted", context.Background(), func(ctx context.Context) error { _, err := Output(ctx, r.Dir, "gc"); return err }, "", Failed, -1, false, "not a command Sheaf runs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rec Recorder
			changes := 0
			rec.OnChange = func() { changes++ }
			ctx := WithSource(WithRecorder(tc.ctx, &rec), "test · "+tc.name)
			err := tc.run(ctx)
			if (err != nil) != (tc.result != Succeeded) {
				t.Fatalf("err %v", err)
			}
			ev := rec.Events()
			if len(ev) != 1 {
				t.Fatalf("%d events", len(ev))
			}
			e := ev[0]
			if e.Kind != tc.kind || e.Result != tc.result || e.Exit != tc.exit || (e.Out > 0) != tc.out || !strings.Contains(e.Stderr, tc.stderr) {
				t.Errorf("event %+v", e)
			}
			if e.Source != "test · "+tc.name || e.Dir != r.Dir || e.ID != 1 || e.Slot != 0 || rec.Running() != 0 || changes != 2 {
				t.Errorf("event %+v, running %d, changes %d", e, rec.Running(), changes)
			}
		})
	}
	// No recorder, nothing recorded and nothing breaks.
	if _, err := Refs(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
}

func TestWithSourceOuterWins(t *testing.T) {
	ctx := WithSource(WithSource(context.Background(), "watch"), "index")
	if got := SourceOf(ctx); got != "watch" {
		t.Errorf("source %q", got)
	}
	if got := SourceOf(WithSource(context.Background(), "")); got != "" {
		t.Errorf("empty source %q", got)
	}
}

// Reads keep the last 2,000; concurrent commands take the lowest free slot.
func TestRecorderRingAndSlots(t *testing.T) {
	var rec Recorder
	ctx := context.Background()
	a := rec.begin(ctx, "/r", []string{"log"}, "x\ny\n")
	b := rec.begin(ctx, "/r", []string{"log"}, "")
	rec.end(ctx, a, 10, "", "", nil)
	c := rec.begin(ctx, "/r", []string{"log"}, "")
	if a.Slot != 0 || b.Slot != 1 || c.Slot != 0 || a.StdinLines != 2 || rec.Running() != 2 {
		t.Errorf("slots %d %d %d, stdin %d, running %d", a.Slot, b.Slot, c.Slot, a.StdinLines, rec.Running())
	}
	rec.end(ctx, b, 0, "", "", nil)
	rec.end(ctx, c, 0, "", "", nil)
	for range keepReads + 5 {
		rec.end(ctx, rec.begin(ctx, "/r", []string{"status"}, ""), 0, "", "", nil)
	}
	ev := rec.Events()
	if len(ev) != keepReads || ev[0].ID != keepReads+8 || ev[len(ev)-1].ID != 9 {
		t.Errorf("kept %d, newest %d, oldest %d", len(ev), ev[0].ID, ev[len(ev)-1].ID)
	}
}

// Writes are an audit trail: appended to WriteLog, read back by a new Recorder, dropped after 30 days.
func TestRecorderWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "commands.jsonl")
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	ctx := context.Background()

	rec := &Recorder{WriteLog: path, Now: clock}
	old := rec.begin(ctx, "/r", []string{"fetch", "origin"}, "")
	rec.end(ctx, old, 5, "", "", nil)
	now = now.Add(29 * 24 * time.Hour)
	recent := rec.begin(ctx, "/r", []string{"fetch", "--prune", "origin"}, "")
	rec.end(ctx, recent, 7, "", "boom", errors.New("exit"))
	for range keepReads + 1 { // reads pushing past the ring do not push writes out
		rec.end(ctx, rec.begin(ctx, "/r", []string{"status"}, ""), 0, "", "", nil)
	}
	if n := countKind(rec.Events(), KindWrite); n != 2 {
		t.Errorf("writes in memory %d", n)
	}

	now = now.Add(2 * 24 * time.Hour) // the first write is 31 days old now
	again := &Recorder{WriteLog: path, Now: clock}
	ev := again.Events()
	if len(ev) != 1 || ev[0].ID != recent.ID || ev[0].Result != Failed || ev[0].Stderr != "boom" || ev[0].Out != 7 {
		t.Fatalf("reloaded %+v", ev)
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "\n") != 1 {
		t.Errorf("old write not dropped from the file:\n%s", raw)
	}
	next := again.begin(ctx, "/r", []string{"status"}, "")
	if next.ID <= recent.ID {
		t.Errorf("ids restart: %d", next.ID)
	}
}

func countKind(ev []Event, k Kind) int {
	n := 0
	for _, e := range ev {
		if e.Kind == k {
			n++
		}
	}
	return n
}

// Recording one command costs about 0.1 µs and 2 allocations; starting git costs milliseconds.
func BenchmarkRecord(b *testing.B) {
	var rec Recorder
	ctx := WithSource(context.Background(), "bench")
	args := []string{"status", "--porcelain=v2", "-z"}
	for b.Loop() {
		rec.end(ctx, rec.begin(ctx, "/r", args, ""), 100, "", "", nil)
	}
}

// mutating are git command lines that change a repository. Sheaf may run one only as a whitelisted write: through Write, recorded
// as KindWrite (an audit record kept 30 days),.
var mutating = [][]string{
	{"add", "x"}, {"am"}, {"apply"}, {"branch", "x"}, {"checkout", "x"}, {"cherry-pick", "x"}, {"clean", "-fd"}, {"clone", "u"},
	{"commit", "-m", "x"}, {"commit-tree", "t"}, {"config", "user.email", "x"}, {"config", "--unset", "user.email"}, {"config", "-e"},
	{"fetch"}, {"gc"}, {"hash-object", "-w", "x"}, {"init"}, {"merge", "x"}, {"merge", "--ff-only", "--end-of-options", "refs/remotes/origin/main"}, {"merge-tree", "--write-tree", "a", "b"}, {"mv", "a", "b"},
	{"notes", "add"}, {"prune"}, {"pull"}, {"push"}, {"read-tree", "x"}, {"rebase", "x"}, {"reflog", "expire"}, {"repack"},
	{"reset", "--hard"}, {"restore", "x"}, {"revert", "x"}, {"rm", "x"}, {"stash"}, {"submodule", "update"}, {"switch", "x"}, {"switch", "-c", "x"}, {"switch", "-C", "x"}, {"switch", "--discard-changes", "x"}, {"switch", "-f", "x"},
	{"switch", "--detach", "x"}, {"switch", "--end-of-options", "-c", "x"}, {"switch"}, {"switch", "a", "b"},
	{"symbolic-ref", "HEAD", "x"}, {"tag", "x"}, {"update-index", "--refresh"}, {"update-ref", "r", "o"}, {"worktree", "add", "p"},
	{"worktree", "remove", "p"}, {"worktree", "prune"}, {"worktree", "move", "a", "b"}, {"write-tree"},
}

func TestWriteWhitelist(t *testing.T) {
	ctx := context.Background()
	for _, args := range mutating {
		switch k := KindOf(args); k {
		case "":
			if Cmd(ctx, "/r", args...).Err == nil {
				t.Errorf("%v: not whitelisted but Cmd lets it run", args)
			}
		case KindWrite:
			if Cmd(ctx, "/r", args...).Err == nil {
				t.Errorf("%v: a write runs through Cmd / Output", args)
			}
			if c := cmd(ctx, "/r", true, args); c.Err != nil {
				t.Errorf("%v: Write refuses it: %v", args, c.Err)
			}
		default:
			t.Errorf("%v changes a repository but is whitelisted as %s", args, k)
		}
	}
	for _, args := range [][]string{
		{"merge", "x"}, {"merge", "--abort"}, {"merge", "--continue"},
		{"merge", "--no-edit", "-m", "message", "--end-of-options", "x"},
		{"merge", "--ff-only", "--no-ff", "x"}, {"merge", "--ff-only", "--squash", "x"},
		{"merge", "--ff-only", "--end-of-options", ""}, {"merge", "--ff-only", "--end-of-options", "a", "b"},
		{"merge-tree", "--write-tree", "a", "b"}, {"commit-tree", "t"},
		{"update-ref", "r", "o"}, {"worktree", "add", "p", "branch"},
		{"switch"}, {"switch", "main"}, {"switch", "--end-of-options", "main"}, {"switch", "--end-of-options", "-odd"},
		{"switch", "-c", "x"}, {"switch", "--detach", "x"}, {"switch", "--discard-changes", "x"},
		{"restore", "x"}, {"restore", "--", ":(literal)a"}, {"restore", "--source=HEAD", "--worktree", "--", ":(literal)a"},
		{"restore", "--source=main", "--staged", "--worktree", "--", ":(literal)a"},
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", "a"},
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", ":(literal)."},
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", ":(literal)"},
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", ":(literal)../a"},
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", ":(literal)src/"},
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", ":(literal)/etc/a"},
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", ":(literal)a", ":(literal)b", ":(literal)c"},
		{"clean", "-fd"}, {"clean", "-f"}, {"clean", "-f", "--", "a"}, {"clean", "-fdx", "--", ":(literal)a"},
		{"clean", "-f", "-x", "--", ":(literal)a"}, {"clean", "-f", "--", ":(literal)."}, {"clean", "-f", "--", ":(literal)a", ":(literal)b"},
	} {
		if KindOf(args) != "" || cmd(ctx, "/r", true, args).Err == nil {
			t.Errorf("removed write still allowed: %v", args)
		}
	}
	for _, args := range [][]string{
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", ":(literal)a/b.go"},
		{"restore", "--source=HEAD", "--staged", "--worktree", "--", ":(literal)new.go", ":(literal)old.go"},
		{"clean", "-f", "--", ":(literal)a/new.txt"},
	} {
		if KindOf(args) != KindWrite || cmd(ctx, "/r", true, args).Err != nil {
			t.Errorf("discarding one file is refused: %v", args)
		}
	}
	for sub, k := range subcommands {
		if k != KindWrite && cmd(ctx, "/r", true, []string{sub}).Err == nil {
			t.Errorf("Write runs the read %s", sub)
		}
	}
	if _, err := Write(ctx, t.TempDir(), "status"); err == nil || !strings.Contains(err.Error(), "only through") {
		t.Errorf("Write ran a read: %v", err)
	}
}

func TestRecorderIdleWaitsForRunningCommands(t *testing.T) {
	var rec Recorder
	ctx := WithRecorder(t.Context(), &rec)
	rec.Idle()
	a := rec.begin(ctx, "/r", []string{"show"}, "")
	b := rec.begin(ctx, "/r", []string{"show"}, "")
	idle := make(chan struct{})
	go func() { rec.Idle(); close(idle) }()
	rec.end(ctx, a, 0, "", "", nil)
	select {
	case <-idle:
		t.Fatal("Idle returned while a command was still running")
	case <-time.After(50 * time.Millisecond):
	}
	rec.end(ctx, b, 0, "", "", nil)
	select {
	case <-idle:
	case <-time.After(5 * time.Second):
		t.Fatal("Idle did not return after the last command ended")
	}
}

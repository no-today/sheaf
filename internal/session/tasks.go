package session

import (
	"slices"
	"sync"
	"time"
)

// Kinds of task: the long actions the user starts or waits on. Background reads (diffs, line counts, uncommitted files)
// are not tasks: they come and go with what is on screen.
const (
	TaskIndex   = "index"   // the index being built or brought up to date
	TaskFetch   = "fetch"   // git fetch in the requirement's repos
	TaskSync    = "sync"    // fast-forwarding branches to their remote branches
	TaskMR      = "mr"      // creating merge or pull requests
	TaskDiscard = "discard" // discarding a file's uncommitted changes
)

// States of a task and of its rows.
const (
	TaskWaiting = "waiting" // a row not begun
	TaskRunning = "running"
	TaskDone    = "done"
	TaskFailed  = "failed"
	TaskSkipped = "skipped" // a row that had nothing to do, or that the user stopped before it began
	TaskStopped = "stopped" // a task the user stopped
)

// keptTasks bounds the finished tasks kept for the task list.
const keptTasks = 20

// Task is one long action as the task list shows it.
type Task struct {
	ID      int
	Kind    string // Task*
	Subject string // what it acts on: a requirement, a file; "" when the kind says it all
	State   string // TaskRunning, TaskDone, TaskFailed or TaskStopped
	Rows    []TaskRow
	Done    int // rows (or, for the index, repos) finished
	Total   int
	Started time.Time
	Ended   time.Time // zero while running
	Error   string    // why the whole task failed
	CanStop bool      // StopTask applies: running, and the kind can be stopped
}

// TaskRow is one repo (or branch, or file) of a task.
type TaskRow struct {
	Name  string
	State string // TaskWaiting, TaskRunning, TaskDone, TaskFailed or TaskSkipped
	Note  string // the outcome or error, in the session's words (an outcome code, an error message)
	URL   string // the merge or pull request a row created or found
}

// tasks keeps the running and the recently finished tasks.
type tasks struct {
	mu      sync.Mutex
	seq     int
	list    []*taskRun // oldest first
	changed func()
}

// taskRun is a task while it runs: rows report through it and StopTask reaches it.
type taskRun struct {
	ts   *tasks
	now  func() time.Time
	t    Task
	stop func() // nil when the kind cannot be stopped
	off  bool   // StopTask was called
}

// Tasks are the running tasks, oldest first, then the finished ones, newest first.
func (s *Session) Tasks() []Task {
	ts := &s.tasks
	ts.mu.Lock()
	defer ts.mu.Unlock()
	var running, ended []Task
	for _, r := range ts.list {
		t := r.t
		t.Rows = slices.Clone(t.Rows)
		t.CanStop = r.stop != nil && !r.off && t.State == TaskRunning
		if t.State == TaskRunning {
			running = append(running, t)
		} else {
			ended = append(ended, t)
		}
	}
	slices.Reverse(ended)
	return append(running, ended...)
}

// StopTask stops task id if it can be stopped: a fetch at once, a batch of writes before its next row begins, so no row
// is left half done.
func (s *Session) StopTask(id int) {
	ts := &s.tasks
	ts.mu.Lock()
	var stop func()
	for _, r := range ts.list {
		if r.t.ID == id && r.stop != nil && !r.off && r.t.State == TaskRunning {
			r.off, stop = true, r.stop
		}
	}
	ts.mu.Unlock()
	if stop != nil {
		stop()
		ts.notify()
	}
}

// ClearTasks drops the finished tasks.
func (s *Session) ClearTasks() {
	ts := &s.tasks
	ts.mu.Lock()
	ts.list = slices.DeleteFunc(ts.list, func(r *taskRun) bool { return r.t.State != TaskRunning })
	ts.mu.Unlock()
	ts.notify()
}

func (ts *tasks) notify() {
	if ts.changed != nil {
		ts.changed()
	}
}

// startTask registers a running task with a row per name. stop, if not nil, makes it stoppable.
func (s *Session) startTask(kind, subject string, rows []string, stop func()) *taskRun {
	r := s.newTask(kind, subject, rows, stop)
	s.tasks.notify()
	return r
}

// newTask is startTask without telling anyone, for callers holding s.mu: they call s.tasks.notify once they let go.
func (s *Session) newTask(kind, subject string, rows []string, stop func()) *taskRun {
	ts := &s.tasks
	ts.mu.Lock()
	ts.seq++
	r := &taskRun{ts: ts, now: s.opts.Now, stop: stop, t: Task{ID: ts.seq, Kind: kind, Subject: subject, State: TaskRunning,
		Total: len(rows), Started: s.opts.Now()}}
	for _, n := range rows {
		r.t.Rows = append(r.t.Rows, TaskRow{Name: n, State: TaskWaiting})
	}
	ts.list = append(ts.list, r)
	ts.mu.Unlock()
	return r
}

// stopped says whether the user stopped the task.
func (r *taskRun) stopped() bool {
	r.ts.mu.Lock()
	defer r.ts.mu.Unlock()
	return r.off
}

// row sets row i's state and note; a row leaving waiting or running counts as done.
func (r *taskRun) row(i int, state, note string) { r.rowURL(i, state, note, "") }

// rowURL is row with the merge or pull request the row led to.
func (r *taskRun) rowURL(i int, state, note, url string) {
	r.ts.mu.Lock()
	if i >= 0 && i < len(r.t.Rows) {
		was := r.t.Rows[i].State
		r.t.Rows[i] = TaskRow{Name: r.t.Rows[i].Name, State: state, Note: note, URL: url}
		if finished(state) && !finished(was) {
			r.t.Done++
		}
	}
	r.ts.mu.Unlock()
	r.ts.notify()
}

// progress sets the done and total counts of a task without rows (the index).
func (r *taskRun) progress(done, total int) {
	r.ts.mu.Lock()
	changed := r.t.Done != done || r.t.Total != total
	r.t.Done, r.t.Total = done, total
	r.ts.mu.Unlock()
	if changed {
		r.ts.notify()
	}
}

// begin marks row i running as a gitops batch starts it; false, once the user stopped the task, stops the batch there.
func (r *taskRun) begin(i int) bool {
	if r.stopped() {
		return false
	}
	r.row(i, TaskRunning, "")
	return true
}

// end finishes the task: failed with err, stopped if the user stopped it, else done. drop removes it from the list
// instead (a routine index update the user need not see once over).
func (r *taskRun) end(err error, drop bool) {
	ts := r.ts
	ts.mu.Lock()
	r.t.Ended = r.now()
	switch {
	case err != nil:
		r.t.State, r.t.Error = TaskFailed, err.Error()
	case r.off:
		r.t.State = TaskStopped
	default:
		r.t.State = TaskDone
	}
	for i := range r.t.Rows { // rows a failure left unreported
		if !finished(r.t.Rows[i].State) {
			r.t.Rows[i].State = TaskSkipped
		}
	}
	if drop && err == nil {
		ts.list = slices.DeleteFunc(ts.list, func(x *taskRun) bool { return x == r })
	}
	ended := 0
	for _, x := range ts.list {
		if x.t.State != TaskRunning {
			ended++
		}
	}
	for i := 0; ended > keptTasks && i < len(ts.list); {
		if ts.list[i].t.State != TaskRunning {
			ts.list = slices.Delete(ts.list, i, i+1)
			ended--
			continue
		}
		i++
	}
	ts.mu.Unlock()
	ts.notify()
}

func finished(state string) bool { return state != TaskWaiting && state != TaskRunning }

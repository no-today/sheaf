package session

import (
	"errors"
	"testing"
	"time"
)

func TestTasks(t *testing.T) {
	changes := 0
	s := &Session{opts: Options{Now: time.Now}}
	s.tasks.changed = func() { changes++ }

	stops := 0
	run := s.startTask(TaskSync, "YT-1", []string{"a · x", "b · x", "c · x"}, func() { stops++ })
	if !run.begin(0) {
		t.Fatal("a fresh task refused its first row")
	}
	run.row(0, TaskDone, "updated")
	ts := s.Tasks()
	if len(ts) != 1 || ts[0].Done != 1 || ts[0].Total != 3 || ts[0].Rows[0].State != TaskDone || ts[0].Rows[1].State != TaskWaiting || !ts[0].CanStop {
		t.Fatalf("running task %+v", ts)
	}
	s.StopTask(run.t.ID)
	s.StopTask(run.t.ID) // a second stop is a no-op
	if stops != 1 || run.begin(1) || s.Tasks()[0].CanStop {
		t.Errorf("stop: %d calls, next row began %v, task %+v", stops, !run.stopped(), s.Tasks()[0])
	}
	run.end(nil, false)
	if got := s.Tasks()[0]; got.State != TaskStopped || got.Rows[2].State != TaskSkipped || got.Ended.IsZero() {
		t.Errorf("stopped task %+v", got)
	}

	// A routine index update leaves the list when it is done, a failed one stays.
	s.startTask(TaskIndex, "", nil, nil).end(nil, true)
	s.startTask(TaskIndex, "", nil, nil).end(errors.New("disk full"), true)
	ts = s.Tasks()
	if len(ts) != 2 || ts[0].Kind != TaskIndex || ts[0].State != TaskFailed || ts[0].Error != "disk full" {
		t.Errorf("after index runs %+v", ts)
	}

	// Running tasks come first; finished ones newest first, at most keptTasks of them.
	running := s.startTask(TaskFetch, "", []string{"a"}, nil)
	for range keptTasks + 5 {
		s.startTask(TaskDiscard, "f.go", nil, nil).end(nil, false)
	}
	ts = s.Tasks()
	if len(ts) != keptTasks+1 || ts[0].ID != running.t.ID || ts[1].ID < ts[2].ID {
		t.Errorf("order or bound: %d tasks, first %+v", len(ts), ts[0])
	}
	s.ClearTasks()
	if ts = s.Tasks(); len(ts) != 1 || ts[0].ID != running.t.ID {
		t.Errorf("clear kept %+v", ts)
	}
	if changes == 0 {
		t.Error("no change was reported")
	}
}

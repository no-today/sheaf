package session

import (
	"context"
	"testing"
)

func TestCallsSupersede(t *testing.T) {
	var c calls
	a, doneA := c.start(context.Background(), "x")
	other, doneO := c.start(context.Background(), "y")
	b, doneB := c.start(context.Background(), "x")
	if a.Err() == nil {
		t.Error("the older call of a kind should be cancelled")
	}
	if b.Err() != nil || other.Err() != nil {
		t.Error("the newest call and other kinds keep running")
	}
	doneA() // finishing late must not drop the newer call's entry
	c2, doneC := c.start(context.Background(), "x")
	if b.Err() == nil {
		t.Error("b should be cancelled by c")
	}
	doneB()
	doneC()
	doneO()
	if c2.Err() == nil {
		t.Error("done cancels its own context")
	}
	if len(c.run) != 0 {
		t.Errorf("entries left: %v", c.run)
	}
}

// A diff window and its workspace window share a session: one's call must not cancel the other's.
func TestCallsSupersedePerCaller(t *testing.T) {
	var c calls
	panel, done1 := c.start(Caller(context.Background(), "1"), "aggPatch")
	win, done2 := c.start(Caller(context.Background(), "2"), "aggPatch")
	if panel.Err() != nil || win.Err() != nil {
		t.Error("calls of different windows cancelled each other")
	}
	again, done3 := c.start(Caller(context.Background(), "1"), "aggPatch")
	if panel.Err() == nil || again.Err() != nil || win.Err() != nil {
		t.Error("a window's newer call cancels only its own older one")
	}
	done1()
	done2()
	done3()
}

// Calls run on goroutines: the one asked for first may reach the session last, and must not cancel the newer one.
func TestCallsSupersedeByAskedOrder(t *testing.T) {
	var c calls
	newer, doneN := c.start(Asked(context.Background(), 2), "x")
	older, doneO := c.start(Asked(context.Background(), 1), "x")
	if newer.Err() != nil {
		t.Error("an older call arriving late cancelled the newer one")
	}
	if older.Err() == nil {
		t.Error("an older call arriving late should start cancelled")
	}
	doneO()
	newest, doneL := c.start(Asked(context.Background(), 3), "x")
	if newer.Err() == nil || newest.Err() != nil {
		t.Error("a newer call cancels the running one")
	}
	doneN()
	doneL()
	if len(c.run) != 0 {
		t.Errorf("entries left: %v", c.run)
	}
}

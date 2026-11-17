package gitcli

import (
	"context"
	"sync"
	"testing"
)

func TestLimit(t *testing.T) {
	rec := &Recorder{}
	l := NewLimit(2)
	ctx := WithLimit(WithRecorder(context.Background(), rec), l)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := Version(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	evs := rec.Events()
	if len(evs) != 8 {
		t.Fatalf("%d commands recorded", len(evs))
	}
	for _, e := range evs {
		if e.Slot >= 2 { // the lowest free slot when it started: slot 2 means three ran at once
			t.Errorf("a command ran in slot %d with a limit of 2", e.Slot)
		}
	}

	l.slots <- struct{}{}
	l.slots <- struct{}{}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := Version(cancelled); err == nil {
		t.Error("a command waiting for a slot does not stop when its context is cancelled")
	}
}

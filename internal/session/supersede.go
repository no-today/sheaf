package session

import (
	"context"
	"sync"
)

// calls cancels a git-backed call when a newer one of the same kind arrives, so a view does not wait for stale reads.
// The zero value is ready to use.
type calls struct {
	mu  sync.Mutex
	run map[string]*call
}

type call struct {
	cancel context.CancelFunc
	asked  uint64
}

type callerKey struct{}

type askedKey struct{}

// Caller names who calls (a window): a call supersedes only the same caller's previous call of its kind. A diff window
// shares its workspace window's session, and its file must not cancel the one the workspace window's diff panel reads.
func Caller(ctx context.Context, who string) context.Context {
	return context.WithValue(ctx, callerKey{}, who)
}

// Asked stamps ctx with n, the order in which its caller asked for the call (larger is newer), taken on the caller's
// thread. Calls run on goroutines and may reach the session out of order: without the stamp an older call arriving
// last would cancel the newer one, which its caller is still waiting for.
func Asked(ctx context.Context, n uint64) context.Context {
	return context.WithValue(ctx, askedKey{}, n)
}

// start cancels the running call of this kind and returns the context of the new one; done must be called when it
// returns. A call asked for before the running one is stale: its own context comes back cancelled.
func (c *calls) start(ctx context.Context, kind string) (context.Context, func()) {
	if who, _ := ctx.Value(callerKey{}).(string); who != "" {
		kind += "\x00" + who
	}
	asked, _ := ctx.Value(askedKey{}).(uint64)
	ctx, cancel := context.WithCancel(ctx)
	me := &call{cancel: cancel, asked: asked}
	c.mu.Lock()
	if c.run == nil {
		c.run = map[string]*call{}
	}
	if prev := c.run[kind]; prev != nil {
		if asked != 0 && asked < prev.asked {
			c.mu.Unlock()
			cancel()
			return ctx, cancel
		}
		prev.cancel()
	}
	c.run[kind] = me
	c.mu.Unlock()
	return ctx, func() {
		cancel()
		c.mu.Lock()
		if c.run[kind] == me {
			delete(c.run, kind)
		}
		c.mu.Unlock()
	}
}

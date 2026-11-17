package gitcli

import "context"

// Limit bounds how many git processes run at once across everything that shares it (one per app: every window's
// session). Each caller also bounds its own fan-out; this is the ceiling over all of them together, so that opening a
// workspace while another indexes does not start dozens of git processes and their memory at once.
type Limit struct{ slots chan struct{} }

func NewLimit(n int) *Limit { return &Limit{slots: make(chan struct{}, max(n, 1))} }

// Cap is how many may run at once.
func (l *Limit) Cap() int { return cap(l.slots) }

type limitKey struct{}

// WithLimit makes the commands run with the returned context wait for a slot of l.
func WithLimit(ctx context.Context, l *Limit) context.Context {
	return context.WithValue(ctx, limitKey{}, l)
}

// acquire waits for a slot (none needed without a Limit); release must be called when the command ended.
// A command never takes a second slot while it holds one (stream readers do not run git), so waiting cannot deadlock.
func acquire(ctx context.Context) (release func(), err error) {
	l, _ := ctx.Value(limitKey{}).(*Limit)
	if l == nil {
		return func() {}, nil
	}
	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

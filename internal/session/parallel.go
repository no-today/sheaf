package session

import (
	"context"
	"sync"
)

// parallel runs f(0..n-1) with at most limit running at once and waits for them.
// Once ctx is cancelled no further call starts and the context's error is returned.
func parallel(ctx context.Context, n, limit int, f func(i int)) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, limit)
	defer wg.Wait()
	for i := range n {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			f(i)
		}()
	}
	return ctx.Err()
}

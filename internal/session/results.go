package session

import "sync"

// resultCache keeps the last keepResults query results, which views page through by id.
// The zero value is ready to use.
type resultCache struct {
	mu   sync.Mutex
	m    map[int]*result
	next int
}

// put stores r and returns its id; the result keepResults ids older is dropped.
func (c *resultCache) put(r *result) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[int]*result{}
	}
	c.next++
	c.m[c.next] = r
	delete(c.m, c.next-keepResults)
	return c.next
}

// get returns nil when the id is unknown or dropped.
func (c *resultCache) get(id int) *result {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[id]
}

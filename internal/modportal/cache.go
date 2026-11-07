package modportal

import (
	"sync"
	"time"
)

type cacheItem[T any] struct {
	value   T
	expires time.Time
}

// cache keeps the portal's answers for cacheLifetime. Callers store only
// answers, a mod or "no such mod", so a failure is never kept.
type cache[T any] struct {
	mu    sync.Mutex
	items map[string]cacheItem[T]
}

func (c *cache[T]) get(key string, now time.Time) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok || !now.Before(it.expires) {
		var zero T
		return zero, false
	}
	return it.value, true
}

func (c *cache[T]) put(key string, v T, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string]cacheItem[T]{}
	}
	for k, it := range c.items {
		if !now.Before(it.expires) {
			delete(c.items, k)
		}
	}
	c.items[key] = cacheItem[T]{value: v, expires: now.Add(cacheLifetime)}
}

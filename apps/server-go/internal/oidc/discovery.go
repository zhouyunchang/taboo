package oidc

import (
	"context"
	"sync"
	"time"

	federation "github.com/zhouyunchang/taboo/apps/server-go/internal/federation/oidc"
)

type cache struct {
	mu      sync.Mutex
	entries map[string]entry
}
type entry struct {
	p  *federation.Provider
	at time.Time
}

var discoCache = &cache{entries: map[string]entry{}}

func (c *cache) get(id, issuer string) (*federation.Provider, *federation.Provider, error) {
	return c.getContext(context.Background(), id, issuer)
}
func (c *cache) getContext(ctx context.Context, id, issuer string) (*federation.Provider, *federation.Provider, error) {
	c.mu.Lock()
	e, ok := c.entries[id]
	c.mu.Unlock()
	if ok && e.p.Issuer == issuer && time.Since(e.at) < 5*time.Minute {
		return e.p, e.p, nil
	}
	p, err := federation.Discover(ctx, issuer)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	// Bound cache size, including administrator configuration probes.
	if len(c.entries) >= 100 {
		c.entries = map[string]entry{}
	}
	c.entries[id] = entry{p, time.Now()}
	c.mu.Unlock()
	return p, p, nil
}
func (c *cache) drop(id string) { c.mu.Lock(); delete(c.entries, id); c.mu.Unlock() }

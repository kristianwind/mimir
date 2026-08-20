package enka

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Cache stores showcase responses until Enka will serve fresh data.
//
// Caching on the response's own TTL is not an optimisation, it is the
// documented way to use this API: requests made before the TTL expires return
// the identical payload and still consume rate limit. Every hit inside the
// window is therefore a wasted request against a shared budget.
type Cache interface {
	Get(uid string) (*Response, time.Time, bool)
	Put(uid string, r *Response, expires time.Time)
}

// MemoryCache is a process-local Cache. It is enough for a single-binary rune
// deployment; a multi-instance deployment should back this with the database
// so instances share one rate-limit budget.
type MemoryCache struct {
	mu      sync.RWMutex
	entries map[string]memEntry
}

type memEntry struct {
	resp    *Response
	expires time.Time
}

// NewMemoryCache returns an empty cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{entries: make(map[string]memEntry)}
}

// Get returns the cached response and its expiry, if present.
func (c *MemoryCache) Get(uid string) (*Response, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[uid]
	if !ok {
		return nil, time.Time{}, false
	}
	return e.resp, e.expires, true
}

// Put stores a response.
func (c *MemoryCache) Put(uid string, r *Response, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[uid] = memEntry{resp: r, expires: expires}
}

// CachedClient wraps a Client with TTL-aware caching.
type CachedClient struct {
	Client *Client
	Cache  Cache
	// Now is injectable for tests.
	Now func() time.Time
}

// NewCached returns a caching client.
func NewCached(userAgent string) *CachedClient {
	return &CachedClient{Client: New(userAgent), Cache: NewMemoryCache(), Now: time.Now}
}

// Fetched reports where a response came from, so the UI can say "showing data
// from 4 minutes ago, refreshable in 56 seconds" instead of pretending
// everything is live.
type Fetched struct {
	*Response
	FromCache bool
	// RefreshableAt is when a new request will actually return new data.
	RefreshableAt time.Time
	// Stale is true when Enka was unreachable or rate limited and Mimir fell
	// back to an expired cache entry.
	Stale bool
}

// Fetch returns a showcase, serving from cache while the TTL holds.
func (c *CachedClient) Fetch(ctx context.Context, uid string) (Fetched, error) {
	now := c.Now
	if now == nil {
		now = time.Now
	}
	if cached, exp, ok := c.Cache.Get(uid); ok && now().Before(exp) {
		return Fetched{Response: cached, FromCache: true, RefreshableAt: exp}, nil
	}

	resp, err := c.Client.Fetch(ctx, uid)
	if err != nil {
		// A rate limit or an outage should degrade to stale data, not to an
		// empty page: the player's build did not change in the meantime.
		if cached, exp, ok := c.Cache.Get(uid); ok && (errors.Is(err, ErrRateLimited) || !errors.Is(err, ErrNotFound)) {
			return Fetched{Response: cached, FromCache: true, RefreshableAt: exp, Stale: true}, nil
		}
		return Fetched{}, err
	}

	ttl := time.Duration(resp.TTL) * time.Second
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	exp := now().Add(ttl)
	c.Cache.Put(uid, resp, exp)
	return Fetched{Response: resp, RefreshableAt: exp}, nil
}

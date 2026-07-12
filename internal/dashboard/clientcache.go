package dashboard

import (
	"net/http"
	"sync"
)

// boundedClientCacheCap is the maximum number of *http.Client entries a
// boundedClientCache holds before it starts evicting to make room for a new
// one. Widget/monitor URL cardinality per dashboard is small in practice;
// this cap exists only to stop the insecureTLS per-baseURL client caches
// (unifi.go, proxmox.go) from growing unbounded over a dashboard pod's
// indefinite lifetime as ServiceCard URLs get edited — each entry holds its
// own *http.Transport and the idle connections it's opened.
const boundedClientCacheCap = 32

// boundedClientCache is a mutex-guarded, capacity-bounded map from a cache
// key (a widget's baseURL) to a *http.Client, shared by unifi.go and
// proxmox.go's insecureTLS client caches (each widget gets its own
// boundedClientCache instance — see unifiInsecureClientCache/
// proxmoxInsecureClientCache — since a URL edited on one widget type has no
// bearing on another's cache). Unlike poller.go's caClientCache (pruned
// precisely, once per poll cycle, against the set of keys actually used
// that cycle), this is a much simpler bound: once at boundedClientCacheCap,
// getOrCreate evicts an arbitrary existing entry (map iteration order) and
// closes its transport's idle connections before inserting the new one —
// no per-cycle bookkeeping, just a hard cap on how many idle-connection
// pools can accumulate.
type boundedClientCache struct {
	mu      sync.Mutex
	clients map[string]*http.Client
}

// newBoundedClientCache returns a ready-to-use, empty boundedClientCache.
func newBoundedClientCache() *boundedClientCache {
	return &boundedClientCache{clients: map[string]*http.Client{}}
}

// getOrCreate returns the client cached under key, or calls build to create
// one, caches it under key, and returns it. If the cache already holds
// boundedClientCacheCap entries and key isn't one of them, an arbitrary
// existing entry is evicted first — CloseIdleConnections is called on its
// Transport (if it implements that method, as *http.Transport does) so the
// evicted client's idle connections don't linger past the cache entry
// itself.
func (c *boundedClientCache) getOrCreate(key string, build func() *http.Client) *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.clients[key]; ok {
		return existing
	}

	if len(c.clients) >= boundedClientCacheCap {
		for evictKey, evictClient := range c.clients {
			closeIdleConnections(evictClient)
			delete(c.clients, evictKey)
			break
		}
	}

	client := build()
	c.clients[key] = client
	return client
}

// closeIdleConnections calls CloseIdleConnections on client's Transport if
// it implements the (unexported-in-net/http, but universally satisfied by
// *http.Transport) idleCloser interface — best-effort, since http.RoundTripper
// doesn't require it.
func closeIdleConnections(client *http.Client) {
	type idleCloser interface{ CloseIdleConnections() }
	if ic, ok := client.Transport.(idleCloser); ok {
		ic.CloseIdleConnections()
	}
}

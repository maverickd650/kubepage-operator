package dashboard

import (
	"fmt"
	"net/http"
	"testing"
)

// countingTransport is a minimal http.RoundTripper that records whether
// CloseIdleConnections was called on it, so tests can assert eviction
// actually closes the evicted client's idle connections rather than just
// dropping the map entry.
type countingTransport struct {
	closed bool
}

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("countingTransport: RoundTrip not implemented")
}

func (t *countingTransport) CloseIdleConnections() {
	t.closed = true
}

func newCountingClient() (*http.Client, *countingTransport) {
	tr := &countingTransport{}
	return &http.Client{Transport: tr}, tr
}

// TestBoundedClientCacheGetOrCreateReusesExistingEntry verifies a second
// getOrCreate call for the same key returns the cached client instead of
// invoking build again.
func TestBoundedClientCacheGetOrCreateReusesExistingEntry(t *testing.T) {
	c := newBoundedClientCache()
	builds := 0
	build := func() *http.Client {
		builds++
		client, _ := newCountingClient()
		return client
	}

	first := c.getOrCreate("https://a.example", build)
	second := c.getOrCreate("https://a.example", build)

	if first != second {
		t.Errorf("second getOrCreate returned a different *http.Client, want the cached one")
	}
	if builds != 1 {
		t.Errorf("build called %d times, want 1 (second call should reuse the cached entry)", builds)
	}
}

// TestBoundedClientCacheRespectsCapAndEvicts verifies the cache never grows
// past boundedClientCacheCap entries, and that filling it past the cap
// evicts an existing entry, closing that entry's transport's idle
// connections in the process.
func TestBoundedClientCacheRespectsCapAndEvicts(t *testing.T) {
	c := newBoundedClientCache()
	transports := make(map[string]*countingTransport, boundedClientCacheCap+1)

	for i := range boundedClientCacheCap {
		key := fmt.Sprintf("https://host-%d.example", i)
		c.getOrCreate(key, func() *http.Client {
			client, tr := newCountingClient()
			transports[key] = tr
			return client
		})
	}

	c.mu.Lock()
	size := len(c.clients)
	c.mu.Unlock()
	if size != boundedClientCacheCap {
		t.Fatalf("cache size = %d after filling to cap, want %d", size, boundedClientCacheCap)
	}

	// One more distinct key must trigger an eviction rather than growing
	// past the cap.
	overflowKey := "https://overflow.example"
	c.getOrCreate(overflowKey, func() *http.Client {
		client, tr := newCountingClient()
		transports[overflowKey] = tr
		return client
	})

	c.mu.Lock()
	size = len(c.clients)
	_, overflowStillPresent := c.clients[overflowKey]
	c.mu.Unlock()
	if size != boundedClientCacheCap {
		t.Errorf("cache size = %d after inserting past cap, want %d (bounded)", size, boundedClientCacheCap)
	}
	if !overflowStillPresent {
		t.Errorf("the newly inserted overflow key was not retained")
	}

	evictedCount := 0
	for key, tr := range transports {
		if key == overflowKey {
			continue
		}
		c.mu.Lock()
		_, present := c.clients[key]
		c.mu.Unlock()
		if !present {
			evictedCount++
			if !tr.closed {
				t.Errorf("evicted entry %q: CloseIdleConnections was not called on its transport", key)
			}
		} else if tr.closed {
			t.Errorf("entry %q is still cached but its transport was closed", key)
		}
	}
	if evictedCount != 1 {
		t.Errorf("evicted %d entries inserting one key past the cap, want exactly 1", evictedCount)
	}
}

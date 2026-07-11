package dashboard

import "sync"

// Broadcaster fans a single signal out to any number of subscribers without
// blocking the publisher when a subscriber isn't currently reading — used to
// wake every open SSE connection (see Server.handleEvents) at the end of
// each Poller.pollOnce cycle, so they can check whether the fragment/header
// content actually changed and push a refresh event to the browser only
// then, rather than the browser having to poll on a fixed interval.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// NewBroadcaster returns a ready-to-use Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: map[chan struct{}]struct{}{}}
}

// Subscribe registers a new channel that receives a value every time
// Publish is called, until Unsubscribe removes it. The channel is buffered
// (size 1): a subscriber that hasn't drained the previous signal yet just
// coalesces into the same pending wakeup instead of blocking Publish.
func (b *Broadcaster) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes ch, added by a prior Subscribe call. Safe to call more
// than once or with a channel that was never subscribed.
func (b *Broadcaster) Unsubscribe(ch chan struct{}) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

// Publish wakes every current subscriber. Never blocks: a full subscriber
// channel (an already-pending, not-yet-drained wakeup) is left as-is rather
// than waited on.
func (b *Broadcaster) Publish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

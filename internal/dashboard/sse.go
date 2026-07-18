package dashboard

import "sync"

// sseHashes is the fragment/header content-hash pair computed once per poll
// cycle (Poller.pollOnce, via currentHashes) and carried through Broadcast
// to every subscriber — see Broadcast's doc comment for why this replaced a
// plain wakeup signal.
type sseHashes struct {
	fragment, header string
}

// maxSSESubscribers bounds Broadcaster.Subscribe. With no auth enabled by
// default, anyone who can reach the dashboard Service can open GET /events
// connections, each holding a goroutine and a channel open indefinitely;
// past this many concurrent subscribers, handleEvents rejects new
// connections with 503 rather than growing unbounded. htmx's own interval
// poll (see index.templ) already covers a client that can't get an SSE
// connection, so a rejected client still gets refreshed, just not pushed.
const maxSSESubscribers = 256

// Broadcaster fans the latest fragment/header content hashes out to any
// number of subscribers without blocking the publisher when a subscriber
// isn't currently reading — used to wake every open SSE connection (see
// Server.handleEvents) at the end of each Poller.pollOnce cycle. The hashes
// are computed once by the publisher rather than once per subscriber, so an
// SSE broadcast costs O(1) renders per cycle regardless of how many tabs are
// open.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan sseHashes]struct{}
}

// NewBroadcaster returns a ready-to-use Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: map[chan sseHashes]struct{}{}}
}

// Subscribe registers a new channel that receives the latest fragment/header
// hashes every time Publish is called, until Unsubscribe removes it. ok is
// false, and no channel is registered, once maxSSESubscribers is already
// reached. The channel is buffered (size 1): Publish overwrites a pending,
// not-yet-delivered value with the newer one rather than blocking or
// accumulating a backlog.
func (b *Broadcaster) Subscribe() (ch chan sseHashes, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) >= maxSSESubscribers {
		return nil, false
	}
	ch = make(chan sseHashes, 1)
	b.subs[ch] = struct{}{}
	return ch, true
}

// Unsubscribe removes ch, added by a prior Subscribe call. Safe to call more
// than once or with a channel that was never subscribed.
func (b *Broadcaster) Unsubscribe(ch chan sseHashes) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

// HasSubscribers reports whether at least one SSE connection is currently
// subscribed. Poller.pollOnce checks this to skip computing currentHashes
// (two full templ renders plus LoadSite) and calling Publish on a cycle with
// no one listening — see pollOnce's comment for why that's safe even though
// a client can connect in the gap between this check and the next cycle.
func (b *Broadcaster) HasSubscribers() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs) > 0
}

// Publish sends fragment/header to every current subscriber. Never blocks: a
// channel still holding an undelivered previous value has that value
// replaced with the newer one instead of Publish waiting on it.
func (b *Broadcaster) Publish(fragment, header string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := sseHashes{fragment: fragment, header: header}
	for ch := range b.subs {
		select {
		case ch <- h:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- h:
			default:
			}
		}
	}
}

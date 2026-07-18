package dashboard

import "testing"

func TestBroadcasterPublishDeliversToSubscribers(t *testing.T) {
	b := NewBroadcaster()
	ch1, ok1 := b.Subscribe()
	ch2, ok2 := b.Subscribe()
	if !ok1 || !ok2 {
		t.Fatal("Subscribe() ok = false, want true below maxSSESubscribers")
	}

	b.Publish("frag-1", "hdr-1")

	select {
	case h := <-ch1:
		if h.fragment != "frag-1" || h.header != "hdr-1" {
			t.Errorf("ch1 got %+v, want {frag-1 hdr-1}", h)
		}
	default:
		t.Error("ch1 did not receive a publish")
	}
	select {
	case h := <-ch2:
		if h.fragment != "frag-1" || h.header != "hdr-1" {
			t.Errorf("ch2 got %+v, want {frag-1 hdr-1}", h)
		}
	default:
		t.Error("ch2 did not receive a publish")
	}
}

func TestBroadcasterPublishOverwritesUndeliveredValue(t *testing.T) {
	b := NewBroadcaster()
	ch, ok := b.Subscribe()
	if !ok {
		t.Fatal("Subscribe() ok = false, want true")
	}

	// Two publishes with nothing draining ch in between: the second must not
	// block even though the channel's buffer (size 1) is already full, and
	// the subscriber must see the newer value, not the stale first one.
	b.Publish("frag-1", "hdr-1")
	b.Publish("frag-2", "hdr-2")

	select {
	case h := <-ch:
		if h.fragment != "frag-2" || h.header != "hdr-2" {
			t.Errorf("ch got %+v, want the newer {frag-2 hdr-2}", h)
		}
	default:
		t.Fatal("ch did not receive the coalesced publish")
	}
}

func TestBroadcasterUnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroadcaster()
	ch, ok := b.Subscribe()
	if !ok {
		t.Fatal("Subscribe() ok = false, want true")
	}
	b.Unsubscribe(ch)

	b.Publish("frag", "hdr")

	select {
	case <-ch:
		t.Error("unsubscribed channel received a publish")
	default:
	}
}

func TestBroadcasterUnsubscribeUnknownChannelIsSafe(t *testing.T) {
	b := NewBroadcaster()
	b.Unsubscribe(make(chan sseHashes, 1))
}

func TestBroadcasterSubscribeRejectsPastMaxSubscribers(t *testing.T) {
	b := NewBroadcaster()
	for i := range maxSSESubscribers {
		if _, ok := b.Subscribe(); !ok {
			t.Fatalf("Subscribe() ok = false at subscriber %d, want true below the cap", i)
		}
	}

	if _, ok := b.Subscribe(); ok {
		t.Error("Subscribe() ok = true at maxSSESubscribers, want false")
	}
}

// TestBroadcasterHasSubscribers verifies HasSubscribers reflects the current
// subscriber count: false with none, true once one is subscribed, false
// again once it's the last one unsubscribed. Poller.pollOnce uses this to
// skip computing currentHashes and calling Publish on a cycle with no SSE
// connections open.
func TestBroadcasterHasSubscribers(t *testing.T) {
	b := NewBroadcaster()
	if b.HasSubscribers() {
		t.Error("HasSubscribers() = true before any Subscribe, want false")
	}

	ch, ok := b.Subscribe()
	if !ok {
		t.Fatal("Subscribe() ok = false, want true")
	}
	if !b.HasSubscribers() {
		t.Error("HasSubscribers() = false after Subscribe, want true")
	}

	b.Unsubscribe(ch)
	if b.HasSubscribers() {
		t.Error("HasSubscribers() = true after Unsubscribe of the last subscriber, want false")
	}
}

// TestBroadcasterUnsubscribeFreesSlotAtCap verifies Unsubscribe actually
// frees the slot it held: at the cap, unsubscribing one connection must let
// exactly one new Subscribe succeed, not zero (a leaked count) and not more
// than one (a miscounted map).
func TestBroadcasterUnsubscribeFreesSlotAtCap(t *testing.T) {
	b := NewBroadcaster()
	chs := make([]chan sseHashes, 0, maxSSESubscribers)
	for i := range maxSSESubscribers {
		ch, ok := b.Subscribe()
		if !ok {
			t.Fatalf("Subscribe() ok = false at subscriber %d, want true below the cap", i)
		}
		chs = append(chs, ch)
	}

	b.Unsubscribe(chs[0])

	if _, ok := b.Subscribe(); !ok {
		t.Error("Subscribe() ok = false right after freeing a slot, want true")
	}
	if _, ok := b.Subscribe(); ok {
		t.Error("Subscribe() ok = true after refilling the freed slot, want false (still at cap)")
	}
}

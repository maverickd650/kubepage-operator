package dashboard

import "testing"

func TestBroadcasterPublishDeliversToSubscribers(t *testing.T) {
	b := NewBroadcaster()
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Publish()

	select {
	case <-ch1:
	default:
		t.Error("ch1 did not receive a publish")
	}
	select {
	case <-ch2:
	default:
		t.Error("ch2 did not receive a publish")
	}
}

func TestBroadcasterPublishDoesNotBlockOnFullSubscriber(t *testing.T) {
	b := NewBroadcaster()
	ch := b.Subscribe()

	// Two publishes with nothing draining ch in between: the second must not
	// block even though the channel's buffer (size 1) is already full.
	b.Publish()
	b.Publish()

	select {
	case <-ch:
	default:
		t.Fatal("ch did not receive the coalesced publish")
	}
}

func TestBroadcasterUnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroadcaster()
	ch := b.Subscribe()
	b.Unsubscribe(ch)

	b.Publish()

	select {
	case <-ch:
		t.Error("unsubscribed channel received a publish")
	default:
	}
}

func TestBroadcasterUnsubscribeUnknownChannelIsSafe(t *testing.T) {
	b := NewBroadcaster()
	b.Unsubscribe(make(chan struct{}, 1))
}

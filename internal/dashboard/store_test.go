package dashboard

import (
	"sync"
	"testing"
)

// TestStoreConcurrentSetAndSnapshot exercises the exact reader/writer pattern
// CLAUDE.md calls out (poller writes on its own ticker, the HTTP server reads
// on every request): Set and Snapshot running at the same instant, not one
// after the other. Run with -race, this is what would actually catch a
// regression that drops or misuses Store.mu (e.g. Snapshot reading s.cards
// without RLock) — the other Store tests only ever call Snapshot after all
// writers have already joined via wg.Wait(), so they pass under -race even
// with a broken lock.
func TestStoreConcurrentSetAndSnapshot(t *testing.T) {
	s := NewStore()
	const writers = 8
	const readers = 4
	const iterations = 500

	var writersWG sync.WaitGroup
	for i := range writers {
		writersWG.Go(func() {
			key := "card-" + intToStr(i)
			for j := range iterations {
				s.Set(Card{Key: key, ServiceName: intToStr(j)})
			}
		})
	}

	stop := make(chan struct{})
	var readersWG sync.WaitGroup
	for range readers {
		readersWG.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_ = s.Snapshot()
				}
			}
		})
	}

	writersWG.Wait()
	close(stop)
	readersWG.Wait()
}

func TestCompareOrder(t *testing.T) {
	one := int32(1)
	two := int32(2)
	tests := map[string]struct {
		a, b *int32
		want int
	}{
		"both nil":         {a: nil, b: nil, want: 0},
		"a nil sorts last": {a: nil, b: &one, want: 1},
		"b nil sorts last": {a: &one, b: nil, want: -1},
		"a less than b":    {a: &one, b: &two, want: -1},
		"a greater than b": {a: &two, b: &one, want: 1},
		"equal":            {a: &one, b: &one, want: 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := compareOrder(tc.a, tc.b); got != tc.want {
				t.Errorf("compareOrder(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

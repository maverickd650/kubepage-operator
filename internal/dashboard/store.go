package dashboard

import (
	"slices"
	"strings"
	"sync"
	"time"
)

// Card is one widget instance's latest poll result, ready for rendering.
type Card struct {
	// Key uniquely identifies this card across polls (ServiceEntry
	// namespace/name + widget index), used as the DOM id for htmx swaps.
	Key string

	Group       string
	ServiceName string
	WidgetType  string
	Order       *int32
	IconURL     string
	Description string
	Href        string
	// Target is the link target for Href ("_blank"/"_self"), already
	// resolved from the ServiceEntry override or the site default.
	Target string

	// Header marks a card produced from an InfoWidget (rendered in the
	// header strip) rather than a ServiceEntry service card.
	Header bool

	// ShowStats controls whether Fields render; HideErrors suppresses Err.
	ShowStats  bool
	HideErrors bool

	// Monitor (ping/siteMonitor) result. Status is "" when no monitor is
	// configured, otherwise "Up"/"Down". StatusStyle is "dot"/"basic".
	Status      string
	StatusStyle string
	Latency     string

	Fields    []Field
	Err       string
	UpdatedAt time.Time
}

// Store holds the most recently polled Card for every known widget
// instance. Safe for concurrent use: the poller writes on its interval, the
// HTTP server reads on every request.
type Store struct {
	mu    sync.RWMutex
	cards map[string]Card
}

func NewStore() *Store {
	return &Store{cards: map[string]Card{}}
}

// Set records the latest Card for its Key, replacing any previous value.
func (s *Store) Set(c Card) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cards[c.Key] = c
}

// Prune removes any stored card whose key is not in keep, so cards for
// deleted/rebound ServiceEntries don't linger forever.
func (s *Store) Prune(keep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.cards {
		if !keep[k] {
			delete(s.cards, k)
		}
	}
}

// Snapshot returns every stored Card, ordered by Group, then Order (nil
// last), then ServiceName, then Key — a stable display order even though
// the underlying map has none.
func (s *Store) Snapshot() []Card {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Card, 0, len(s.cards))
	for _, c := range s.cards {
		out = append(out, c)
	}

	slices.SortFunc(out, func(a, b Card) int {
		if c := strings.Compare(a.Group, b.Group); c != 0 {
			return c
		}
		if c := compareOrder(a.Order, b.Order); c != 0 {
			return c
		}
		if c := strings.Compare(a.ServiceName, b.ServiceName); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out
}

// compareOrder orders *int32 ascending with nil sorting last, matching
// internal/render's group/entry ordering convention.
func compareOrder(a, b *int32) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	default:
		return 0
	}
}

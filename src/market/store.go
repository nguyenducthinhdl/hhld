package market

import (
	"fmt"
	"sync"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

type bookKey struct {
	venue  exchange.VenueID
	symbol exchange.Symbol
}

type bookState struct {
	book      exchange.Book
	seq       uint64
	hasSnap   bool
	updatedAt time.Time
}

// BookStore holds the latest book per (venue, symbol), applying snapshots and deltas.
type BookStore struct {
	mu   sync.RWMutex
	byKey map[bookKey]*bookState
}

// NewBookStore builds an empty store.
func NewBookStore() *BookStore {
	return &BookStore{byKey: make(map[bookKey]*bookState)}
}

// Apply updates the store. Returns the resulting book.
// Delta before snapshot for a key is rejected.
func (s *BookStore) Apply(ev BookEvent) (exchange.Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch ev.Kind {
	case KindSnapshot:
		return s.applySnapshot(ev.Snapshot)
	case KindDelta:
		return s.applyDelta(ev.Delta)
	default:
		return exchange.Book{}, fmt.Errorf("market: unknown event kind %d", ev.Kind)
	}
}

func (s *BookStore) applySnapshot(b exchange.Book) (exchange.Book, error) {
	if b.Venue == "" || b.Symbol == "" {
		return exchange.Book{}, fmt.Errorf("market: snapshot missing venue or symbol")
	}
	key := bookKey{venue: b.Venue, symbol: b.Symbol}
	cp := cloneBook(b)
	now := time.Now().UTC()
	s.byKey[key] = &bookState{book: cp, hasSnap: true, seq: 0, updatedAt: now}
	return cloneBook(cp), nil
}

func (s *BookStore) applyDelta(d BookDelta) (exchange.Book, error) {
	if d.Venue == "" || d.Symbol == "" {
		return exchange.Book{}, fmt.Errorf("market: delta missing venue or symbol")
	}
	key := bookKey{venue: d.Venue, symbol: d.Symbol}
	st, ok := s.byKey[key]
	if !ok || !st.hasSnap {
		return exchange.Book{}, fmt.Errorf("market: delta before snapshot for %s/%s", d.Venue, d.Symbol)
	}
	if d.Seq != 0 && st.seq != 0 && d.Seq <= st.seq {
		return exchange.Book{}, fmt.Errorf("market: sequence went backward for %s/%s (%d <= %d)", d.Venue, d.Symbol, d.Seq, st.seq)
	}
	st.book = mergeDelta(st.book, d)
	if d.Seq != 0 {
		st.seq = d.Seq
	}
	st.updatedAt = time.Now().UTC()
	return cloneBook(st.book), nil
}

// Get returns a copy of the book if present.
func (s *BookStore) Get(venue exchange.VenueID, symbol exchange.Symbol) (exchange.Book, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.byKey[bookKey{venue: venue, symbol: symbol}]
	if !ok || !st.hasSnap {
		return exchange.Book{}, false
	}
	return cloneBook(st.book), true
}

// LastUpdated returns when the book was last applied locally (feed receive time).
func (s *BookStore) LastUpdated(venue exchange.VenueID, symbol exchange.Symbol) (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.byKey[bookKey{venue: venue, symbol: symbol}]
	if !ok || !st.hasSnap || st.updatedAt.IsZero() {
		return time.Time{}, false
	}
	return st.updatedAt, true
}

// Clear removes the book for a venue/symbol (e.g. after reconnect before next snapshot).
func (s *BookStore) Clear(venue exchange.VenueID, symbol exchange.Symbol) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byKey, bookKey{venue: venue, symbol: symbol})
}

func mergeDelta(base exchange.Book, d BookDelta) exchange.Book {
	out := cloneBook(base)
	if d.Kind != "" {
		out.Kind = d.Kind
	}
	if !d.Time.IsZero() {
		out.Time = d.Time
	}
	out.Bids = applySide(out.Bids, d.Bids)
	out.Asks = applySide(out.Asks, d.Asks)
	return out
}

func applySide(cur, delta []exchange.Level) []exchange.Level {
	m := make(map[string]string, len(cur)+len(delta))
	order := make([]string, 0, len(cur)+len(delta))
	seen := make(map[string]struct{}, len(cur)+len(delta))
	for _, lv := range cur {
		m[lv.Price] = lv.Size
		if _, ok := seen[lv.Price]; !ok {
			seen[lv.Price] = struct{}{}
			order = append(order, lv.Price)
		}
	}
	for _, lv := range delta {
		if _, ok := seen[lv.Price]; !ok {
			seen[lv.Price] = struct{}{}
			order = append(order, lv.Price)
		}
		if lv.Size == "0" || lv.Size == "" {
			delete(m, lv.Price)
			continue
		}
		m[lv.Price] = lv.Size
	}
	out := make([]exchange.Level, 0, len(m))
	for _, px := range order {
		sz, ok := m[px]
		if !ok {
			continue
		}
		out = append(out, exchange.Level{Price: px, Size: sz})
	}
	return out
}

func cloneBook(b exchange.Book) exchange.Book {
	out := b
	if b.Bids != nil {
		out.Bids = append([]exchange.Level(nil), b.Bids...)
	}
	if b.Asks != nil {
		out.Asks = append([]exchange.Level(nil), b.Asks...)
	}
	return out
}

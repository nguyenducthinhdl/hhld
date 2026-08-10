// Package market is the event-driven book core (P8.5): BookEvent, BookStore, Bus, Runner.
package market

import (
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// BookDelta is an incremental book update. Size "0" deletes that price level.
type BookDelta struct {
	Venue  exchange.VenueID
	Symbol exchange.Symbol
	Kind   exchange.Kind
	Bids   []exchange.Level
	Asks   []exchange.Level
	Time   time.Time
	// Seq is an optional monotonic sequence; 0 means unused.
	Seq uint64
}

// Kind identifies a BookEvent variant.
type Kind int

const (
	KindSnapshot Kind = iota + 1
	KindDelta
)

// BookEvent is a market-data ingress message (snapshot or delta).
type BookEvent struct {
	Kind     Kind
	Snapshot exchange.Book // KindSnapshot
	Delta    BookDelta     // KindDelta
}

// SnapshotEvent builds a snapshot BookEvent.
func SnapshotEvent(b exchange.Book) BookEvent {
	return BookEvent{Kind: KindSnapshot, Snapshot: b}
}

// DeltaEvent builds a delta BookEvent.
func DeltaEvent(d BookDelta) BookEvent {
	return BookEvent{Kind: KindDelta, Delta: d}
}

// VenueSymbol returns the key for the event.
func (e BookEvent) VenueSymbol() (exchange.VenueID, exchange.Symbol) {
	switch e.Kind {
	case KindSnapshot:
		return e.Snapshot.Venue, e.Snapshot.Symbol
	case KindDelta:
		return e.Delta.Venue, e.Delta.Symbol
	default:
		return "", ""
	}
}

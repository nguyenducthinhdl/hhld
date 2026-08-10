package market

import (
	"context"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/exchange/grvt"
)

// BridgeBooks subscribes to venue books and publishes each update as a Snapshot event.
// Use for Hyperliquid l2Book (full snapshots each push) and GRVT v1.book.s.
// Blocks until ctx is canceled (same contract as live SubscribeBook).
func BridgeBooks(ctx context.Context, ex exchange.Exchange, symbol exchange.Symbol, bus *Bus) error {
	return ex.SubscribeBook(ctx, symbol, func(b exchange.Book) {
		bus.Publish(SnapshotEvent(b))
	})
}

// PublishDelta publishes a delta event onto the bus (tests / GRVT book.d path).
func PublishDelta(bus *Bus, d BookDelta) {
	bus.Publish(DeltaEvent(d))
}

// BridgeFakeDeltas wires fake.PushDelta into the bus as BookDelta events.
func BridgeFakeDeltas(ex *fake.Exchange, symbol exchange.Symbol, kind exchange.Kind, bus *Bus) {
	ex.SubscribeDelta(symbol, func(sym exchange.Symbol, bids, asks []exchange.Level, t time.Time) {
		PublishDelta(bus, BookDelta{
			Venue:  ex.ID(),
			Symbol: sym,
			Kind:   kind,
			Bids:   bids,
			Asks:   asks,
			Time:   t,
		})
	})
}

// BridgeGRVTDeltas streams GRVT v1.book.d into the bus.
// On each WS session start, store.Clear is called for the venue/symbol so the next
// snapshot resets state (P8 reconnect doctrine). Snapshots and deltas are published.
func BridgeGRVTDeltas(ctx context.Context, ad *grvt.Adapter, symbol exchange.Symbol, store *BookStore, bus *Bus) error {
	return ad.SubscribeBookDeltas(ctx, symbol,
		func(sym exchange.Symbol) {
			if store != nil {
				store.Clear(ad.ID(), sym)
			}
		},
		func(b exchange.Book) {
			bus.Publish(SnapshotEvent(b))
		},
		func(venue exchange.VenueID, sym exchange.Symbol, kind exchange.Kind, bids, asks []exchange.Level, t time.Time, seq uint64) {
			bus.Publish(DeltaEvent(BookDelta{
				Venue: venue, Symbol: sym, Kind: kind,
				Bids: bids, Asks: asks, Time: t, Seq: seq,
			}))
		},
	)
}

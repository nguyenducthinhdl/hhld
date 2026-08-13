package market

import (
	"context"
	"log"
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

// BridgeGRVTPoll polls GRVT REST SnapshotBook at interval and publishes into the bus.
// Use as a fallback when the GRVT WS protocol is unavailable (e.g. API breaking change).
// Blocks until ctx is canceled.
func BridgeGRVTPoll(ctx context.Context, ad *grvt.Adapter, symbol exchange.Symbol, interval time.Duration, bus *Bus) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			snapCtx, cancel := context.WithTimeout(ctx, interval)
			b, err := ad.SnapshotBook(snapCtx, symbol)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("grvt poll %s: %v", symbol, err)
				}
				continue
			}
			bus.Publish(SnapshotEvent(b))
		}
	}
}

// BridgeGRVTDeltas streams GRVT v1.book.d into the bus.
// On WS reconnect (not the first session), store.Clear resets state before the next
// snapshot (P8 reconnect doctrine). The first session leaves any seeded REST book intact
// until the first WS snapshot arrives.
func BridgeGRVTDeltas(ctx context.Context, ad *grvt.Adapter, symbol exchange.Symbol, store *BookStore, bus *Bus) error {
	firstSession := true
	return ad.SubscribeBookDeltas(ctx, symbol,
		func(sym exchange.Symbol) {
			if firstSession {
				firstSession = false
				return
			}
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

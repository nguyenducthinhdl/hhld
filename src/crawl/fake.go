package crawl

import (
	"context"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

// FakeDual writes scripted dual-venue book snapshots (no network).
// Same shape as sim replay tests — useful when no sample file is present.
type FakeDual struct {
	Symbol exchange.Symbol
	VenueA exchange.VenueID
	VenueB exchange.VenueID
	Kind   exchange.Kind
	Books  []exchange.Book
}

// DefaultFakeDualBooks returns a two-step HL/GRVT BTCUSD history with one arb gap.
func DefaultFakeDualBooks() []exchange.Book {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	t1 := t0.Add(time.Second)
	return []exchange.Book{
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.0", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
			Time: t0,
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.2", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.3", Size: "2"}},
			Time: t0,
		},
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.0", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
			Time: t1,
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "101.0", Size: "2"}},
			Asks: []exchange.Level{{Price: "101.1", Size: "2"}},
			Time: t1,
		},
	}
}

func (c FakeDual) Run(ctx context.Context, store warehouse.Store) error {
	books := c.Books
	if len(books) == 0 {
		books = DefaultFakeDualBooks()
	}
	for _, b := range books {
		if err := ctx.Err(); err != nil {
			return err
		}
		if b.Symbol == "" {
			b.Symbol = c.Symbol
		}
		if b.Symbol == "" {
			b.Symbol = "BTCUSD"
		}
		if b.Kind == "" {
			b.Kind = c.Kind
		}
		if b.Kind == "" {
			b.Kind = exchange.KindPerp
		}
		if err := store.AppendBook(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

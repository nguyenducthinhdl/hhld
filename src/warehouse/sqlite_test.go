package warehouse_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

// TestSQLite_AppendAndQuery persists normalized books/ticks for backtest replay.
//
// Why: P7 done-when requires crawled sample data in a local store queryable by symbol/time
// (spec/roadmap/p7.md; spec/tech-stack.md Data warehouse).
func TestSQLite_AppendAndQuery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hhld.db")
	st, err := warehouse.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ts := time.Unix(100, 0).UTC()
	book := exchange.Book{
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Bids: []exchange.Level{{Price: "99.9", Size: "1"}},
		Asks: []exchange.Level{{Price: "100.0", Size: "1"}},
		Time: ts,
	}
	tick := exchange.Tick{
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Price: "100", Size: "1", Side: exchange.SideBuy, Time: ts,
	}
	if err := st.AppendBook(ctx, book); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTick(ctx, tick); err != nil {
		t.Fatal(err)
	}

	books, err := st.QueryBooks(ctx, "BTCUSD", ts.Add(-time.Second), ts.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].Venue != "hyperliquid" || books[0].Asks[0].Price != "100.0" {
		t.Fatalf("books: %+v", books)
	}

	ticks, err := st.QueryTicks(ctx, "BTCUSD", ts.Add(-time.Second), ts.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 1 || ticks[0].Price != "100" {
		t.Fatalf("ticks: %+v", ticks)
	}
}

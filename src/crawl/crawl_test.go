package crawl_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/crawl"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/sim"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

// TestSampleFileToSimReplay is the P7 done-when path: crawl → warehouse → P6 replay.
//
// Why: spec/roadmap/p7.md requires crawled sample data in the warehouse feeding backtest sim.
func TestSampleFileToSimReplay(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "hhld.db")
	st, err := warehouse.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	samplePath := filepath.Join("..", "..", "data", "samples", "btcusd_books.ndjson")
	if err := (crawl.SampleFile{Path: samplePath}).Run(ctx, st); err != nil {
		t.Fatal(err)
	}

	from := time.Unix(1_700_000_000, 0).UTC().Add(-time.Hour)
	to := from.Add(2 * time.Hour)
	in, err := sim.InputFromStore(ctx, st, "BTCUSD", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Books) != 4 {
		t.Fatalf("want 4 books from sample crawl, got %d", len(in.Books))
	}

	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"},
		Size:    "1",
		MinGap:  0.3,
	})
	gate := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0.01, PartialFillFactor: 1,
		MaxBookAge: 10 * time.Second, MaxInFlight: 8,
	})
	tracker := pnl.NewMemory()
	snap, err := sim.NewReplay(nil).Run(ctx, in, arb, gate, tracker)
	if err != nil {
		t.Fatal(err)
	}
	got, err := strconv.ParseFloat(snap.Realized, 64)
	if err != nil {
		t.Fatal(err)
	}
	if got <= 0 {
		t.Fatalf("want positive PnL from warehouse-fed replay, got %s", snap.Realized)
	}
}

// TestFakeDualCrawler persists scripted books without a sample file.
func TestFakeDualCrawler(t *testing.T) {
	ctx := context.Background()
	st, err := warehouse.OpenSQLite(filepath.Join(t.TempDir(), "hhld.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := (crawl.FakeDual{}).Run(ctx, st); err != nil {
		t.Fatal(err)
	}
	from := time.Unix(1_700_000_000, 0).UTC().Add(-time.Hour)
	to := from.Add(2 * time.Hour)
	books, err := st.QueryBooks(ctx, "BTCUSD", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 4 {
		t.Fatalf("want 4 books, got %d", len(books))
	}
}
